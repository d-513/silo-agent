package voice

// The talker is a call's fast front: a small text model that hears every
// turn first, answers what it can in a moment (small talk, a question back),
// and hands everything else to the Bot with its delegate tool. It cannot act:
// its three tools only steer the call. Its own words are written into the
// chat as an exchange of their own, cut to what the human heard, so the Bot
// reads them in order next time.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"silo.agent/internal/app/run"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/lang"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/speech"
	"silo.agent/internal/textx"
)

// Talker limits.
const (
	// talkTimeout bounds one talker turn; past it the Bot takes the turn.
	talkTimeout = 20 * time.Second
	// talkHistory is how many recent messages of the chat the talker reads.
	talkHistory = 16
	// talkMessageMax caps one of them: the Bot's long answers are for the eye.
	talkMessageMax = 600
	// soulMax caps the SOUL the talker is given.
	soulMax = 1500
)

var talkerTools = []llm.Tool{
	{Name: "delegate", Description: "Hand this turn to the Bot: it does the work and speaks its own answer. Say a few words first.", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
	{Name: "stop_task", Description: "Stop what the Bot is working on, when the person asks you to.", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
	{Name: "end_call", Description: "Hang up, after saying goodbye.", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
}

// talkTurn is a turn the talker is answering: what was said, its reply, and
// what of the reply has been heard.
type talkTurn struct {
	id     string
	said   string
	cut    string
	reply  string
	played []string // pieces that started playing, in order
	done   bool     // the reply is written in full (the stream ended)
	// interrupted: the human cut in; heard is what of the reply they got.
	interrupted bool
	heard       string
}

// exchange is a talker turn waiting to be written, while a reply runs.
type exchange struct{ said, cut, reply string }

// talk answers one turn with the talker; on any failure the Bot takes it.
func (c *call) talk(said, cut string) {
	cfg := c.s.cfg()
	client, provider, model, err := c.s.models.Client(cfg.ModelVoice)
	if err != nil {
		log.Printf("voice: call %s talker: %v", c.id, err)
		c.hand(said, cut)
		return
	}
	var b db.Bot
	c.s.db.Select("name", "soul", "language").Where("id = ?", c.botID).Limit(1).Find(&b)
	req := llm.Request{
		Model:     model,
		System:    []llm.SystemBlock{{Text: talkerSystem(&b), CacheAfter: true}},
		Messages:  c.talkerHistory(said, cut, b.Name),
		Tools:     talkerTools,
		MaxTokens: 400,
		Latency:   true,
	}
	ctx, cancel := context.WithTimeout(c.ctx, talkTimeout)
	defer cancel()
	c.mu.Lock()
	c.nTalk++
	turn := &talkTurn{id: fmt.Sprintf("talk-%d", c.nTalk), said: said, cut: cut}
	c.talking = turn
	c.mu.Unlock()

	started := time.Now()
	stream, err := client.Stream(ctx, req)
	if err != nil {
		c.talkFailed(turn, provider, model, req, err, started)
		return
	}
	defer stream.Close()
	reader := speech.NewStream(speech.PointersFor(c.snd.Language))
	var text strings.Builder
	calls := map[int]*llm.ToolCall{}
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case llm.EventText:
			text.WriteString(ev.Text)
			for _, chunk := range reader.Push(ev.Text) {
				c.say(chunk, turn.id)
			}
		case llm.EventToolCallStart:
			calls[ev.Index] = &llm.ToolCall{ID: ev.ToolCallID, Name: ev.ToolName}
		case llm.EventToolCallDelta:
			if tc := calls[ev.Index]; tc != nil {
				tc.Arguments += ev.Text
			}
		}
	}
	for _, chunk := range reader.Flush() {
		c.say(chunk, turn.id)
	}
	reply := strings.TrimSpace(text.String())
	var names []string
	var tcs []llm.ToolCall
	for _, tc := range calls {
		names = append(names, tc.Name)
		tcs = append(tcs, *tc)
	}
	c.s.models.Record(c.botID, "voice-talk", llm.Record{Provider: provider, Model: model, System: req.System, Messages: req.Messages, Tools: req.Tools, Text: reply, ToolCalls: tcs, Error: stream.Err(), Duration: time.Since(started)})
	if err := stream.Err(); err != nil && reply == "" && len(calls) == 0 {
		c.talkFailed(turn, provider, model, req, err, started)
		return
	}
	delegated := false
	for _, n := range names {
		switch n {
		case "delegate":
			delegated = true
		case "stop_task":
			c.s.host.StopChat(c.botID, c.chatID)
		case "end_call":
			c.mu.Lock()
			c.ending = true
			c.mu.Unlock()
		}
	}
	// A turn the talker neither answered nor handed on is the Bot's.
	if reply == "" && len(names) == 0 {
		delegated = true
	}
	c.mu.Lock()
	turn.reply, turn.done = reply, true
	if delegated && c.talking == turn {
		// The few words before a hand-off are filler, not part of the record.
		c.talking = nil
	}
	c.mu.Unlock()
	if delegated {
		c.hand(said, cut)
		return
	}
	// Written once it has been heard, or cut short.
	if !c.out.busy() || turn.interrupted {
		c.finishTalk()
	}
}

// talkFailed gives the turn to the Bot: a talker that cannot answer must not
// lose what was said.
func (c *call) talkFailed(turn *talkTurn, provider, model string, req llm.Request, err error, started time.Time) {
	if c.ctx.Err() != nil {
		return
	}
	log.Printf("voice: call %s talker %s/%s: %v", c.id, provider, model, err)
	c.mu.Lock()
	if c.talking == turn {
		c.talking = nil
	}
	c.mu.Unlock()
	c.hand(turn.said, turn.cut)
}

// finishTalk writes the talker's turn once its reply is over: in full when
// the voice ran out, up to what was heard when the human cut in.
func (c *call) finishTalk() {
	c.mu.Lock()
	turn := c.talking
	if turn == nil || !turn.done {
		c.mu.Unlock()
		return
	}
	c.talking = nil
	reply := turn.reply
	if turn.interrupted {
		reply = turn.heard
	}
	ending := c.ending
	c.mu.Unlock()
	c.record(exchange{said: turn.said, cut: turn.cut, reply: reply})
	if ending {
		c.cancel(errHangup)
	}
}

// cutTalk is the human speaking over the talker: the rest of its reply is
// not said, and what was heard of it is what is written.
func (c *call) cutTalk(heard string) {
	c.mu.Lock()
	turn := c.talking
	if turn == nil {
		c.mu.Unlock()
		return
	}
	played := turn.played
	if len(played) > 0 {
		played = played[:len(played)-1]
	}
	turn.interrupted = true
	turn.heard = strings.TrimSpace(strings.Join(append(append([]string(nil), played...), heard), " "))
	c.muted[turn.id] = true
	done := turn.done
	c.mu.Unlock()
	if done {
		c.finishTalk()
	}
}

// record writes an exchange into the chat, or keeps it until the reply that
// runs there now is over.
func (c *call) record(ex exchange) {
	err := c.s.host.RecordSpoken(c.botID, c.chatID, ex.said, ex.reply, run.Spoken{CallID: c.id, Interrupted: ex.cut})
	if errors.Is(err, run.ErrBusy) {
		c.mu.Lock()
		c.waiting = append(c.waiting, ex)
		c.mu.Unlock()
		return
	}
	if err != nil {
		log.Printf("voice: call %s record: %v", c.id, err)
	}
}

// flushWaiting writes the exchanges that waited for a reply to end.
func (c *call) flushWaiting() {
	c.mu.Lock()
	waiting := c.waiting
	c.waiting = nil
	c.mu.Unlock()
	for _, ex := range waiting {
		c.record(ex)
	}
}

// hand gives a turn to the Bot: the human's words, marked spoken, as a
// message in the chat.
func (c *call) hand(said, cut string) {
	runID, err := c.s.host.StartOrInject(c.botID, c.chatID, said, run.Extra{Spoken: &run.Spoken{CallID: c.id, Interrupted: cut}}, nil)
	if err != nil {
		log.Printf("voice: call %s: %v", c.id, err)
		if errors.Is(err, run.ErrBusy) {
			c.say(sayBusy.in(c.snd.Language), "")
		} else {
			c.say(sayFailed.in(c.snd.Language), "")
		}
		return
	}
	c.mu.Lock()
	// What the Bot says next answers this turn: speak it even if an earlier
	// sentence of the same run was cut off.
	delete(c.muted, runID)
	if c.pending != nil {
		c.pending.Delegated = c.mode == config.VoiceModeTalker
	}
	c.mu.Unlock()
}

// talkerSystem is the talker's system prompt for a Bot.
func talkerSystem(b *db.Bot) string {
	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = "the Bot"
	}
	language := lang.Name(b.Language)
	if language == "" {
		language = "the language the person speaks"
	}
	out := strings.NewReplacer("{name}", name, "{language}", language).Replace(strings.TrimSpace(prompts.VoiceTalker))
	if soul := strings.TrimSpace(b.Soul); soul != "" {
		out += "\n\n## Who " + name + " is\n" + textx.CapRunes(soul, soulMax)
	}
	return out
}

