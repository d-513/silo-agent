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

// Filter is the base system prompt for the output filter when its model is a
// text model. The output policy is appended to it.
//
//go:embed FILTER.md
var Filter string

// FilterPolicy is the output policy of a Bot whose owner wrote none: what in
// a tool result counts as an attempt to steer the Bot.
//
//go:embed FILTER_POLICY.md
var FilterPolicy string

// Automation is the per-run note for a run an automation started.
//
//go:embed AUTOMATION.md
var Automation string

// Heartbeat is the default prompt of the pinned Heartbeat automation.
//
//go:embed HEARTBEAT.md
var Heartbeat string

// Dashboard is the default prompt of the pinned Dashboard automation.
//
//go:embed DASHBOARD.md
var Dashboard string

// Compact is the system prompt for summarizing a conversation that no longer
// fits the model's context window.
//
//go:embed COMPACT.md
var Compact string

// Memory is the system prompt of the memory collector, which reads a chat's
// new messages and answers with the facts to save as JSON.
//
//go:embed MEMORY.md
var Memory string

// MemoryDigest is the system prompt for summarizing a Bot's long-term
// memories into the briefing its prompt carries; {max} is the size cap.
//
//go:embed MEMORY_DIGEST.md
var MemoryDigest string

// Subagent is the per-run note for a subagent's run. {{NAME}} is replaced with
// its name.
//
//go:embed SUBAGENT.md
var Subagent string

// Mail is the system-prompt section for a Bot that has a mailbox; {address}
// is its address.
//
//go:embed MAIL.md
var Mail string

// Decide is the system-prompt section for the decide tool, there while the
// operator has allowed a decision model; {default} is the default one and
// {models} the allowed ones.
//
//go:embed DECIDE.md
var Decide string

// Tunnels is the system-prompt section for a Bot that has the tunnel tools.
//
//go:embed TUNNELS.md
var Tunnels string
