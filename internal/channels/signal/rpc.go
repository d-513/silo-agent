package signal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"silo.agent/internal/channels"
)

const (
	// rpcTimeout bounds an ordinary call; sending and fetching a file get
	// fileTimeout, since the bytes travel inside the JSON.
	rpcTimeout  = 30 * time.Second
	fileTimeout = 5 * time.Minute
	// maxAnswer is a safety net under the host's own cap on what it saves: an
	// attachment arrives as base64 inside one JSON answer.
	maxAnswer = 512 << 20
	// maxEventLine bounds one line of the event stream. Attachments are not
	// inlined there, so a line is a message's text and its metadata.
	maxEventLine = 8 << 20

	// signal-cli's JSON-RPC error codes that this adapter words itself.
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// daemon is one signal-cli daemon and the account this channel uses on it.
// Every connection goes through the operator's policy for addresses a user
// typed (cfg.Net): the daemon's address is one.
type daemon struct {
	base    string // http://host:port, no trailing slash
	account string // +491701234567
	rpc     *http.Client
	// stream has no overall timeout: the event stream stays open for as long
	// as the channel runs.
	stream *http.Client
}

var numberRe = regexp.MustCompile(`^\+[1-9][0-9]{5,14}$`)

// cleanNumber reads a phone number as people write it (+49 170 123-45-67).
func cleanNumber(s string) string {
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "", "\u00a0", "").Replace(strings.TrimSpace(s))
	if strings.HasPrefix(s, "00") {
		s = "+" + s[2:]
	}
	return s
}

