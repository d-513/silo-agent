package workspace

import (
	"path/filepath"
	"strings"
)

// MIME is the content type served for a workspace file by its extension; "" when
// the extension is not one the UI previews or the channels attach.
func MIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".ogv":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".zip":
		return "application/zip"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".tar":
		return "application/x-tar"
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt", ".log":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	default:
		return ""
	}
}

// Passive reports whether a browser can show a file of this type in place
// without it acting as a page: pixels, sound, a PDF, plain text. Everything
// else a Bot's workspace holds (HTML, SVG, anything unknown) is only ever a
// download, because whoever wrote it would otherwise run script in the
// control plane's own origin, signed in as the person looking.
func Passive(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/avif", "image/x-icon",
		"application/pdf", "text/plain":
		return true
	}
	return strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/")
}

// Untrusted sets the headers every response made of somebody else's bytes
// carries: the declared type is final, and a type that could act as a page is
// sandboxed with no origin even if something does render it.
func Untrusted(h interface{ Set(key, value string) }, mime string) {
	h.Set("X-Content-Type-Options", "nosniff")
	if !Passive(mime) {
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	}
}
