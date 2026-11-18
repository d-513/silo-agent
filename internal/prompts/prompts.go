package prompts

import _ "embed"

// System is the base system prompt every run gets: the machine, the rules,
// the loop's own tools. It is the same bytes for every Bot.
//
//go:embed SYSTEM.md
var System string

// Lead follows System for every run but a subagent's: what only the agent
// that answers the human does (automations, the Dashboard, the Feed and
// questions, subagents, sections). A subagent has none of those tools.
//
//go:embed LEAD.md
var Lead string

// Network is the system-prompt section for a Bot whose machine is behind the
// egress filter.
//
//go:embed NETWORK.md
var Network string

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

// Onboarding is the interview a new Bot opens its main chat with (and what
// /onboard runs): the opening turn of a run Silo starts, not the owner.
//
//go:embed ONBOARDING.md
var Onboarding string

// Dashboard is the default prompt of the pinned Dashboard automation.
//
//go:embed DASHBOARD.md
var Dashboard string

// Compact is the system prompt for summarizing a conversation that no longer
// fits the model's context window.
//
//go:embed COMPACT.md
var Compact string

// Memory is the system prompt of the memory keeper's runs, which read a
// conversation that went quiet or tidy the store, through the memory tools.
// {infer} is MemoryInfer or MemoryStated.
//
//go:embed MEMORY.md
var Memory string

// MemoryInfer is the keeper's section on keeping what nobody said, while
// memory.infer is on; MemoryStated stands in for it while it is off.
//
//go:embed MEMORY_INFER.md
var MemoryInfer string

//go:embed MEMORY_STATED.md
var MemoryStated string

// MemoryTidy is the default prompt of the pinned Memory automation: what a
// firing of it does, which is tidying the store.
//
//go:embed MEMORY_TIDY.md
var MemoryTidy string

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

// BotMessage is the per-run note for a run that answers another Bot's
// message. {{FROM}} is the sending Bot's name, {{FOR}} whom it asks for.
//
//go:embed BOT_MESSAGE.md
var BotMessage string

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

// Voice is the system-prompt section that says what a spoken turn is, there
// while calls are on. The same text for every Bot.
//
//go:embed VOICE.md
var Voice string

// VoiceTalker is the system prompt of a call's talker; {name} is the Bot's
// and {language} the language it speaks.
//
//go:embed VOICE_TALKER.md
var VoiceTalker string

// Tunnels is the system-prompt section for a Bot that has the tunnel tools.
//
//go:embed TUNNELS.md
var Tunnels string
