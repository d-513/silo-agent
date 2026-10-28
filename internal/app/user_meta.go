package app

import (
	"encoding/json"
	"fmt"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/textx"
)

// A user message's Meta holds what it carried besides its text: the files the
// human uploaded and the quotes they pointed at. Replay builds the model's
// turn from it (userTurnText), the same bytes a live run read.

type eventAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
}

type eventQuote struct {
	Source  string `json:"source"`
	Ref     string `json:"ref,omitempty"`
	Label   string `json:"label,omitempty"`
	Text    string `json:"text,omitempty"`
	Locator string `json:"locator,omitempty"`
	Image   string `json:"image,omitempty"`
}

type userMeta struct {
	Files  []eventAttachment `json:"files,omitempty"`
	Quotes []eventQuote      `json:"quotes,omitempty"`
	Spoken *spokenMeta       `json:"spoken,omitempty"`
}

// spokenMeta marks a turn said on a voice call (run.Spoken).
type spokenMeta struct {
	CallID      string `json:"call_id"`
	Interrupted string `json:"interrupted,omitempty"`
}

// maxInterrupted caps the cut-off words a spoken turn carries.
const maxInterrupted = 600

// The caps on what one message may quote.
const (
	maxQuotes     = 8
	maxQuoteText  = 4000
	maxQuoteLabel = 200
)

// Quote sources. A message quote's ref is reply (the Bot's words) or own (the
// human's); a file's is its workspace path; an artifact's artifact_id@version.
const (
	quoteMessage  = "message"
	quoteFile     = "file"
	quoteArtifact = "artifact"
)

// userMeta is a message's extras as the log keeps them: masked, valid UTF-8.
func (a *App) userMeta(botID string, ex run.Extra) userMeta {
	var um userMeta
	for _, at := range ex.Files {
		um.Files = append(um.Files, eventAttachment{Name: at.GetName(), Path: at.GetPath(), Size: at.GetSize(), Mime: at.GetMime()})
	}
	for _, q := range ex.Quotes {
		um.Quotes = append(um.Quotes, eventQuote{
			Source: q.GetSource(), Ref: textx.ValidUTF8(q.GetRef()), Label: a.kept(botID, q.GetLabel()),
			Text: a.kept(botID, q.GetText()), Locator: q.GetLocator(), Image: q.GetImage(),
		})
	}
	if sp := ex.Spoken; sp != nil {
		um.Spoken = &spokenMeta{CallID: sp.CallID, Interrupted: a.kept(botID, textx.CapRunes(sp.Interrupted, maxInterrupted))}
	}
	return um
}

func (um userMeta) encode() string {
	if len(um.Files) == 0 && len(um.Quotes) == 0 && um.Spoken == nil {
		return ""
	}
	b, err := json.Marshal(um)
	if err != nil {
		return ""
	}
	return string(b)
}

// decodeUserMeta reads a user event's Meta: an object, or the bare list of
// attachments of a message from before quotes.
func decodeUserMeta(meta string) userMeta {
	var um userMeta
	meta = strings.TrimSpace(meta)
	switch {
	case meta == "":
	case strings.HasPrefix(meta, "["):
		_ = json.Unmarshal([]byte(meta), &um.Files)
	default:
		_ = json.Unmarshal([]byte(meta), &um)
	}
	return um
}

func v1Attachments(flat []eventAttachment) []*v1.Attachment {
	if len(flat) == 0 {
		return nil
	}
	out := make([]*v1.Attachment, 0, len(flat))
	for _, a := range flat {
		out = append(out, &v1.Attachment{Name: a.Name, Path: a.Path, Size: a.Size, Mime: a.Mime})
	}
	return out
}

// v1Spoken is a spoken turn's mark for the thread.
func v1Spoken(um userMeta) *v1.Spoken {
	if um.Spoken == nil {
		return nil
	}
	return &v1.Spoken{CallId: um.Spoken.CallID, Interrupted: um.Spoken.Interrupted}
}

