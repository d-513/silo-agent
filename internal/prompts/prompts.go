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
