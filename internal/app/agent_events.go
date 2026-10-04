package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
)

func (a *App) emit(botID, chatID, runID, kind, body, tool string) {
	a.emitMeta(botID, chatID, runID, kind, body, tool, "")
}

// emitMeta is emit with server-only Meta persisted on the row (not sent to
// viewers). An event with no run is an orphan (see callRun): it is published
// to the Bot's bus without a chat, so no thread shows it, and never persisted,
// since no run would ever replay it.
func (a *App) emitMeta(botID, chatID, runID, kind, body, tool, meta string) {
	body = textx.ValidUTF8(a.Mask(botID).Apply(body))
	tool = textx.ValidUTF8(tool)
	id := ids.New()
	if runID == "" {
		a.Bus.Publish(botID, &v1.RunEvent{Id: id, Kind: kind, Body: body, Tool: tool})
		return
	}
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: kind, Body: body, Tool: tool, Meta: meta, CreatedAt: time.Now()})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: kind, Body: body, Tool: tool})
}

// presentBrowseLimit is the byte budget the CP asks the worker for when a
// tool presents a file. It is higher than the Files-tab preview budget so a
// photo or a rendered PDF page still reaches the multimodal model.
const presentBrowseLimit = 32 << 20

type eventAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
}

// emitUser persists the opening user message and carries any chat uploads so
// the thread can render chips on reload and the model can be told the paths.
// from names a non-human sender ("lead" for a subagent's brief and the lead's
// messages); it rides on Tool so the thread can label the bubble.
func (a *App) emitUser(botID, chatID, runID, body string, atts []*v1.Attachment, from string) {
	body = textx.ValidUTF8(a.Mask(botID).Apply(body))
	meta := ""
	if len(atts) > 0 {
		flat := make([]eventAttachment, 0, len(atts))
		for _, at := range atts {
			flat = append(flat, eventAttachment{Name: at.GetName(), Path: at.GetPath(), Size: at.GetSize(), Mime: at.GetMime()})
		}
		if b, err := json.Marshal(flat); err == nil {
			meta = string(b)
		}
	}
	id := ids.New()
	now := time.Now()
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: "user", Body: body, Tool: from, Meta: meta, CreatedAt: now})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: "user", Body: body, Tool: from, Attachments: atts, CreatedAt: now.Format(time.RFC3339)})
}

func attachmentsFromMeta(meta string) []eventAttachment {
	if meta == "" {
		return nil
	}
	var out []eventAttachment
	if json.Unmarshal([]byte(meta), &out) != nil {
		return nil
	}
	return out
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

// emitDelivered persists one user-visible block and, on a channel origin,
// delivers it. kind is "assistant" for a plain reply, "section" for a
// sentinel-bounded block on the final turn, or "section_live" for one emitted
// mid-turn (displayed and delivered but not replayed as model history).
func (a *App) emitDelivered(botID, chatID, runID, kind, body string, origin *runOrigin) {
	if strings.TrimSpace(body) == "" {
		return
	}
	a.emit(botID, chatID, runID, kind, body, "")
	if origin != nil && origin.deliver != nil {
		if err := origin.deliver(channels.Outbound{ExternalID: origin.external, Text: body}); err != nil {
			a.emit(botID, chatID, runID, "error", "channel send failed: "+err.Error(), "")
		}
	}
}

// deliverTool forwards a present/artifact side effect to a channel by reading
// the real bytes from the workspace and attaching them, so the human on the
// other side gets the file itself. A skill directory is zipped.
func (a *App) deliverTool(ctx context.Context, botID, chatID, runID string, origin *runOrigin, name, argsJSON, img string) {
	if origin == nil || origin.deliver == nil {
		return
	}
	send := func(text string, att *channels.Attachment) {
		msg := channels.Outbound{ExternalID: origin.external, Text: text}
		if att != nil {
			msg.Files = []channels.Attachment{*att}
		}
		if err := origin.deliver(msg); err != nil {
			a.emit(botID, chatID, runID, "error", "channel attach failed: "+err.Error(), "")
		}
	}
	var args struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		Kind  string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch name {
	case "present":
		if img != "" {
			if att, ok := dataURLAttachment(args.Path, img); ok {
				send("File: "+filepath.Base(args.Path), &att)
				return
			}
		}
		if att, err := a.workspaceAttachment(ctx, botID, args.Path); err == nil {
			send("File: "+att.Name, &att)
			return
		}
		if args.Path != "" {
			send("Presented in the workspace: "+args.Path, nil)
		}
	case "artifact":
		if info, err := a.describeArtifact(ctx, botID, args.Path, args.Kind, args.Title); err == nil {
			if info.Type == "skill" {
				if att, err := a.skillZipAttachment(ctx, botID, info.Path, info.Title); err == nil {
					send("Skill: "+info.Name, &att)
					return
				}
			} else if att, err := a.workspaceAttachment(ctx, botID, info.Path); err == nil {
				send("Artifact: "+info.Title, &att)
				return
			}
		}
		title := strings.TrimSpace(args.Title)
		if title == "" {
			title = filepath.Base(args.Path)
		}
		if title != "" {
			send("Artifact: "+title, nil)
		}
	}
}

func dataURLAttachment(path, dataURL string) (channels.Attachment, bool) {
	const marker = ";base64,"
	i := strings.Index(dataURL, marker)
	if !strings.HasPrefix(dataURL, "data:") || i < 0 {
		return channels.Attachment{}, false
	}
	mime := strings.TrimPrefix(dataURL[:i], "data:")
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(marker):])
	if err != nil || len(raw) == 0 {
		return channels.Attachment{}, false
	}
	name := filepath.Base(path)
	if name == "." || name == "/" || name == "" {
		name = "file"
	}
	return channels.Attachment{Name: name, Mime: mime, Data: raw}, true
}

// emitUsage reports token accounting for a turn, with the model's context
// window, so the composer can show the cache chip and the context meter. It
// persists like any other run event.
func (a *App) emitUsage(botID, chatID, runID string, u llm.Usage, window int) {
	body, _ := json.Marshal(map[string]int{
		"input":       u.InputTokens,
		"output":      u.OutputTokens,
		"cache_read":  u.CacheReadTokens,
		"cache_write": u.CacheWriteTokens,
		"window":      window,
	})
	a.emit(botID, chatID, runID, "usage", string(body), "")
}
