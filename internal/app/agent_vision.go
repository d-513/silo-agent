package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"silo.agent/internal/llm"
)

const (
	lookPath     = "bot/screen.jpg"
	lookCoordLaw = "Image is 1600×900. Origin top-left. click(x,y) is in these pixels. The worker applies them with no scale."
)

// isLookImage reports whether a user turn carries a live desktop screenshot.
// The text is the coordinate law set by visionImage.
func isLookImage(m llm.Message) bool {
	return m.Role == llm.RoleUser && len(m.Images) > 0 && strings.HasPrefix(strings.TrimSpace(m.Text), "Image is 1600×900")
}

// pruneLookImages drops pixels from earlier screenshots so a long GUI run does
// not re-send every frame on each model call. Only the newest look is kept.
func pruneLookImages(msgs []llm.Message) {
	for i := range msgs {
		if isLookImage(msgs[i]) {
			msgs[i].Images = nil
			msgs[i].Text = "Earlier screenshot omitted — act on the current one."
		}
	}
}

// isLookTool reports whether a tool call's image is the desktop, not a
// presented file. Python and terminal can take a look as a side effect.
func isLookTool(name string) bool {
	switch name {
	case "look", "exec_python", "terminal":
		return true
	default:
		return false
	}
}

func visionImage(tool, url string) (string, llm.Image) {
	img := imageFromDataURL(url)
	if isLookTool(tool) {
		return lookCoordLaw, img
	}
	img.Detail = "high"
	return "You presented this image. Read the visible labels before you act.", img
}

// imageFromDataURL decodes a data: URL into a neutral image.
func imageFromDataURL(url string) llm.Image {
	const marker = ";base64,"
	i := strings.Index(url, marker)
	if !strings.HasPrefix(url, "data:") || i < 0 {
		return llm.Image{}
	}
	raw, err := base64.StdEncoding.DecodeString(url[i+len(marker):])
	if err != nil {
		return llm.Image{}
	}
	return llm.Image{Mime: strings.TrimPrefix(url[:i], "data:"), Data: raw}
}

func lookAck(raw string) string {
	return lookCoordLaw + " " + presentAck(lookPath, raw)
}

func presentImageURL(path, raw string) string {
	var row struct {
		Name      string `json:"name"`
		Data      string `json:"data"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal([]byte(raw), &row) != nil || row.Data == "" || row.Truncated {
		// A truncated payload is a broken image; never attach it.
		return ""
	}
	mime := imageMIME(path)
	if mime == "" {
		mime = imageMIME(row.Name)
	}
	if mime == "" {
		return ""
	}
	return "data:" + mime + ";base64," + row.Data
}

func imageMIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

// previewExts are the extensions the thread renders inline (keep in sync with
// web/src/fileKind.ts). Anything else has no preview, so a present of it
// should have gone through artifact instead.
var previewExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".svg": true, ".ico": true, ".avif": true,
	".pdf": true,
	".mp4": true, ".webm": true, ".ogv": true, ".mov": true,
	".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".flac": true, ".aac": true,
	".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".json": true, ".docx": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true, ".go": true, ".rs": true, ".rb": true,
	".java": true, ".kt": true, ".c": true, ".h": true, ".cpp": true, ".cc": true,
	".sh": true, ".bash": true, ".zsh": true, ".css": true, ".html": true, ".htm": true,
	".xml": true, ".yml": true, ".yaml": true, ".toml": true, ".sql": true,
	".txt": true, ".log": true, ".env": true, ".cfg": true, ".ini": true, ".conf": true,
	".gitignore": true, ".dockerfile": true,
}

func hasInlinePreview(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return true // extensionless files render as text
	}
	return previewExts[ext]
}

func presentAck(path, raw string) string {
	var row struct {
		Name      string `json:"name"`
		Truncated bool   `json:"truncated"`
		Size      int64  `json:"size"`
	}
	if json.Unmarshal([]byte(raw), &row) != nil || row.Name == "" {
		if botScratch(path) {
			return "seen. Scratch — you have the pixels; do not retype."
		}
		return "presented. Shown in the thread — do not retype it."
	}
	label := path
	if label == "" {
		label = row.Name
	}
	if botScratch(path) {
		msg := fmt.Sprintf("seen %s (%s). Scratch — you have the pixels; the human has a collapsed row.", label, formatSize(row.Size))
		if row.Truncated {
			msg += " Preview is truncated."
		}
		return msg
	}
	msg := fmt.Sprintf("presented %s (%s). Shown in the thread — do not retype it.", row.Name, formatSize(row.Size))
	if row.Truncated {
		msg += " Preview is truncated."
	}
	if !hasInlinePreview(row.Name) {
		msg += " This type has no inline preview — use artifact instead so the human gets a downloadable card."
	}
	return msg
}
