package main

import (
	"context"
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
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return errors.New("key required")
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '_' {
			continue
		}
		return fmt.Errorf("bad key %q", s)
	}
	return nil
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
	dest, err := w.resolve("bot/screen.png")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := w.x11(ctx, "scrot", "-z", "-o", dest); err != nil {
		return "", err
	}
	ww, hh, err := pngSize(dest)
	if err != nil {
		return "", err
	}
	if ww != screenW || hh != screenH {
		return "", fmt.Errorf("screenshot is %dx%d, want %dx%d", ww, hh, screenW, screenH)
	}
	return w.browseFile("bot/screen.png")
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
	if err := w.xdotool(ctx, "type", "--clearmodifiers", "--delay", "1", "--", text); err != nil {
		return "", err
	}
	return "typed", nil
}

func (w *worker) key(ctx context.Context, name string) (string, error) {
	if err := validKey(name); err != nil {
		return "", err
	}
	if err := w.xdotool(ctx, "key", "--clearmodifiers", name); err != nil {
		return "", err
	}
	return "key " + name, nil
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