// talkerHistory is what the talker reads: the chat's last messages as plain
// text, then this turn (with where it cut the voice off, and whether the Bot
// is still at work).
func (c *call) talkerHistory(said, cut, name string) []llm.Message {
	var evs []db.RunEvent
	c.s.db.Model(&db.RunEvent{}).
		Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ? AND run_events.kind IN ?", c.chatID, []string{"user", "assistant", "section"}).
		Order("run_events.seq DESC").Limit(talkHistory).
		Select("run_events.kind", "run_events.body").Find(&evs)
	var msgs []llm.Message
	for i := len(evs) - 1; i >= 0; i-- {
		role := llm.RoleAssistant
		if evs[i].Kind == "user" {
			role = llm.RoleUser
		}
		text := textx.CapRunes(strings.TrimSpace(evs[i].Body), talkMessageMax)
		if text == "" {
			continue
		}
		// Roles alternate: a run of one side reads as one message.
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Text += "\n\n" + text
			continue
		}
		msgs = append(msgs, llm.Message{Role: role, Text: text})
	}
	// The reply the human cut into may not be written yet: it is what they
	// last heard of the Bot.
	var head []string
	if cut != "" {
		head = append(head, "[I cut you off after: “"+cut+"”]")
	}
	if c.s.host.LiveRunID(c.botID, c.chatID) != "" {
		head = append(head, "["+name+" is still working on an earlier request.]")
	}
	turn := said
	if len(head) > 0 {
		turn = strings.Join(head, "\n") + "\n" + said
	}
	if n := len(msgs); n > 0 && msgs[n-1].Role == llm.RoleUser {
		msgs[n-1].Text += "\n\n" + turn
	} else {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: turn})
	}
	// A conversation opens with the human.
	for len(msgs) > 0 && msgs[0].Role != llm.RoleUser {
		msgs = msgs[1:]
	}
	return msgs
}
