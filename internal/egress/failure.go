package egress

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

// Stages of a request at which the origin can fail.
const (
	stageDial   = "dial"
	stageTLS    = "tls"
	stageSend   = "send"
	stageAnswer = "answer"
	stageBody   = "body"
)

// answerTimeout is how long an origin has, once the request is sent, to start
// answering (its status line and headers). A variable for tests.
var answerTimeout = 2 * time.Minute

// failure is what the box is told when the gateway could not get an answer
// from the origin, worded so whoever reads it (a Bot, mostly) knows what went
// wrong and whose side it is on. A vague "the filter could not reach" reads as
// the filter being broken, and a Bot that believes that stops trying sites
// that are only down, or tells its owner the network is broken.
type failure struct {
	status int
	// reason is the X-Silo-Filter header: a stable word for code to branch
	// on.
	reason string
	// text is the sentence after "Silo network filter:".
	text string
	// site is true when the fault is the origin's (down, refusing, cut off,
	// a bad certificate): the answer then says the filter is working.
	site bool
	// block is true for an address the filter itself refused at dial: it is
	// worded and reported as a block.
	block bool
}

// upstreamFailure reads err, met at stage while reaching host:port.
func upstreamFailure(stage, host string, port int, err error) failure {
	where := fmt.Sprintf("%s on port %d", host, port)
	var ra *refusedAddr
	if errors.As(err, &ra) {
		if ra.list != "" {
			return failure{status: http.StatusForbidden, reason: "threat_list", block: true,
				text: fmt.Sprintf("%s resolves to %s, which is on threat list %s.", host, ra.addr, ra.list)}
		}
		return failure{status: http.StatusForbidden, reason: "private", block: true,
			text: fmt.Sprintf("%s resolves to an internal address (%s), which Bots may not reach.", host, ra.addr)}
	}
	var de *net.DNSError
	if errors.As(err, &de) {
		switch {
		case de.IsNotFound:
			return failure{status: http.StatusBadGateway, reason: "dns_not_found", site: true,
				text: host + " does not exist: DNS has no record of that name. Check the spelling, or the site may have gone."}
		case de.IsTimeout:
			return failure{status: http.StatusGatewayTimeout, reason: "dns_timeout",
				text: "looking up " + host + " timed out: the DNS server did not answer. Try again in a moment."}
		}
		return failure{status: http.StatusBadGateway, reason: "dns_failed",
			text: "looking up " + host + " failed: " + de.Err + ". Try again in a moment."}
	}
	var certErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &certErr) || errors.As(err, &hostErr) || errors.As(err, &invalid) || strings.Contains(err.Error(), "x509:") {
		return failure{status: http.StatusBadGateway, reason: "bad_certificate", site: true,
			text: fmt.Sprintf("%s's HTTPS certificate is not valid (%s): it is expired, self-signed or for another name. The filter checks every site's certificate and does not pass one that fails.", host, certProblem(err))}
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return failure{status: http.StatusBadGateway, reason: "refused", site: true,
			text: where + " refused the connection: nothing is listening there."}
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return failure{status: http.StatusBadGateway, reason: "unreachable", site: true,
			text: "there is no route to " + where + "."}
	case isTimeout(err):
		if stage == stageAnswer {
			return failure{status: http.StatusGatewayTimeout, reason: "no_answer", site: true,
				text: fmt.Sprintf("%s accepted the request but did not start answering within %s.", host, answerTimeout)}
		}
		return failure{status: http.StatusGatewayTimeout, reason: "timeout", site: true,
			text: fmt.Sprintf("%s did not answer within %s: the site is down, overloaded, or does not take connections on that port.", where, originTimeout)}
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		switch stage {
		case stageTLS:
			return failure{status: http.StatusBadGateway, reason: "closed", site: true,
				text: host + " closed the connection during the TLS handshake. Some sites refuse automated clients this way; others are just failing."}
		case stageSend:
			return failure{status: http.StatusBadGateway, reason: "closed", site: true,
				text: host + " closed the connection while the request was being sent."}
		case stageBody:
			return failure{status: http.StatusBadGateway, reason: "closed", site: true,
				text: host + " closed the connection as its answer began."}
		}
		return failure{status: http.StatusBadGateway, reason: "closed", site: true,
			text: host + " closed the connection without answering."}
	}
	switch stage {
	case stageTLS:
		return failure{status: http.StatusBadGateway, reason: "tls_failed", site: true,
			text: "the TLS handshake with " + host + " failed: " + plainErr(err) + "."}
	case stageAnswer:
		return failure{status: http.StatusBadGateway, reason: "bad_answer", site: true,
			text: host + " answered with something that is not valid HTTP: " + plainErr(err) + "."}
	case stageSend:
		return failure{status: http.StatusBadGateway, reason: "send_failed", site: true,
			text: "sending the request to " + host + " failed: " + plainErr(err) + "."}
	}
	return failure{status: http.StatusBadGateway, reason: "unreachable", site: true,
		text: "could not connect to " + where + ": " + plainErr(err) + "."}
}

// Text is the body the box reads.
func (f failure) Text() string {
	if f.block {
		return "Blocked by the Silo network filter. " + f.text
	}
	s := "Silo network filter: " + f.text
	if f.site {
		s += " This is the site's doing; the filter itself is working."
	}
	return s
}

func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// certProblem is a short reading of a verification error.
func certProblem(err error) string {
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &hostErr):
		return "it is for another name"
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "expired or not yet valid"
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return "signed by an authority nobody trusts"
	}
	return plainErr(err)
}

// plainErr is an error's text without the net package's layers of operation
// and address, which repeat what the sentence already says.
func plainErr(err error) string {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Err != nil {
		err = oe.Err
	}
	s := err.Error()
	s = strings.TrimPrefix(s, "tls: ")
	s = strings.TrimPrefix(s, "remote error: tls: ")
	return strings.TrimSuffix(s, ".")
}

// writeFailure answers the box for a failure, marked as the filter's own
// answer so code can tell it from the site's.
func writeFailure(w io.Writer, f failure) {
	writeAnswer(w, f.status, f.reason, f.Text())
}
