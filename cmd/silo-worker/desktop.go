package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	v1 "silo.agent/gen/silo/v1"
)

const (
	screenW = 1280
	screenH = 720
	display = ":1"
)

func screenPoint(x, y int) error {
	if x < 0 || x >= screenW || y < 0 || y >= screenH {
		return fmt.Errorf("(%d,%d) is outside 1280×720", x, y)
	}
	return nil
}

func clickButton(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "left":
		return "left", nil
	case "right":
		return "right", nil
	case "double":
		return "double", nil
	default:
		return "", errors.New("button must be left, right, or double")
	}
}

func validKey(s string) error {
	_, err := normalizeKey(s)
	return err
}

func looksLikeChord(s string) bool {
	t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if t == "" {
		return false
	}
	for _, p := range []string{"ctrl", "control", "ctl", "alt", "option", "shift", "super", "cmd", "command", "win", "meta"} {
		if strings.HasPrefix(t, p+"+") || strings.HasPrefix(t, p+"-") {
			return true
		}
	}
	return false
}

var keyAlias = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "ctl": "ctrl",
	"alt": "alt", "option": "alt",
	"shift": "shift",
	"super": "super", "cmd": "super", "command": "super", "win": "super", "meta": "super",
	"enter": "Return", "return": "Return",
	"esc": "Escape", "escape": "Escape",
	"tab":       "Tab",
	"backspace": "BackSpace", "bksp": "BackSpace",
	"delete": "Delete", "del": "Delete",
	"space": "space",
	"left":  "Left", "right": "Right", "up": "Up", "down": "Down",
	"pageup": "Page_Up", "page_up": "Page_Up",
	"pagedown": "Page_Down", "page_down": "Page_Down",
	"home": "Home", "end": "End",
	"minus": "minus", "plus": "plus", "equal": "equal",
}

func normalizeKey(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 80 {
		return "", errors.New("key required")
	}
	t := strings.ReplaceAll(s, " ", "")
	if looksLikeChord(t) {
		t = strings.ReplaceAll(t, "-", "+")
	}
	var parts []string
	for _, raw := range strings.Split(t, "+") {
		p := strings.TrimSpace(raw)
		if p == "" {
			return "", fmt.Errorf("bad key %q", s)
		}
		low := strings.ToLower(p)
		if alias, ok := keyAlias[low]; ok {
			parts = append(parts, alias)
			continue
		}
		if len(low) >= 2 && (low[0] == 'f' || low[0] == 'F') {
			n := low[1:]
			if _, err := strconv.Atoi(n); err == nil && n != "" {
				parts = append(parts, "F"+n)
				continue
			}
		}
		if len(p) == 1 {
			parts = append(parts, strings.ToLower(p))
			continue
		}
		for _, r := range p {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				continue
			}
			return "", fmt.Errorf("bad key %q", s)
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("bad key %q", s)
	}
	return strings.Join(parts, "+"), nil
}

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func (w *worker) look(ctx context.Context) (string, error) {
	if err := w.lookShot(ctx); err != nil {
		return "", err
	}
	return w.browseFile("bot/screen.png")
}

func (w *worker) lookShot(ctx context.Context) error {
	dest, err := w.resolve("bot/screen.png")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := w.x11(ctx, "scrot", "-z", "-o", dest); err != nil {
		return err
	}
	ww, hh, err := pngSize(dest)
	if err != nil {
		return err
	}
	if ww != screenW || hh != screenH {
		return fmt.Errorf("screenshot is %dx%d, want %dx%d", ww, hh, screenW, screenH)
	}
	return nil
}

func (w *worker) click(ctx context.Context, c *v1.ClickCmd) (string, error) {
	x, y := int(c.GetX()), int(c.GetY())
	if err := screenPoint(x, y); err != nil {
		return "", err
	}
	btn, err := clickButton(c.GetButton())
	if err != nil {
		return "", err
	}
	args := []string{"mousemove", "--", strconv.Itoa(x), strconv.Itoa(y)}
	switch btn {
	case "right":
		args = append(args, "click", "3")
	case "double":
		args = append(args, "click", "--repeat", "2", "1")
	default:
		args = append(args, "click", "1")
	}
	if err := w.xdotool(ctx, args...); err != nil {
		return "", err
	}
	return fmt.Sprintf("clicked %d,%d %s", x, y, btn), nil
}

func (w *worker) typeText(ctx context.Context, text string) (string, error) {
	if text == "" {
		return "", errors.New("text required")
	}
	if looksLikeChord(text) {
		return w.key(ctx, text)
	}
	if err := w.xdotool(ctx, "type", "--clearmodifiers", "--delay", "1", "--", text); err != nil {
		return "", err
	}
	return "typed", nil
}

func (w *worker) key(ctx context.Context, name string) (string, error) {
	chord, err := normalizeKey(name)
	if err != nil {
		return "", err
	}
	args := []string{"key"}
	if !strings.Contains(chord, "+") {
		args = append(args, "--clearmodifiers")
	}
	args = append(args, chord)
	if err := w.xdotool(ctx, args...); err != nil {
		return "", err
	}
	return "key " + chord, nil
}

func (w *worker) scroll(ctx context.Context, c *v1.ScrollCmd) (string, error) {
	x, y := int(c.GetX()), int(c.GetY())
	if err := screenPoint(x, y); err != nil {
		return "", err
	}
	dy := int(c.GetDy())
	if dy == 0 {
		return "", errors.New("dy required")
	}
	n := dy
	btn := "5"
	if dy < 0 {
		n = -dy
		btn = "4"
	}
	if n > 40 {
		n = 40
	}
	args := []string{"mousemove", "--", strconv.Itoa(x), strconv.Itoa(y), "click", "--repeat", strconv.Itoa(n), btn}
	if err := w.xdotool(ctx, args...); err != nil {
		return "", err
	}
	return fmt.Sprintf("scrolled %d at %d,%d", dy, x, y), nil
}

func (w *worker) xdotool(ctx context.Context, args ...string) error {
	return w.x11(ctx, "xdotool", args...)
}

func (w *worker) x11(ctx context.Context, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = w.workspace
	c.Env = w.childEnv("")
	if !envHas(c.Env, "DISPLAY=") {
		c.Env = append(c.Env, "DISPLAY="+display)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %s", name, msg)
	}
	return nil
}

func (w *worker) runDesktop(ctx context.Context, action string, args map[string]any) (string, error) {
	switch action {
	case "look":
		if err := w.lookShot(ctx); err != nil {
			return "", err
		}
		return `{"path":"bot/screen.png"}`, nil
	case "click":
		return w.click(ctx, &v1.ClickCmd{X: int32(intArg(args, "x")), Y: int32(intArg(args, "y")), Button: strArg(args, "button")})
	case "type":
		return w.typeText(ctx, strArg(args, "text"))
	case "key":
		return w.key(ctx, strArg(args, "name"))
	case "scroll":
		return w.scroll(ctx, &v1.ScrollCmd{X: int32(intArg(args, "x")), Y: int32(intArg(args, "y")), Dy: int32(intArg(args, "dy"))})
	default:
		return "", fmt.Errorf("unknown desktop action %s", action)
	}
}

func strArg(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func intArg(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}
