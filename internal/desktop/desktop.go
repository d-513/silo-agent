// Package desktop is what the CP and the Bot worker both need to agree on about
// the Bot's X11 desktop: its size and which code needs the headed browser. The
// Xvfb geometry is also set in botimage/start.sh and stated in the prompts.
package desktop

import (
	"fmt"
	"strings"
)

// Screen size in pixels. Screenshots and click coordinates share it unscaled.
const (
	Width  = 1600
	Height = 900
)

// CheckPoint reports whether (x, y) lies on the screen.
func CheckPoint(x, y int) error {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return fmt.Errorf("(%d,%d) is outside %d×%d", x, y, Width, Height)
	}
	return nil
}

// WantsChromium reports whether Python code needs the headed browser on :9222
// (Playwright or silo_runtime.chrome_page), so it is opened before the code runs.
func WantsChromium(code string) bool {
	s := strings.ToLower(code)
	return strings.Contains(s, "chrome_page") ||
		strings.Contains(s, "playwright") ||
		strings.Contains(s, "connect_over_cdp")
}