// daemonURL reads the daemon's address as the owner may paste it: with or
// without the scheme, a trailing slash, or one of the API's own paths.
func daemonURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("the daemon's address is required, e.g. http://127.0.0.1:8080")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not an address — it looks like http://127.0.0.1:8080", s)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("the daemon speaks HTTP: the address starts with http:// (not %s://)", u.Scheme)
	}
	path := strings.TrimRight(u.Path, "/")
	if i := strings.Index(path, "/api/v1"); i >= 0 {
		path = path[:i]
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// newDaemon reads the channel's settings. The caller closes it.
func newDaemon(cfg channels.Config) (*daemon, error) {
	base, err := daemonURL(cfg.Get("daemon_url"))
	if err != nil {
		return nil, err
	}
	account := cleanNumber(cfg.Get("account"))
	switch {
	case account == "":
		return nil, errors.New("the account is required: the phone number the daemon is registered as, e.g. +491701234567")
	case !numberRe.MatchString(account):
		return nil, fmt.Errorf("%q is not a phone number in international form, e.g. +491701234567", cfg.Get("account"))
	}
	t := cfg.Net.Transport()
	return &daemon{base: base, account: account, rpc: &http.Client{Transport: t}, stream: &http.Client{Transport: t}}, nil
}

func (d *daemon) close() { d.rpc.CloseIdleConnections() }

// rpcError is the daemon's own refusal of a call.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return "signal-cli: " + e.Message }

func isCode(err error, code int) bool {
	var re *rpcError
	return errors.As(err, &re) && re.Code == code
}

// call makes one JSON-RPC request for the account. params may be nil; out,
// when not nil, takes the result.
func (d *daemon) call(ctx context.Context, timeout time.Duration, method string, params map[string]any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// A daemon started without -a serves several accounts and needs to be
	// told which; one started with -a ignores the parameter.
	p := map[string]any{"account": d.account}
	for k, v := range params {
		p[k] = v
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": method, "method": method, "params": p})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/api/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.rpc.Do(req)
	if err != nil {
		return d.unreachable(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return d.unreachable(err)
	}
	if len(raw) > maxAnswer {
		return fmt.Errorf("the daemon's answer to %s is over %d MB", method, maxAnswer>>20)
	}
	var ans struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		if err := statusError(res.StatusCode); err != nil {
			return err
		}
		return fmt.Errorf("%s did not answer as a signal-cli daemon does — check the address", d.base)
	}
	if ans.Error != nil {
		if ans.Error.Code == codeInvalidParams && strings.Contains(strings.ToLower(ans.Error.Message), "account") {
			return channels.Permanent(d.noAccount())
		}
		return ans.Error
	}
	if out == nil || len(ans.Result) == 0 {
		return nil
	}
	return json.Unmarshal(ans.Result, out)
}

func (d *daemon) noAccount() error {
	return fmt.Errorf("the signal-cli daemon has no account %s — register or link that number there (signal-cli -a %s register, or signal-cli link), or correct the number", d.account, d.account)
}

// statusError words the HTTP answers the daemon gives outside JSON-RPC.
func statusError(code int) error {
	switch {
	case code == http.StatusMisdirectedRequest:
		// signal-cli refuses a Host header it was not started to listen on.
		return errors.New("the signal-cli daemon refused the address it was called by — use the host and port given to its --http option")
	case code == http.StatusNotFound:
		return errors.New("that address answers, but not as a signal-cli daemon (start it with: signal-cli daemon --http HOST:PORT)")
	case code >= 400:
		return fmt.Errorf("the signal-cli daemon answered HTTP %d", code)
	}
	return nil
}

// unreachable words a connection that failed, without the request's URL
// repeated around it.
func (d *daemon) unreachable(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("cannot reach the signal-cli daemon at %s: %w", d.base, err)
}

// check asks whether the daemon is up and knows the account.
func (d *daemon) check(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, d.base+"/api/v1/check", nil)
	if err != nil {
		return err
	}
	res, err := d.rpc.Do(req)
	if err != nil {
		return d.unreachable(err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	res.Body.Close()
	if err := statusError(res.StatusCode); err != nil {
		return err
	}
	var accounts []struct {
		Number string `json:"number"`
	}
	err = d.call(ctx, rpcTimeout, "listAccounts", nil, &accounts)
	if isCode(err, codeMethodNotFound) {
		// A daemon started with -a serves one account and cannot list it.
		// A call that needs the account still proves the daemon speaks.
		return d.call(ctx, rpcTimeout, "version", nil, nil)
	}
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if a.Number == d.account {
			return nil
		}
	}
	return channels.Permanent(d.noAccount())
}

// event is one entry of the daemon's event stream.
type event struct {
	ID   string
	Name string
	Data []byte
}

// events reads the Server-Sent Events stream until it ends, handing each
// event to fn. after is the id of the last event already read, so a
// reconnect gets what arrived meanwhile (the daemon keeps the last 1000).
// opened is called once the daemon has accepted the stream.
func (d *daemon) events(ctx context.Context, after string, opened func(), fn func(event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/api/v1/events?account="+url.QueryEscape(d.account), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if after != "" {
		req.Header.Set("Last-Event-ID", after)
	}
	res, err := d.stream.Do(req)
	if err != nil {
		return d.unreachable(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusBadRequest {
		// The events endpoint answers 400 for an account it does not have.
		return channels.Permanent(d.noAccount())
	}
	if err := statusError(res.StatusCode); err != nil {
		return err
	}
	opened()
	r := bufio.NewReaderSize(res.Body, 64<<10)
	var ev event
	for {
		line, err := readLine(r)
		if err != nil {
			if err == io.EOF {
				return errors.New("the daemon closed the event stream")
			}
			return d.unreachable(err)
		}
		switch {
		case line == "":
			if len(ev.Data) > 0 {
				fn(ev)
			}
			ev = event{ID: ev.ID} // the last id stands until another is given
		case strings.HasPrefix(line, ":"):
			// a keep-alive comment
		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "id":
				ev.ID = value
			case "event":
				ev.Name = value
			case "data":
				if len(ev.Data) > 0 {
					ev.Data = append(ev.Data, '\n')
				}
				ev.Data = append(ev.Data, value...)
			}
		}
	}
}

// readLine reads one line without its ending, refusing one past maxEventLine.
func readLine(r *bufio.Reader) (string, error) {
	var buf []byte
	for {
		part, err := r.ReadSlice('\n')
		buf = append(buf, part...)
		if len(buf) > maxEventLine {
			return "", errors.New("an event is too long to read")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(buf), "\r\n"), nil
	}
}
