package config

import "strings"

// Images says when the engine fetches an image from its registry.
type Images struct {
	// Pull is missing (the default), always or never; see PullPolicy.
	Pull string `koanf:"pull"`
}

// Pull policies for images.pull.
const (
	// PullMissing pulls an image only when the engine does not have it: right
	// for a release's images, whose tags never move.
	PullMissing = "missing"
	// PullAlways asks the registry again at start, every hour and before a
	// create, for a tag that moves (an operator's :master or :latest).
	PullAlways = "always"
	// PullNever never asks a registry: the images are loaded by hand.
	PullNever = "never"
)

// PullPolicy is images.pull, an unknown value reading as missing.
func (c Config) PullPolicy() string {
	switch p := strings.ToLower(strings.TrimSpace(c.Images.Pull)); p {
	case PullAlways, PullNever:
		return p
	}
	return PullMissing
}

// fillImages puts this build's image back where silo.yaml left one empty
// (the Admin form's field cleared): an empty reference is no image at all.
func (c *Config) fillImages() {
	if strings.TrimSpace(c.BotImage) == "" {
		c.BotImage = DefaultBotImage()
	}
	if strings.TrimSpace(c.MCPStdioImage) == "" {
		c.MCPStdioImage = DefaultMCPStdioImage()
	}
}

// isImageKey names the settings an emptied Admin field takes out of
// silo.yaml, so the image follows the release again (Store.Patch).
func isImageKey(k string) bool {
	switch k {
	case "bot_image", "mcp_stdio_image", "drives.image", "egress.image":
		return true
	}
	return false
}
