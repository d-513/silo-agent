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
	// Recall is the auto-recalled memory note, computed once per run.
	Recall string
	// Subagent is set when the run is a subagent's work in its own log chat.
	// Its runs are not delivered anywhere and get the subagent tool set.
	Subagent *db.Subagent
}

// Report is what a lead's wake run opens with: its subagents' results.
type Report struct {
	Body  string
	Label string
}

// Request is one call into the shared execution engine. Chats, channels,
// automations and subagents all go through here.
type Request struct {
	BotID  string
	ChatID string
	Text   string
	Atts   []*v1.Attachment
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

// ErrBusy is what StartIfIdle returns when the conversation already has a live
// run.
var ErrBusy = errors.New("a run is already live for this conversation")

// Engine is the part of the agent runtime other domains drive.
type Engine interface {
	// StartRun records a run and launches the agent loop. A caller with a live
	// run for the conversation should StartOrInject instead.
	StartRun(Request) (string, error)
	// StartOrInject steers a live run for the conversation with the message,
	// or starts a new one: the single entry point for chats and channels.
	StartOrInject(botID, chatID, text string, atts []*v1.Attachment, origin *Origin) (string, error)
	// StartIfIdle starts a run only when the conversation has no live one,
	// otherwise it returns ErrBusy: a scheduled firing is skipped, never queued.
	StartIfIdle(Request) (string, error)
	// LiveRunID is the active run of a conversation, or "".
	LiveRunID(botID, chatID string) string
	// StopChatLive stops any live run of the conversation and waits for it.
	StopChatLive(botID, chatID string)
}
