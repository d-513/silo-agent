// Package run names a unit of agent work and the narrow door into the engine
// that carries it out. The domains that start or steer runs (channels,
// automations, subagents, the chat itself) describe the work as a Request and
// reach the engine through Engine, so none of them imports package app.
package run

import (
	"context"
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
	// Working tells the other side the Bot is at it, for as long as the run
	// lasts: it is called when the run starts and what it returns when it ends.
	// nil where there is nobody to tell.
	Working func() (done func())
	// Automation is set when a scheduled automation started the run. Its runs
	// start from a fresh context and are not delivered anywhere.
	Automation *db.Automation
	// Profile is set when the automation is a domain's housekeeping job: the
	// run is narrowed to it.
	Profile *Profile
	// Subagent is set when the run is a subagent's work in its own log chat.
	// Its runs are not delivered anywhere and get the subagent tool set.
	Subagent *db.Subagent
	// Peer is set when the run answers another Bot's message in this Bot's
	// conversation for that Bot (message_bot). Its final reply goes back.
	Peer *db.BotMessage
	// Room is set when the run is a Bot's in its seat in a room: what it
	// writes is posted to the room (Deliver), and its prompt says who is
	// there.
	Room *db.RoomBot
	// Turned is called once, when the run's first model turn has ended (it
	// answered, stayed quiet, called tools or failed) or the run ended before
	// one: a room hands the floor to the next Bot then. nil when nobody waits.
	Turned func()
}

// Profile narrows a run to one background job: a prompt of its own in place
// of the Bot's, only the tools it names, a model of its own, and no machine.
// A pinned automation carries one (automation.Pinned.Profile), so a domain's
// housekeeping goes through the same loop, rules and log as any run without
// being handed the Bot's whole reach.
type Profile struct {
	// Name labels the job's model calls in the LLM debug log ("memory").
	Name string
	// Prompt is the base system prompt, in the Bot's own prompt's place. The
	// run is told who the Bot is and who uses it, and nothing else of it.
	Prompt func() string
	// Tools are the built-in tools the run is offered, by name, wherever each
	// is otherwise exposed. None of them may need the Bot's machine: the run
	// does not start it.
	Tools []string
	// Model is the model the run uses; "" means the Bot's own.
	Model func() string
}

// Report is what a lead's wake run opens with: its subagents' results.
type Report struct {
	Body  string
	Label string
}

// Extra is what a message carries besides its text: files the human uploaded
// and what they quoted, or that they said it on a voice call.
type Extra struct {
	Files  []*v1.Attachment
	Quotes []*v1.Quote
	Spoken *Spoken
	// Bot marks a message another Bot sent (message_bot): who sent it and for
	// whom.
	Bot *BotFrom
	// Room marks what a room said, handed to a Bot in its seat.
	Room *RoomFrom
}

// RoomFrom is the room a Bot is handed messages from: its title as it was,
// and whether the messages name the Bot (it must answer then).
type RoomFrom struct {
	Title     string
	Mentioned bool
}

// BotFrom is the sender of a message from another Bot: its name, and the
// person (email) it asks for, "" for the sending Bot's own work.
type BotFrom struct {
	Name string
	For  string
}

// Spoken marks a message the human said on a voice call: the call, and the
// words of the Bot's reply they cut off by speaking, if they did.
type Spoken struct {
	CallID      string
	Interrupted string
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
	// Resume opens with nothing: the run answers the history as it stands
	// (Retry after a reply failed).
	Resume bool
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
	// FromBot is a message from another Bot of the Workspace (message_bot).
	FromBot = "bot"
	// FromRoom is what a room said, handed to a Bot in its seat: people's
	// and other Bots' words, never one person's request on its own.
	FromRoom = "room"
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
	// UserID is who pressed: the placement goes to their main chat, and a
	// chat of its own is theirs. "" is the Bot's own doing.
	UserID string
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
	// StartOrSteer is StartOrInject for a whole request (its sender
	// included): started says a new run began, rather than the message
	// going into the live one.
	StartOrSteer(req Request) (runID string, started bool, err error)
	// StartIfIdle starts a run only when the conversation has no live one,
	// otherwise it returns ErrBusy: a scheduled firing is skipped, never queued.
	StartIfIdle(Request) (string, error)
	// Place puts something into the Bot's main chat, or a chat of its own,
	// holding the conversation lock across the choice, the write and the
	// start, so no run starts in the main chat in between.
	Place(Placement) (Placed, error)
	// LiveRunID is the active run of a conversation, or "".
	LiveRunID(botID, chatID string) string
	// Wait blocks until the run has exited (or ctx ends) and answers the
	// status it ended with; "" when ctx ended first.
	Wait(ctx context.Context, runID string) string
	// StopChatLive stops any live run of the conversation and waits for it.
	StopChatLive(botID, chatID string)
	// Command runs text typed in a channel conversation as a slash command;
	// handled is false when it is an ordinary message. reply is what to send
	// back.
	Command(botID, chatID, text string) (reply string, handled bool)
}
