package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Signing in is the binary's own doing: `claude auth login` keeps the account
// in its config directory, and `claude setup-token` issues a token good for a
// year. Both print a page to open and wait for the code that page shows; the
// control plane only carries the two between the binary and the operator.

const (
	claudeMethodLogin = "login"
	claudeMethodToken = "token"
	// claudeSignInWait is how long a begun sign-in waits for its code.
	claudeSignInWait = 10 * time.Minute
)

var (
	claudeURL   = regexp.MustCompile(`https://[^\s"']+oauth/authorize[^\s"']*`)
	claudeToken = regexp.MustCompile(`sk-ant-oat[0-9]{2}-[A-Za-z0-9_\-]{20,}`)
	// ansi matches the terminal control sequences setup-token draws with.
	ansiForward = regexp.MustCompile(`\x1b\[[0-9]*C`)
	// claudeRefused is how the binary says a code was not taken.
	claudeRefused = regexp.MustCompile(`(?i)oauth error|invalid code|press enter to retry`)
	ansi          = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[=>]`)
)

func (c *claudeClient) SignInMethods() []SignInMethod {
	return []SignInMethod{
		{ID: claudeMethodLogin, Label: "Sign in", Description: "Signs this server in to your Claude account. The sign-in is kept in the provider's directory and renewed by Claude Code."},
		{ID: claudeMethodToken, Label: "Create a long-lived token", Description: "Has Claude Code issue a token good for a year and saves it as the provider's Long-lived token. Nothing but the token is kept."},
	}
}

func (c *claudeClient) AccountStatus(ctx context.Context) (AccountStatus, error) {
	cmd, err := c.conf.command("auth", "status", "--json")
	if err != nil {
		return AccountStatus{Problem: err.Error()}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Signed out is exit 1 with the same JSON, so the output decides.
	out, runErr := runClaude(ctx, cmd)
	var st struct {
		LoggedIn     bool   `json:"loggedIn"`
		AuthMethod   string `json:"authMethod"`
		Email        string `json:"email"`
		Subscription string `json:"subscriptionType"`
		Expires      string `json:"loginExpiresAt"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &st) != nil {
		problem := "Claude Code did not report its sign-in"
		if runErr != nil {
			problem += ": " + runErr.Error()
		}
		return AccountStatus{Problem: problem}, nil
	}
	res := AccountStatus{SignedIn: st.LoggedIn, Method: st.AuthMethod, Email: st.Email, Plan: st.Subscription}
	if t, err := time.Parse(time.RFC3339, st.Expires); err == nil {
		res.Expires = t
	}
	return res, nil
}

// runClaude runs a short command of the binary and returns its stdout.
func runClaude(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.Bytes(), err
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return out.Bytes(), ctx.Err()
	}
}

// claudeSignIn is a sign-in waiting for its code.
type claudeSignIn struct {
	method string
	cmd    *exec.Cmd
	in     io.Writer
	closer io.Closer
	done   chan struct{}
	timer  *time.Timer

	mu  sync.Mutex
	out bytes.Buffer
}

func (s *claudeSignIn) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Write(p)
}

// text is what the binary has shown, without terminal control sequences.
func (s *claudeSignIn) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The terminal interface moves the cursor where a space would be.
	text := ansiForward.ReplaceAllString(strings.ToValidUTF8(s.out.String(), ""), " ")
	return ansi.ReplaceAllString(text, "")
}

func (s *claudeSignIn) end() {
	s.timer.Stop()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.closer != nil {
		_ = s.closer.Close()
	}
}

// await polls what the binary has shown until find answers, the binary
// exits, or the wait runs out.
func (s *claudeSignIn) await(ctx context.Context, wait time.Duration, find func(text string) string) (string, bool) {
	deadline := time.After(wait)
	for {
		if got := find(s.text()); got != "" {
			return got, true
		}
		select {
		case <-s.done:
			got := find(s.text())
			return got, got != ""
		case <-deadline:
			return "", false
		case <-ctx.Done():
			return "", false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// claudeSignIns holds the one sign-in each setup may have in progress.
var claudeSignIns = struct {
	sync.Mutex
	m map[string]*claudeSignIn
}{m: map[string]*claudeSignIn{}}

func (c *claudeClient) BeginSignIn(ctx context.Context, method string) (SignInStep, error) {
	if method == "" {
		method = claudeMethodLogin
	}
	args := []string{"auth", "login", "--claudeai"}
	switch method {
	case claudeMethodLogin:
	case claudeMethodToken:
		args = []string{"setup-token"}
	default:
		return SignInStep{}, fmt.Errorf("unknown sign-in method %q", method)
	}
	// A token in the environment would answer for the account being signed in.
	conf := c.conf
	conf.token = ""
	cmd, err := conf.command(args...)
	if err != nil {
		return SignInStep{}, err
	}
	s := &claudeSignIn{method: method, cmd: cmd, done: make(chan struct{})}
	if method == claudeMethodToken {
		// setup-token draws a terminal interface and prints nothing without
		// one. The terminal is wide so the address is on one line.
		cmd.Env = append(cmd.Env, "TERM=xterm")
		tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 4000})
		if err != nil {
			return SignInStep{}, fmt.Errorf("start Claude Code: %w", err)
		}
		s.in, s.closer = tty, tty
		go func() {
			_, _ = io.Copy(s, tty)
			_ = cmd.Wait()
			close(s.done)
		}()
	} else {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return SignInStep{}, err
		}
		cmd.Stdout, cmd.Stderr = s, s
		if err := cmd.Start(); err != nil {
			return SignInStep{}, fmt.Errorf("start Claude Code: %w", err)
		}
		s.in, s.closer = stdin, stdin
		go func() {
			_ = cmd.Wait()
			close(s.done)
		}()
	}
	s.timer = time.AfterFunc(claudeSignInWait, func() { c.dropSignIn(s) })

	url, ok := s.await(ctx, 30*time.Second, func(text string) string { return claudeURL.FindString(text) })
	if !ok {
		s.end()
		return SignInStep{}, fmt.Errorf("Claude Code did not show a sign-in page: %s", lastLines(s.text(), 3))
	}
	claudeSignIns.Lock()
	if old := claudeSignIns.m[c.conf.dir]; old != nil {
		old.end()
	}
	claudeSignIns.m[c.conf.dir] = s
	claudeSignIns.Unlock()
	return SignInStep{URL: url}, nil
}

