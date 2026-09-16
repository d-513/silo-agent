package prompts

import _ "embed"

//go:embed SYSTEM.md
var System string

// Channel is appended to the system prompt when a run replies over a channel.
//
//go:embed CHANNEL.md
var Channel string
