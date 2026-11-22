package siloosd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf8"
)

// Runner runs one command, never through a shell, and answers its standard
// output. An error carries what the command said on standard error.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Exec is the real Runner.
type Exec struct{}

// commands is every program siloosd runs, by absolute path: the PATH of
// whoever started it is not believed.
var commands = map[string]string{
	"nmcli":       "/usr/bin/nmcli",
	"busctl":      "/usr/bin/busctl",
	"bootc":       "/usr/bin/bootc",
	"hostnamectl": "/usr/bin/hostnamectl",
	"timedatectl": "/usr/bin/timedatectl",
	"journalctl":  "/usr/bin/journalctl",
	"systemctl":   "/usr/bin/systemctl",
}

func (Exec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, ok := commands[name]
	if !ok {
		return nil, fmt.Errorf("siloosd runs no %q", name)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	// A fixed, plain environment: no locale (the parsers read English), no
	// pager, no colour.
	cmd.Env = []string{"PATH=/usr/bin:/usr/sbin", "LC_ALL=C", "SYSTEMD_PAGER=", "SYSTEMD_COLORS=0", "NO_COLOR=1"}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			return out.Bytes(), fmt.Errorf("%s: %w", name, err)
		}
		return out.Bytes(), fmt.Errorf("%s: %s", name, clip(msg, 600))
	}
	return out.Bytes(), nil
}

// clip keeps at most n bytes of s, on a rune boundary, as valid UTF-8.
func clip(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// tail keeps the last n bytes of s, on a rune boundary.
func tail(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…" + s[cut:]
}
