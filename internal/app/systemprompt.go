package app

import (
	"fmt"
	"strings"

	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/channel"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/app/knowledge"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/skill"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/skills"
)

// promptSection is one ordered block of the system prompt. An empty body is
// dropped when the builder renders it. A trailing section is placed after the
// last cache breakpoint: it belongs to one conversation, or changes on its own.
type promptSection struct {
	title    string
	body     string
	trailing bool
}

func (s promptSection) render() string {
	body := strings.TrimSpace(s.body)
	if body == "" {
		return ""
	}
	if s.title == "" {
		return "\n\n" + body
	}
	return "\n\n## " + s.title + "\n" + body
}

// promptContext is the per-run snapshot providers read from. It is built from
// the DB and never carries worker state.
type promptContext struct {
	bot        *db.Bot
	connectors []connector.View
	skills     []skills.Info
	// channel is the origin channel when this run came from one; channels is
	// every enabled channel the Bot can send to.
	channel  *db.Channel
	channels []db.Channel
	// automation is set when a scheduled automation started this run.
	automation *db.Automation
	// memories is the Bot's long-term memory section (all of them, or their
	// digest); it changes whenever one is saved, so it trails the cached tiers.
	memories string
	// subagent is set when this run is a subagent's.
	subagent *db.Subagent
	// drives are the Bot's saved drives (mounted under /workspace/drives).
	drives []db.Drive
	// knowledge are the Bot's indexed folders (search_docs).
	knowledge []db.KnowledgeFolder
}

// promptProvider contributes ordered sections for the current session.
type promptProvider func(promptContext) []promptSection

// promptProviders is the ordered list of session-dependent prompt sources. The
// main SYSTEM.md always leads; append a provider here to extend the engine.
func (a *App) promptProviders() []promptProvider {
	return []promptProvider{
		a.memorySections,
		a.connectorSections,
		a.skillSections,
		a.channelSections,
		a.driveSections,
		a.knowledgeSections,
		a.tunnelSections,
		a.mailSections,
		a.automationSections,
		a.subagentSections,
	}
}

// memorySections is the Bot's long-term memory, first among the trailing
// sections: the ones after it belong to a single conversation.
func (a *App) memorySections(pc promptContext) []promptSection {
	return []promptSection{{title: "Long-term memories", body: pc.memories, trailing: true}}
}

// channelSections tells the model what channels exist and, when the run came
// from one, embeds that channel's user-configured prompt plus the section
// delivery contract.
func (a *App) channelSections(pc promptContext) []promptSection {
	return []promptSection{
		{title: "Channels", body: channel.ListPrompt(pc.channels, pc.channel)},
		{title: "This conversation", body: channel.ConversationPrompt(pc.channel), trailing: true},
	}
}

// tunnelSections explains the tunnel tools, only while the operator has tunnels
// on and a domain for them. The text is the same for every Bot, so it stays in
// the cached prefix.
func (a *App) tunnelSections(promptContext) []promptSection {
	return []promptSection{{title: "Tunnels", body: a.Tunnels.Prompt(prompts.Tunnels)}}
}

// mailSections gives the Bot its own address and the mail tools, only while
// the operator has mail on and a domain for it. The address changes only when
// the owner rotates it, so the section sits in the session tier.
func (a *App) mailSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Mailbox", body: a.Mail.Prompt(prompts.Mail, pc.bot.ID)}}
}

// driveSections lists the Bot's drives for the session tier.
func (a *App) driveSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Drives", body: a.Drives.Prompt(pc.drives)}}
}

// systemPromptBuilder assembles the system prompt as ordered cache tiers, the
// longest-lived first, each ending in a cache breakpoint:
//
//  1. the base prompt — the same bytes for every Bot, so with the tool list
//     it is one cache entry shared by all of them;
//  2. this Bot's session: its name, connectors, skills, channels, drives;
//  3. SOUL and CORE MEMORY, which the Bot edits now and then.
//
// What follows has no breakpoint of its own (a provider allows four, and the
// last belongs to the conversation): long-term memories, then the notes of
// this one conversation. A run builds the prompt once and never rebuilds it.
type systemPromptBuilder struct {
	base     string
	bot      *db.Bot
	sections []promptSection
	trailing []promptSection
}

func (b *systemPromptBuilder) add(s promptSection) {
	if strings.TrimSpace(s.body) == "" {
		return
	}
	b.sections = append(b.sections, s)
}

