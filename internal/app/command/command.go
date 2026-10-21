// Package command is the slash commands a human types in a conversation:
// "/clear", "/model gpt", … One registry serves every surface: the web and
// iOS composers send the text through Send like any message, and a channel
// adapter's inbound text goes through the same parse, so a command means the
// same thing wherever it is typed. A text that is not a known command is an
// ordinary message.
package command

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Surface is where a command was typed.
type Surface int

const (
	// Web is the owner's console or app: every command.
	Web Surface = iota
	// Channel is a channel conversation (Telegram, Discord, WhatsApp), where
	// the sender may be anyone the conversation lets in: only commands that
	// destroy nothing and change no setting of the owner's.
	Channel
)

// Def is one command.
type Def struct {
	Name string
	// Args is how the argument reads in the menu ("[model]"), empty for none.
	Args    string
	Summary string
	// Channel: it also works from a channel conversation.
	Channel bool
	// Confirm: it destroys something, so it answers with a question first
	// and acts only when sent again confirmed.
	Confirm bool
	Run     func(ctx context.Context, c Call) (Result, error)
}

// Call is one use of a command.
type Call struct {
	BotID  string
	ChatID string
	// Args is the text after the command's name, trimmed.
	Args      string
	Surface   Surface
	Confirmed bool
}

// Result is what a command did.
type Result struct {
	// Notice is what it says back: shown by the composer, sent back over a
	// channel.
	Notice string
	// Confirm is the question a Confirm command asks before it acts; the
	// command did nothing yet.
	Confirm string
	// ChatID is a chat the command made or acted on, RunID a run it started.
	ChatID string
	RunID  string
	// NewChat asks the client to open an empty new chat.
	NewChat bool
	// Value is the result for a client that words it itself (SendResponse).
	Value string
}

// ErrRefused is a command that may not run where it was typed. Its text is
// for the human.
var ErrRefused = errors.New("refused")

// Registry is the set of commands.
type Registry struct {
	defs   []Def
	byName map[string]*Def
}

// New makes a registry of defs, listed by name.
func New(defs []Def) *Registry {
	r := &Registry{defs: append([]Def(nil), defs...), byName: map[string]*Def{}}
	sort.Slice(r.defs, func(i, j int) bool { return r.defs[i].Name < r.defs[j].Name })
	for i := range r.defs {
		r.byName[r.defs[i].Name] = &r.defs[i]
	}
	return r
}

// List is the commands offered on a surface.
func (r *Registry) List(s Surface) []Def {
	out := make([]Def, 0, len(r.defs))
	for _, d := range r.defs {
		if s == Channel && !d.Channel {
			continue
		}
		out = append(out, d)
	}
	return out
}

// Find is the command of that name, or nil.
func (r *Registry) Find(name string) *Def {
	return r.byName[strings.ToLower(name)]
}

// Parsed is what a typed text is.
type Parsed struct {
	// Def is the command, nil for a message.
	Def  *Def
	Args string
	// Text is the message to send when Def is nil: the text itself, or with
	// one slash taken off when it was escaped ("//clear" sends "/clear").
	Text string
}

// Parse reads text as a command or a message. A command is "/" and a known
// name at the very start, then whitespace or the end; Telegram's
// "/clear@SomeBot" form is a command too. Anything else, including an
// unknown "/name" (a pasted path), is a message.
func (r *Registry) Parse(text string) Parsed {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "//") {
		return Parsed{Text: strings.TrimPrefix(strings.TrimLeft(text, " \t\r\n"), "/")}
	}
	if !strings.HasPrefix(t, "/") {
		return Parsed{Text: text}
	}
	head, args, _ := strings.Cut(t[1:], " ")
	if i := strings.IndexAny(head, "\n\t"); i >= 0 {
		args = head[i:] + " " + args
		head = head[:i]
	}
	if at := strings.IndexByte(head, '@'); at > 0 {
		head = head[:at]
	}
	d := r.Find(head)
	if d == nil {
		return Parsed{Text: text}
	}
	return Parsed{Def: d, Args: strings.TrimSpace(args)}
}

// Run runs a parsed command on a surface.
func (r *Registry) Run(ctx context.Context, d *Def, c Call) (Result, error) {
	if c.Surface == Channel && !d.Channel {
		return Result{}, refused("/" + d.Name + " works only in Silo itself, not from a channel.")
	}
	return d.Run(ctx, c)
}

// Refused is an error a command answers when it may not run as asked.
func Refused(msg string) error { return refused(msg) }

type refusedErr struct{ msg string }

func (e refusedErr) Error() string { return e.msg }
func (e refusedErr) Unwrap() error { return ErrRefused }

func refused(msg string) error { return refusedErr{msg} }
