package app

import (
	"fmt"
	"strings"

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

// connectorView joins an attachment link with its connector definition so
// providers do not each re-query the DB.
type connectorView struct {
	link db.BotConnector
	conn db.Connector
}

// promptContext is the per-run snapshot providers read from. It is built from
// the DB and never carries worker state.
type promptContext struct {
	bot        *db.Bot
	connectors []connectorView
	skills     []skills.Info
	// channel is the origin channel when this run came from one; channels is
	// every enabled channel the Bot can send to.
	channel  *db.Channel
	channels []db.Channel
	// recall is this run's auto-recalled memories (volatile, trailing).
	recall string
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
	if len(pc.channels) == 0 && pc.channel == nil {
		return nil
	}
	var out []promptSection
	if len(pc.channels) > 0 {
		var b strings.Builder
		b.WriteString("This Bot is reachable through these channels. Use the `channel` tool to send a message to one (it defaults to the current conversation) and the `chats` tool to read chat history.\n")
		for i := range pc.channels {
			c := &pc.channels[i]
			line := fmt.Sprintf("- %s (adapter: %s)", c.Name, c.Adapter)
			if pc.channel != nil && pc.channel.ID == c.ID {
				line += " — this conversation"
			}
			b.WriteString(line + "\n")
		}
		out = append(out, promptSection{title: "Channels", body: b.String()})
	}
	if pc.channel != nil {
		var b strings.Builder
		fmt.Fprintf(&b, "This conversation is the %s channel “%s”.\n\n", pc.channel.Adapter, pc.channel.Name)
		if p := strings.TrimSpace(pc.channel.Prompt); p != "" {
			b.WriteString(p)
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(prompts.Channel))
		out = append(out, promptSection{title: "This conversation", body: b.String(), trailing: true})
	}
	return out
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
	mem.WriteString("\n\n## MEMORY\n")
	if t := strings.TrimSpace(b.bot.Memory); t != "" {
		mem.WriteString(t)
	} else {
		mem.WriteString("(empty)")
	}
	if len(b.bot.Memory) > memoryMax {
		fmt.Fprintf(&mem, "\n\nMEMORY is over the %d-character cap (now %d). Compact it with `memory` (replace redundant facts with a shorter summary) before adding more.", memoryMax, len(b.bot.Memory))
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

func (b *systemPromptBuilder) String() string {
	var s strings.Builder
	for _, blk := range b.Blocks() {
		s.WriteString(blk.Text)
	}
	return s.String()
}

// buildSystemBlocks loads the session and returns the ordered, cache-aware
// system prompt. An optional origin makes the prompt aware of the channel a run
// came from; that per-run note is placed last.
func (a *App) buildSystemBlocks(botID string, origin ...*runOrigin) []llm.SystemBlock {
	if a.DB == nil {
		return []llm.SystemBlock{{Text: prompts.System}}
	}
	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return []llm.SystemBlock{{Text: prompts.System}}
	}
	var o *runOrigin
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

// buildSystem renders the system prompt as one string.
func (a *App) buildSystem(botID string, origin ...*runOrigin) string {
	var s strings.Builder
	for _, blk := range a.buildSystemBlocks(botID, origin...) {
		s.WriteString(blk.Text)
	}
	return s.String()
}

// promptContext snapshots the session state providers may depend on.
func (a *App) promptContext(botID string, bot *db.Bot, origin *runOrigin) promptContext {
	pc := promptContext{bot: bot, skills: a.enabledSkills(botID)}
	var links []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&links)
	for i := range links {
		var c db.Connector
		if a.DB.First(&c, "id = ?", links[i].ConnectorID).Error != nil {
			continue
		}
		pc.connectors = append(pc.connectors, connectorView{link: links[i], conn: c})
	}
	pc.channels = a.enabledChannels(botID)
	if origin != nil {
		pc.channel = origin.channel
		pc.recall = origin.recall
	}
	return pc
}