// Blocks renders the prompt as cache-aware blocks. A block with CacheAfter ends
// a provider cache breakpoint. Empty blocks are dropped and their breakpoint
// carries back to the previous block.
func (b *systemPromptBuilder) Blocks() []llm.SystemBlock {
	var session strings.Builder
	if b.bot.Name != "" {
		fmt.Fprintf(&session, "\n\nThis Bot's name is %s.", b.bot.Name)
		if d := strings.TrimSpace(b.bot.Description); d != "" {
			fmt.Fprintf(&session, " Description: %s.", d)
		}
	}
	for _, sec := range b.sections {
		session.WriteString(sec.render())
	}
	var trailing strings.Builder
	for _, sec := range b.trailing {
		trailing.WriteString(sec.render())
	}

	// mem continues the SOUL tier.
	var soul strings.Builder
	soul.WriteString("\n\n## SOUL\n")
	if t := strings.TrimSpace(b.bot.Soul); t != "" {
		soul.WriteString(t)
	} else {
		soul.WriteString("(empty — write it with the soul tool.)")
	}
	mem := &soul
	mem.WriteString("\n\n## CORE MEMORY\n")
	if t := strings.TrimSpace(b.bot.Memory); t != "" {
		mem.WriteString(t)
	} else {
		mem.WriteString("(empty)")
	}
	if len(b.bot.Memory) > coreMemoryMax {
		fmt.Fprintf(mem, "\n\nCORE MEMORY is over the %d-character cap (now %d). Compact it with `core_memory` (replace redundant facts with a shorter summary) before adding more.", coreMemoryMax, len(b.bot.Memory))
	}

	raw := []llm.SystemBlock{
		{Text: b.base, CacheAfter: true},
		{Text: session.String(), CacheAfter: true},
		{Text: soul.String(), CacheAfter: true},
		{Text: trailing.String()},
	}
	out := make([]llm.SystemBlock, 0, len(raw))
	for _, blk := range raw {
		if strings.TrimSpace(blk.Text) == "" {
			if blk.CacheAfter && len(out) > 0 {
				out[len(out)-1].CacheAfter = true
			}
			continue
		}
		out = append(out, blk)
	}
	return out
}

// buildSystemBlocks loads the session and returns the ordered, cache-aware
// system prompt. An optional origin makes the prompt aware of the channel a run
// came from; that per-run note is placed last.
func (a *App) buildSystemBlocks(botID string, origin ...*run.Origin) []llm.SystemBlock {
	if a.DB == nil {
		return []llm.SystemBlock{{Text: prompts.System}}
	}
	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return []llm.SystemBlock{{Text: prompts.System}}
	}
	var o *run.Origin
	if len(origin) > 0 {
		o = origin[0]
	}
	pc := a.promptContext(botID, &bot, o)
	b := &systemPromptBuilder{base: prompts.System, bot: &bot}
	for _, p := range a.promptProviders() {
		for _, sec := range p(pc) {
			if sec.trailing {
				if strings.TrimSpace(sec.body) != "" {
					b.trailing = append(b.trailing, sec)
				}
				continue
			}
			b.add(sec)
		}
	}
	return b.Blocks()
}

// promptContext snapshots the session state providers may depend on.
func (a *App) promptContext(botID string, bot *db.Bot, origin *run.Origin) promptContext {
	pc := promptContext{bot: bot, skills: a.Skills.Enabled(botID)}
	pc.connectors = a.Connectors.Views(botID)
	pc.channels = a.Channels.Enabled(botID)
	pc.drives = a.Drives.BotDrives(botID)
	pc.knowledge = a.Knowledge.Folders(botID)
	pc.memories = a.Memory.Prompt(botID)
	if origin != nil {
		pc.channel = origin.Channel
		pc.automation = origin.Automation
		pc.subagent = origin.Subagent
	}
	return pc
}

// knowledgeSections lists the indexed folders for the session tier.
func (a *App) knowledgeSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Searchable documents", body: knowledge.Prompt(pc.knowledge)}}
}

// automationSections adds the per-run note when an automation started the run.
func (a *App) automationSections(pc promptContext) []promptSection {
	if pc.automation == nil {
		return nil
	}
	return []promptSection{{title: "This run", body: automation.Prompt(pc.automation), trailing: true}}
}

// skillSections lists the enabled skills for the session tier.
func (a *App) skillSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Skills", body: skill.Prompt(pc.skills)}}
}

// connectorSections lists the attached connectors for the session tier.
func (a *App) connectorSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Connectors", body: a.Connectors.Prompt(pc.connectors)}}
}
