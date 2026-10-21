// Package run names a unit of agent work and the narrow door into the engine
// that carries it out. The domains that start or steer runs (channels,
// automations, subagents, the chat itself) describe the work as a Request and
// reach the engine through Engine, so none of them imports package app.
package run

import (
	"errors"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// Origin identifies where a run came from and how to deliver user-visible
// sections. A nil origin or channel is a Web UI chat.
type Origin struct {
	Channel  *db.Channel
	External string
	Deliver  func(channels.Outbound) error
	// Automation is set when a scheduled automation started the run. Its runs
	// start from a fresh context and are not delivered anywhere.
	Automation *db.Automation
	// Subagent is set when the run is a subagent's work in its own log chat.
	// Its runs are not delivered anywhere and get the subagent tool set.
	Subagent *db.Subagent
}

// Report is what a lead's wake run opens with: its subagents' results.
type Report struct {
	Body  string
	Label string
}

// Extra is what a message carries besides its text: files the human uploaded
// and what they quoted.
type Extra struct {
	Files  []*v1.Attachment
	Quotes []*v1.Quote
}

// Request is one call into the shared execution engine. Chats, channels,
// automations and subagents all go through here.
type Request struct {
	BotID  string
	ChatID string
	Text   string
	Extra  Extra
	Origin *Origin
	// Compact starts a manual compaction run: it summarizes the chat and only
	// goes on to a model turn if a message is injected meanwhile.
	Compact bool
	// From names a non-human sender of Text.
	From string
	// Report opens a lead's wake run with its subagents' results instead of a
	// user message.
	Report *Report
}

// The bare Request.From tags: who other than a human wrote a run's opening
// text. History and the thread branch on them.
const (
	// FromOnboarding is the interview's opening turn, which Silo wrote.
	FromOnboarding = "onboarding"
	// FromMail is a mail that woke the Bot.
	FromMail = "mail"
	// FromLead is a subagent's brief or a message from its lead.
	FromLead = "lead"
)

// ErrBusy is what StartIfIdle returns when the conversation already has a live
// run.
var ErrBusy = errors.New("a run is already live for this conversation")

// ErrNotMain is what Place answers when the main chat is not open to the
// placement (a reply runs there, or the Bot may not write there unasked) and
// it has no chat of its own to fall back to.
var ErrNotMain = errors.New("the main chat is not open to this now")

// Placement is something put into a Bot's main chat from outside it: a quote
// of a Feed post or a Dashboard thing, a question or an offer from a
// background run, a mail that wakes the Bot. It goes to the main chat unless
// a reply is running there; then to a chat of its own (Title), so a live
// run's history and the thread never disagree about order.
type Placement struct {
	BotID string
	// Write puts the thing into ch (the main chat or the new one) and answers
	// the event it wrote, which the chat's viewers are sent; nil writes
	// nothing but the message.
	Write func(ch *db.Chat) (*db.RunEvent, error)
	// Title is the chat of its own's. "" means there is none: with the main
	// chat not open, Place answers ErrNotMain.
	Title string
	// Own opens the chat of its own even when the main chat is idle (the
	// owner asked for a new chat).
	Own bool
	// Proactive takes the main chat only while the Bot's proactivity lets it
	// write there unasked.
	Proactive bool
	// Message, when set, starts a run in the chat with it as the turn that
	// answers what was written; From names a non-human sender.
	Message string
	From    string
}

// Placed is where a Placement went.
type Placed struct {
	Chat  *db.Chat
	Main  bool
	RunID string
}

// Engine is the part of the agent runtime other domains drive.
type Engine interface {
	// StartRun records a run and launches the agent loop. A caller with a live
	// run for the conversation should StartOrInject instead.
	StartRun(Request) (string, error)
	// StartOrInject steers a live run for the conversation with the message,
	// or starts a new one: the single entry point for chats and channels.
	StartOrInject(botID, chatID, text string, ex Extra, origin *Origin) (string, error)
	// StartIfIdle starts a run only when the conversation has no live one,
	// otherwise it returns ErrBusy: a scheduled firing is skipped, never queued.
	StartIfIdle(Request) (string, error)
	// Place puts something into the Bot's main chat, or a chat of its own,
	// holding the conversation lock across the choice, the write and the
	// start, so no run starts in the main chat in between.
	Place(Placement) (Placed, error)
	// LiveRunID is the active run of a conversation, or "".
	LiveRunID(botID, chatID string) string
	// StopChatLive stops any live run of the conversation and waits for it.
	StopChatLive(botID, chatID string)
	// Command runs text typed in a channel conversation as a slash command;
	// handled is false when it is an ordinary message. reply is what to send
	// back.
	Command(botID, chatID, text string) (reply string, handled bool)
}
