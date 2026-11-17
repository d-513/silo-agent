package runlog

import (
	"sync"
	"time"

	"silo.agent/internal/app/models"
)

const (
	// backgroundMax is how many lines a Bot keeps, backgroundAge how long.
	backgroundMax = 500
	backgroundAge = 24 * time.Hour
	// burst is how close two like calls must be to be one line: a Knowledge
	// sync embeds in hundreds of requests, a call speaks in dozens of pieces.
	burst = time.Minute
)

// Line is one background model call, or a burst of like ones.
type Line struct {
	At time.Time
	models.Call
	// Count is how many calls the line stands for; its tokens and duration
	// are theirs added up.
	Count int
}

// Background is the model calls made for no run, per Bot, in memory: a chat's
// title, a memory digest, the talker on a call, embeddings, speech. They
// change nothing a run's history would have to explain, so nothing of them
// is stored and the list starts empty with the server.
type Background struct {
	mu    sync.Mutex
	since time.Time
	bots  map[string][]Line
	now   func() time.Time
}

func newBackground() *Background {
	return &Background{since: time.Now(), bots: map[string][]Line{}, now: time.Now}
}

// Note keeps a call. One like the Bot's newest line (the same label, model
// and outcome, within burst) joins that line.
func (b *Background) Note(c models.Call) {
	if c.BotID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	lines := b.bots[c.BotID]
	if n := len(lines); n > 0 {
		last := &lines[n-1]
		if last.Label == c.Label && last.Provider == c.Provider && last.Model == c.Model &&
			last.Err == c.Err && last.Verdict == c.Verdict && now.Sub(last.At) < burst {
			last.At = now
			last.Count++
			last.In += c.In
			last.Out += c.Out
			last.Duration += c.Duration
			return
		}
	}
	lines = append(lines, Line{At: now, Call: c, Count: 1})
	if len(lines) > backgroundMax {
		lines = append(lines[:0:0], lines[len(lines)-backgroundMax:]...)
	}
	b.bots[c.BotID] = lines
}

// List is a Bot's lines, newest first, without the ones past backgroundAge.
func (b *Background) List(botID string) []Line {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := b.bots[botID]
	cut := b.now().Add(-backgroundAge)
	first := 0
	for first < len(lines) && lines[first].At.Before(cut) {
		first++
	}
	if first > 0 {
		lines = append(lines[:0:0], lines[first:]...)
		b.bots[botID] = lines
	}
	out := make([]Line, len(lines))
	for i, l := range lines {
		out[len(lines)-1-i] = l
	}
	return out
}

// Drop forgets a Bot's lines.
func (b *Background) Drop(botID string) {
	b.mu.Lock()
	delete(b.bots, botID)
	b.mu.Unlock()
}