func (c *claudeClient) dropSignIn(s *claudeSignIn) {
	claudeSignIns.Lock()
	if claudeSignIns.m[c.conf.dir] == s {
		delete(claudeSignIns.m, c.conf.dir)
	}
	claudeSignIns.Unlock()
	s.end()
}

func (c *claudeClient) FinishSignIn(ctx context.Context, code string) (map[string]string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("paste the code the sign-in page showed")
	}
	// A code is one line: nothing typed here may reach the binary as more.
	if strings.ContainsAny(code, "\r\n\x1b") {
		return nil, errors.New("that is not a sign-in code")
	}
	claudeSignIns.Lock()
	s := claudeSignIns.m[c.conf.dir]
	delete(claudeSignIns.m, c.conf.dir)
	claudeSignIns.Unlock()
	if s == nil {
		return nil, errors.New("the sign-in ran out; start it again")
	}
	defer s.end()
	enter := "\n"
	if s.method == claudeMethodToken {
		enter = "\r"
	}
	if _, err := io.WriteString(s.in, code+enter); err != nil {
		return nil, errors.New("the sign-in ran out; start it again")
	}
	if s.method == claudeMethodToken {
		// What was shown before the code is not the answer to it.
		before := len(s.text())
		got, _ := s.await(ctx, 90*time.Second, func(text string) string {
			if token := claudeToken.FindString(text); token != "" {
				return token
			}
			if before <= len(text) && claudeRefused.MatchString(text[before:]) {
				return "refused"
			}
			return ""
		})
		token := claudeToken.FindString(got)
		if token == "" {
			return nil, fmt.Errorf("Claude Code issued no token: %s", claudeSaid(s.text()))
		}
		c.forgetModels()
		return map[string]string{"oauth_token": token}, nil
	}
	select {
	case <-s.done:
	case <-time.After(90 * time.Second):
		return nil, errors.New("Claude Code did not finish the sign-in")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if !s.cmd.ProcessState.Success() {
		return nil, fmt.Errorf("the sign-in failed: %s", claudeSaid(s.text()))
	}
	c.forgetModels()
	return nil, nil
}

// claudeSaid is the end of what the binary wrote, without the page address
// (it carries the sign-in's challenge).
func claudeSaid(text string) string {
	text = claudeURL.ReplaceAllString(text, "")
	var keep []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keep = append(keep, line)
		}
	}
	if len(keep) == 0 {
		return "it said nothing"
	}
	// Its refusal, when it wrote one, over the prompt that follows it.
	for i := len(keep) - 1; i >= 0; i-- {
		if claudeRefused.MatchString(keep[i]) || strings.Contains(strings.ToLower(keep[i]), "fail") {
			return keep[i]
		}
	}
	return lastLines(strings.Join(keep, "\n"), 2)
}

// forgetModels drops the cached model list: it depends on the account.
func (c *claudeClient) forgetModels() {
	claudeModelCache.Lock()
	claudeModelCache.m = map[string]*claudeModelList{}
	claudeModelCache.Unlock()
}

func (c *claudeClient) SignOut(ctx context.Context) ([]string, error) {
	conf := c.conf
	conf.token = ""
	cmd, err := conf.command("auth", "logout")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := runClaude(ctx, cmd); err != nil {
		// Nothing stored is signed out already.
		if _, statErr := os.Stat(conf.configDir()); statErr != nil {
			return []string{"oauth_token"}, nil
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, fmt.Errorf("sign out of Claude: %w", err)
		}
	}
	c.forgetModels()
	return []string{"oauth_token"}, nil
}
