package channel

import (
	"context"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/channels"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
)

const (
	// mediaTimeout bounds downloading one file and saving or hearing it.
	mediaTimeout = 3 * time.Minute
	// maxInboundFiles is how many files of one message are read.
	maxInboundFiles = 20
	maxNameRunes    = 80
)

// readFiles turns what came with a message into what the Bot is told: the
// caption, a voice memo's words, a line for anything it cannot have, and the
// rest saved as uploads, the same as a file attached in the web composer. The
// `[voice message]` marker is also why a spoken "/clear" is never a command.
func (s *Service) readFiles(botID string, in channels.Inbound) (string, []*v1.Attachment) {
	var lines []string
	if t := strings.TrimSpace(in.Text); t != "" {
		lines = append(lines, t)
	}
	var kept []*v1.Attachment
	files := in.Files
	if len(files) > maxInboundFiles {
		lines = append(lines, fmt.Sprintf("[%d more attachments were left out]", len(files)-maxInboundFiles))
		files = files[:maxInboundFiles]
	}
	for _, m := range files {
		ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
		line, att := s.readFile(ctx, botID, m)
		cancel()
		if line != "" {
			lines = append(lines, line)
		}
		if att != nil {
			kept = append(kept, att)
		}
	}
	return strings.Join(lines, "\n"), kept
}

// readFile takes one file in. Either result may be empty; a memo that could
// not be heard has both, so the Bot can still try it with its transcribe tool.
func (s *Service) readFile(ctx context.Context, botID string, m channels.Media) (string, *v1.Attachment) {
	name := cleanName(m.Name)
	if m.Size > workspace.PutMax {
		return tooLarge(name), nil
	}
	data, err := m.Fetch(ctx)
	if err != nil {
		log.Printf("channel: download %s bot=%s: %v", name, botID, err)
		return "[attachment: " + name + " could not be downloaded]", nil
	}
	if len(data) > workspace.PutMax {
		return tooLarge(name), nil
	}
	line := ""
	if m.Voice {
		said, err := s.voice.Hear(ctx, botID, llm.Audio{Data: data, MIME: m.Mime, Filename: name})
		switch {
		case err == nil && said == "":
			return "[voice message: no speech found]", nil
		case err == nil:
			return "[voice message]\n" + said, nil
		}
		log.Printf("channel: transcribe bot=%s: %v", botID, err)
		line = "[voice message: could not be transcribed]"
	}
	att, err := s.save(ctx, botID, name, m.Mime, data)
	if err != nil {
		log.Printf("channel: save %s bot=%s: %v", name, botID, err)
		return strings.TrimSpace(line + "\n[attachment: " + name + " could not be saved]"), nil
	}
	return line, att
}

// save puts a file where the web composer's uploads go. Uploads are wiped when
// the box starts, so the box is brought up first.
func (s *Service) save(ctx context.Context, botID, name, mime string, data []byte) (*v1.Attachment, error) {
	if !s.host.Wake(ctx, botID) {
		return nil, fmt.Errorf("the Bot machine is not running yet")
	}
	rel := "tmp/" + ids.New()[:8] + "-" + name
	if err := s.ws.Put(ctx, botID, rel, data); err != nil {
		return nil, err
	}
	return &v1.Attachment{Name: name, Path: rel, Size: int64(len(data)), Mime: mime}, nil
}

func tooLarge(name string) string {
	return fmt.Sprintf("[attachment: %s is too large, over %d MB]", name, workspace.PutMax>>20)
}

// cleanName is a sender's file name made safe to be a path and to be read by a
// model: letters, digits and a little punctuation, no way out of the folder.
func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.TrimLeft(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name), ".")
	if runes := []rune(name); len(runes) > maxNameRunes {
		name = string(runes[len(runes)-maxNameRunes:]) // the end holds the extension
	}
	if strings.Trim(name, "_") == "" {
		return "file"
	}
	return name
}

// lanes runs a conversation's slow messages one at a time, in arrival order.
type lanes struct {
	mu   sync.Mutex
	busy map[string][]func()
}

// occupied says a message is still being worked on in that lane.
func (l *lanes) occupied(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.busy[key]
	return ok
}

// run queues job behind what is in the lane, off the caller's goroutine.
func (l *lanes) run(key string, job func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if q, ok := l.busy[key]; ok {
		l.busy[key] = append(q, job)
		return
	}
	l.busy[key] = nil
	go func() {
		for next := job; next != nil; next = l.next(key) {
			next()
		}
	}()
}

func (l *lanes) next(key string) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	q := l.busy[key]
	if len(q) == 0 {
		delete(l.busy, key)
		return nil
	}
	l.busy[key] = q[1:]
	return q[0]
}
