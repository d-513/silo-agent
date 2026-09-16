package app

import (
	"fmt"
	"strings"

	"silo.agent/internal/db"
	"silo.agent/internal/prompts"
	"silo.agent/internal/skills"
)

// promptSection is one ordered block of the system prompt. An empty body is
// dropped when the builder renders it.
type promptSection struct {
	title string
	body  string
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
	}
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
		out = append(out, promptSection{title: "This conversation", body: b.String()})
	}
	return out
}

// systemPromptBuilder assembles the system message: the base prompt, the Bot
// identity, provider sections in registration order, then SOUL and MEMORY.
type systemPromptBuilder struct {
	base     string
	bot      *db.Bot
	sections []promptSection
}

func (b *systemPromptBuilder) add(s promptSection) {
	if strings.TrimSpace(s.body) == "" {
		return
	}
	b.sections = append(b.sections, s)
}

func (b *systemPromptBuilder) String() string {
	var s strings.Builder
	s.WriteString(b.base)
	if b.bot.Name != "" {
		fmt.Fprintf(&s, "\n\nThis Bot's name is %s.", b.bot.Name)
	}
	if d := strings.TrimSpace(b.bot.Description); d != "" {
		fmt.Fprintf(&s, " Description: %s.", d)
	}
	for _, sec := range b.sections {
		s.WriteString(sec.render())
	}
	s.WriteString("\n\n## SOUL\n")
	if t := strings.TrimSpace(b.bot.Soul); t != "" {
		s.WriteString(t)
	} else {
		s.WriteString("(empty — write it with the soul tool.)")
	}
	s.WriteString("\n\n## MEMORY\n")
	if t := strings.TrimSpace(b.bot.Memory); t != "" {
		s.WriteString(t)
	} else {
		s.WriteString("(empty)")
	}
	if len(b.bot.Memory) > memoryMax {
		fmt.Fprintf(&s, "\n\nMEMORY is over the %d-character cap (now %d). Compact it with `memory` (replace redundant facts with a shorter summary) before adding more.", memoryMax, len(b.bot.Memory))
	}
	return s.String()
}

// buildSystem loads the session and assembles the system prompt. An optional
// origin makes the prompt aware of the channel a run came from.
func (a *App) buildSystem(botID string, origin ...*runOrigin) string {
	if a.DB == nil {
		return prompts.System
	}
	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return prompts.System
	}
	var o *runOrigin
	if len(origin) > 0 {
		o = origin[0]
	}
	pc := a.promptContext(botID, &bot, o)
	b := &systemPromptBuilder{base: prompts.System, bot: &bot}
	for _, p := range a.promptProviders() {
		for _, sec := range p(pc) {
			b.add(sec)
		}
	}
	return b.String()
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
	}
	return pc
}
