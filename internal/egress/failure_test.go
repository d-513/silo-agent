package egress

import (
	"bufio"
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func dialErr(err error) error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: err}
}

// What the box is told says what happened and whose side it is on: a site that
// is down, refusing or cut off is not the filter being broken.
func TestUpstreamFailureSaysWhatHappened(t *testing.T) {
	cases := []struct {
		name, stage string
		err         error
		status      int
		reason      string
		says        string
		site, block bool
	}{
		{"no such name", stageDial, dialErr(&net.DNSError{Err: "no such host", Name: "gone.example", Server: "10.213.4.1:53", IsNotFound: true}),
			502, "dns_not_found", "gone.example does not exist", true, false},
		{"dns timeout", stageDial, dialErr(&net.DNSError{Err: "i/o timeout", Name: "slow.example", IsTimeout: true}),
			504, "dns_timeout", "the DNS server did not answer", false, false},
		{"refused", stageDial, dialErr(&os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}),
			502, "refused", "refused the connection", true, false},
		{"unreachable", stageDial, dialErr(&os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}),
			502, "unreachable", "no route to", true, false},
		{"dial timeout", stageDial, dialErr(os.ErrDeadlineExceeded),
			504, "timeout", "did not answer within", true, false},
		{"silent after the request", stageAnswer, os.ErrDeadlineExceeded,
			504, "no_answer", "did not start answering", true, false},
		{"hung up in the handshake", stageTLS, io.EOF,
			502, "closed", "during the TLS handshake", true, false},
		{"reset before answering", stageAnswer, &net.OpError{Op: "read", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}},
			502, "closed", "closed the connection without answering", true, false},
		{"bad certificate", stageTLS, x509.HostnameError{Certificate: &x509.Certificate{}, Host: "site.example"},
			502, "bad_certificate", "for another name", true, false},
		{"internal address", stageDial, dialErr(&refusedAddr{addr: netip.MustParseAddr("10.0.0.5")}),
			403, "private", "internal address (10.0.0.5)", false, true},
		{"threat list address", stageDial, dialErr(&refusedAddr{addr: netip.MustParseAddr("203.0.113.9"), list: "et_open"}),
			403, "threat_list", "threat list et_open", false, true},
	}
	for _, c := range cases {
		f := upstreamFailure(c.stage, "site.example", 443, c.err)
		if c.name == "no such name" || c.name == "dns timeout" {
			f = upstreamFailure(c.stage, c.err.(*net.OpError).Err.(*net.DNSError).Name, 443, c.err)
		}
		text := f.Text()
		if f.status != c.status || f.reason != c.reason || !strings.Contains(text, c.says) || f.block != c.block {
			t.Errorf("%s: %d %s %q", c.name, f.status, f.reason, text)
		}
		if strings.Contains(text, "filter itself is working") != c.site {
			t.Errorf("%s: says the filter works = %v, want %v: %q", c.name, !c.site, c.site, text)
		}
		if c.block && !strings.HasPrefix(text, "Blocked by the Silo network filter.") {
			t.Errorf("%s: a block not worded as one: %q", c.name, text)
		}
		// The resolv.conf server a lookup error names is not the one asked.
		if strings.Contains(text, "10.213.4.1") {
			t.Errorf("%s: names the stale nameserver: %q", c.name, text)
		}
	}
}

// filterAnswer sends one request through a plain proxy door in front of dial
// and reads what the box gets back.
func filterAnswer(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) *http.Response {
	t.Helper()
	s, err := NewState(testApply(t))
	if err != nil {
		t.Fatal(err)
	}
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	p := &Proxy{Client: func(_, _ netip.Addr) *Client { return b1 }, Guard: func() *Guard { return g }, Dial: dial}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go p.Serve(ctx, ln, ModeProxy)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET http://site.example:8080/ HTTP/1.1\r\nHost: site.example:8080\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestProxyNamesARefusedConnection(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := closed.Addr().String()
	closed.Close()
	resp := filterAnswer(t, func(ctx context.Context, n, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, n, addr)
	})
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 502 || resp.Header.Get("X-Silo-Filter") != "refused" ||
		!strings.Contains(string(body), "site.example on port 8080 refused the connection") {
		t.Fatalf("%d %v %q", resp.StatusCode, resp.Header, body)
	}
}

// A site that takes the request and then says nothing gets a 504 naming that,
// rather than holding the box until its own client gives up.
func TestProxyNamesASiteThatNeverAnswers(t *testing.T) {
	old := answerTimeout
	answerTimeout = 200 * time.Millisecond
	t.Cleanup(func() { answerTimeout = old })
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { silent.Close() })
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	resp := filterAnswer(t, func(context.Context, string, string) (net.Conn, error) {
		return net.Dial("tcp", silent.Addr().String())
	})
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 504 || resp.Header.Get("X-Silo-Filter") != "no_answer" ||
		!strings.Contains(string(body), "did not start answering") || !strings.Contains(string(body), "filter itself is working") {
		t.Fatalf("%d %v %q", resp.StatusCode, resp.Header, body)
	}
}
