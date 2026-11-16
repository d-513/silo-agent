package voice

// A lap is one turn's latency, stage by stage, sent to the browser as it
// fills in so the call screen's debug readout can show where the time goes.
// It is for looking at, not a record: voice_turns keeps what is kept.

import "time"

type lap struct {
	at       time.Time // the turn's end, as the control plane saw it
	EouMs    int       `json:"eou_ms"`    // the browser deciding the turn was over
	SttMs    int       `json:"stt_ms"`    // speech-to-text
	TalkerMs int       `json:"talker_ms"` // the talker's first token, from its request
	Talker   string    `json:"talker"`    // answered | delegated | failed
	handAt   time.Time
	BotMs    int `json:"bot_ms"`   // the Bot's first words, from the hand-off
	TtsMs    int `json:"tts_ms"`   // the first piece, from queued to its first frame
	FirstMs  int `json:"first_ms"` // the first sound, from the turn's end
}

func since(t time.Time) int { return int(time.Since(t) / time.Millisecond) }

// lapDo changes the current lap under the call's lock and sends it.
func (c *call) lapDo(f func(l *lap)) {
	c.mu.Lock()
	l := c.lap
	if l == nil {
		c.mu.Unlock()
		return
	}
	f(l)
	cp := *l
	c.mu.Unlock()
	_ = c.send(map[string]any{"t": "lap", "lap": cp})
}
