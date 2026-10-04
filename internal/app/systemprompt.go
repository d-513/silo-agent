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
// dropped when the builder renders it. A trailing section is placed after SOUL
// and MEMORY so per-run content never invalidates the cached prefix.
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
	// recall is this run's auto-recalled memories (volatile, trailing).
	recall string
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
		a.connectorSections,
		a.skillSections,
		a.channelSections,
		a.driveSections,
		a.knowledgeSections,
		a.automationSections,
		a.subagentSections,
		a.recallSections,
	}
}

// recallSections places this run's auto-recalled memories after the cache
// breakpoints: they change with every message.
func (a *App) recallSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Recalled memories", body: pc.recall, trailing: true}}
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

// driveSections lists the Bot's drives for the session tier.
func (a *App) driveSections(pc promptContext) []promptSection {
	return []promptSection{{title: "Drives", body: a.Drives.Prompt(pc.drives)}}
}

// systemPromptBuilder assembles the system prompt as ordered cache tiers:
// (1) base + identity, (2) session config (connectors, skills, channels),
// (3) SOUL, (4) MEMORY, (5) per-run trailing sections. Stable content leads so
// prompt caches keep the longest prefix.
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
	var stable strings.Builder
	stable.WriteString(b.base)
	if b.bot.Name != "" {
		fmt.Fprintf(&stable, "\n\nThis Bot's name is %s.", b.bot.Name)
	}
	if d := strings.TrimSpace(b.bot.Description); d != "" {
		fmt.Fprintf(&stable, " Description: %s.", d)
	}
	var session strings.Builder
	for _, sec := range b.sections {
		session.WriteString(sec.render())
	}
	var trailing strings.Builder
	for _, sec := range b.trailing {
		trailing.WriteString(sec.render())
	}

	var soul strings.Builder
	soul.WriteString("\n\n## SOUL\n")
	if t := strings.TrimSpace(b.bot.Soul); t != "" {
		soul.WriteString(t)
	} else {
		soul.WriteString("(empty — write it with the soul tool.)")
	}
	var mem strings.Builder
	mem.WriteString("\n\n## CORE MEMORY\n")
	if t := strings.TrimSpace(b.bot.Memory); t != "" {
		mem.WriteString(t)
	} else {
		mem.WriteString("(empty)")
	}
	if len(b.bot.Memory) > coreMemoryMax {
		fmt.Fprintf(&mem, "\n\nCORE MEMORY is over the %d-character cap (now %d). Compact it with `core_memory` (replace redundant facts with a shorter summary) before adding more.", coreMemoryMax, len(b.bot.Memory))
	}

	raw := []llm.SystemBlock{
		{Text: stable.String(), CacheAfter: true},
		{Text: session.String(), CacheAfter: true},
		{Text: soul.String(), CacheAfter: true},
		{Text: mem.String()},
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
	if origin != nil {
		pc.channel = origin.Channel
		pc.recall = origin.Recall
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