func v1Quotes(qs []eventQuote) []*v1.Quote {
	if len(qs) == 0 {
		return nil
	}
	out := make([]*v1.Quote, 0, len(qs))
	for _, q := range qs {
		out = append(out, &v1.Quote{Source: q.Source, Ref: q.Ref, Label: q.Label, Text: q.Text, Locator: q.Locator, Image: q.Image})
	}
	return out
}

// cleanQuotes normalizes what a client says a message quotes. A quote is the
// human's own pointer, so a bad one is dropped rather than refused, and each
// is capped.
func cleanQuotes(in []*v1.Quote) []*v1.Quote {
	out := make([]*v1.Quote, 0, len(in))
	for _, q := range in {
		if q == nil {
			continue
		}
		src := strings.TrimSpace(q.GetSource())
		switch src {
		case quoteMessage, quoteFile, quoteArtifact:
		default:
			continue
		}
		text := strings.TrimSpace(textx.ValidUTF8(q.GetText()))
		img := workspace.Rel(q.GetImage())
		if img != "" && !strings.HasPrefix(img, "tmp/") {
			img = "" // a crop is an upload; nothing else is pointed at this way
		}
		if text == "" && img == "" {
			continue
		}
		out = append(out, &v1.Quote{
			Source: src, Ref: capRunes(strings.TrimSpace(q.GetRef()), 300), Label: oneLine(textx.ValidUTF8(q.GetLabel()), maxQuoteLabel),
			Text: capRunes(text, maxQuoteText), Locator: oneLine(textx.ValidUTF8(q.GetLocator()), 80), Image: img,
		})
		if len(out) == maxQuotes {
			break
		}
	}
	return out
}

func capRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// userTurnText is a user message as the model reads it: what the human
// quoted, then what they wrote, then the files they attached. Replay and a
// live run both build the turn here, so the bytes are the same.
func userTurnText(text string, um userMeta) string {
	var b strings.Builder
	// What was said aloud is a transcript, and the reply will be heard: the
	// marker says both (prompts/VOICE.md tells the model what it means).
	if sp := um.Spoken; sp != nil {
		if sp.Interrupted != "" {
			b.WriteString("[spoken; I cut you off after: \u201c" + sp.Interrupted + "\u201d]\n")
		} else {
			b.WriteString("[spoken]\n")
		}
	}
	for _, q := range um.Quotes {
		b.WriteString(quoteHead(q))
		b.WriteString("\n")
		if q.Text != "" {
			for _, line := range strings.Split(q.Text, "\n") {
				b.WriteString("> ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(text)
	if len(um.Files) > 0 {
		paths := make([]string, 0, len(um.Files))
		for _, at := range um.Files {
			paths = append(paths, at.Path)
		}
		b.WriteString("\n\n[Attached files in the workspace: " + strings.Join(paths, ", ") + "]")
	}
	return b.String()
}

// quoteHead is the fixed line that opens a quote: what it is from, where in it,
// and the crop when it is a region of an image.
func quoteHead(q eventQuote) string {
	// A message's words are named by whose they were, never by the client.
	from := q.Label
	switch {
	case q.Source == quoteMessage && q.Ref == "reply":
		from = "your earlier reply"
	case q.Source == quoteMessage && q.Ref == "own":
		from = "their own earlier message"
	case q.Source == quoteMessage:
		from = "an earlier message"
	case from == "":
		from = q.Ref
	}
	head := "[The human quotes " + from
	if q.Source == quoteFile && q.Ref != "" && !strings.Contains(from, q.Ref) {
		head += " (/workspace/" + q.Ref + ")"
	}
	if q.Locator != "" && q.Image == "" {
		head += ", " + q.Locator
	}
	if q.Image != "" {
		head += fmt.Sprintf(": a region of it, saved as %s — present it to see it", q.Image)
	}
	return head + "]"
}
