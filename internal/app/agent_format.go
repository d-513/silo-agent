package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// readResult is the Worker's sliced-read payload (see worker readFileSlice).
type readResult struct {
	Content    string `json:"content"`
	NextOffset int    `json:"next_offset"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated"`
}

// formatRead numbers the already-sliced Worker read and appends a continuation
// hint. A non-JSON body (older worker) falls through unchanged.
func formatRead(raw string, offset int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	res := readResult{}
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			return raw
		}
	} else {
		res.Content = raw
	}
	content := strings.TrimRight(res.Content, "\n")
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if offset < 1 {
		offset = 1
	}
	last := offset + len(lines) - 1
	width := len(fmt.Sprintf("%d", max(max(last, res.TotalLines), 1)))
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%*d|%s\n", width, offset+i, line)
	}
	if res.Truncated {
		if res.TotalLines > 0 {
			fmt.Fprintf(&b, "… lines %d–%d of %d; read offset=%d for more\n", offset, last, res.TotalLines, res.NextOffset)
		} else {
			fmt.Fprintf(&b, "… lines %d–%d; read offset=%d for more\n", offset, last, res.NextOffset)
		}
	} else if offset > 1 || res.TotalLines > last {
		fmt.Fprintf(&b, "… lines %d–%d of %d\n", offset, last, res.TotalLines)
	}
	return b.String()
}

func capHits(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n…%d more hits", len(lines)-n)
}
