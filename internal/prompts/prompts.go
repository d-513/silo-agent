package prompts

import _ "embed"

//go:embed SYSTEM.md
var System string

// Channel is appended to the system prompt when a run replies over a channel.
//
//go:embed CHANNEL.md
var Channel string

// Approval is the base system prompt for the auto-approval model. The Bot's
// own policy is appended to it.
//
//go:embed APPROVAL.md
var Approval string

// Automation is the per-run note for a run an automation started.
//
//go:embed AUTOMATION.md
var Automation string

// Heartbeat is the default prompt of the pinned Heartbeat automation.
//
//go:embed HEARTBEAT.md
var Heartbeat string

// Compact is the system prompt for summarizing a conversation that no longer
// fits the model's context window.
//
//go:embed COMPACT.md
var Compact string

// Memory is the system prompt of the memory collector, which reads a chat's
// new messages and answers with the facts and lessons to save as JSON.
//
//go:embed MEMORY.md
var Memory string

// Subagent is the per-run note for a subagent's run. {{NAME}} is replaced with
// its name.
//
//go:embed SUBAGENT.md
var Subagent string

// Tunnels is the system-prompt section for a Bot that has the tunnel tools.
//
//go:embed TUNNELS.md
var Tunnels string
