// Package quote is something the Bot made for the owner (a Feed post, a
// Dashboard widget or ask, a question asked from a background run) put into a
// chat for the owner to reply to. The chat's first event is
// the quoted thing; the model reads it as a user turn that says truthfully
// what was quoted, and the thread draws it as what it was (a widget as the
// widget, as it stood).
package quote

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/db"
	"silo.agent/internal/zone"
)

// Kind is the run event a quote is. The name is from when only Feed posts
// were quoted; events are stored under it, so it stays.
const Kind = "feed_quote"

// What was quoted. An event with no `from` is a Feed post (or, from before
// this package, a Dashboard quote that kept the Feed's wording).
const (
	FromFeed   = "feed"
	FromWidget = "widget"
	FromAsk    = "ask"
	// FromAsked is a question the Bot asked from a background run
	// (ask_owner in an automation), put into the main chat with its card.
	FromAsked = "asked"
)

// Widget is a widget as it stood when it was quoted: enough for the thread to
// draw it again, with no actions (a thread is a record, not the board).
type Widget struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Spec  string `json:"spec,omitempty"`
	Data  string `json:"data,omitempty"`
	State string `json:"state,omitempty"`
	// Source is where its data came from ("Calendar"), as the board shows it.
	Source string `json:"source,omitempty"`
}

// Quote is one quoted thing.
type Quote struct {
	From string
	// Title heads the card in the thread.
	Title string
	// Body is what the model reads as quoted, markdown.
	Body string
	// Source is where the Bot made it ("automation “Prices”"), may be empty.
	Source string
	// At is when it was made: posted, asked, or the widget's data time.
	At     time.Time
	Widget *Widget
	// Card is the ask a quote of one stands for, so the thread draws it live.
	Card string
}

type meta struct {
	From   string  `json:"from,omitempty"`
	Title  string  `json:"title,omitempty"`
	Widget *Widget `json:"widget,omitempty"`
	Card   string  `json:"card,omitempty"`
}

// CardOf is the card a quote event stands for, "" for none.
func CardOf(ev *db.RunEvent) string {
	if ev.Kind != Kind || ev.Meta == "" {
		return ""
	}
	return decode(ev.Meta).Card
}

// Origin is the run a quote is written in.
const Origin = "quote"

// Put writes the quote at the end of ch, in a run of its own; the owner's
// next message there is the reply to it. Where it goes (the main chat or a
// chat of its own) is run.Engine.Place's to decide.
func Put(gdb *gorm.DB, ch *db.Chat, q Quote) (*db.RunEvent, error) {
	m, _ := json.Marshal(meta{From: q.From, Title: q.Title, Widget: q.Widget, Card: q.Card})
	// CreatedAt is the quoted thing's time: history frames the quote with it.
	return chats.Put(gdb, ch, Origin, db.RunEvent{Kind: Kind, Body: q.Body, Tool: q.Source, Meta: string(m), CreatedAt: q.At})
}

func decode(raw string) meta {
	var m meta
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

// Proto is the quote as the thread draws it, nil for an event that is no
// quote (or one from before quotes carried what they were).
func Proto(ev *db.RunEvent) *v1.Quoted {
	if ev.Kind != Kind || ev.Meta == "" {
		return nil
	}
	m := decode(ev.Meta)
	out := &v1.Quoted{From: m.From, Title: m.Title}
	if w := m.Widget; w != nil {
		out.Widget = &v1.DashboardWidget{Key: w.Key, Kind: w.Kind, Title: m.Title, SpecJson: w.Spec, DataJson: w.Data, StateJson: w.State, Source: w.Source, DataAt: ev.CreatedAt.Format(time.RFC3339)}
	}
	return out
}

// Text is what the model reads for a quote event: a user turn that frames
// the quoted thing, so the owner's next message reads as a reply to it.
// The zone is the Bot's when the quote was made, kept on the event so the
// turn replays as the same text wherever the Bot is later.
func Text(ev *db.RunEvent) string {
	m := decode(ev.Meta)
	var b strings.Builder
	switch m.From {
	case FromWidget:
		fmt.Fprintf(&b, "I'm quoting the “%s” widget from my Dashboard", m.Title)
		if w := m.Widget; w != nil {
			fmt.Fprintf(&b, " (`%s`, a %s)", w.Key, w.Kind)
		}
		b.WriteString(", as it stood")
	case FromAsk:
		fmt.Fprintf(&b, "I'm quoting the card “%s” you put on my Dashboard", m.Title)
	case FromAsked:
		b.WriteString("You asked me this")
	default:
		b.WriteString("I'm quoting a post you made to my Feed")
	}
	switch {
	case ev.CreatedAt.IsZero():
	case ev.Zone == "":
		// A quote from before Bots had a zone: the bytes it always had.
		fmt.Fprintf(&b, " on %s", ev.CreatedAt.Local().Format("Mon 2006-01-02 15:04 MST"))
	default:
		fmt.Fprintf(&b, " on %s", zone.Stamp(ev.CreatedAt, zone.Resolve(ev.Zone)))
	}
	if ev.Tool != "" {
		b.WriteString(" from " + ev.Tool)
	}
	b.WriteString(":\n\n")
	for _, line := range strings.Split(ev.Body, "\n") {
		b.WriteString("> " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
