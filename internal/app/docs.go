package app

import (
	"errors"
	"fmt"
	"strings"

	"silo.agent/internal/db"
	"silo.agent/internal/prompts"
)

const (
	memoryMax = 8000
	soulMax   = 8000
)

const defaultSoul = `Who you are, how you speak, and hard rules. You and the human both edit this.`

func buildSystem(b *db.Bot, connectors string) string {
	var s strings.Builder
	s.WriteString(prompts.System)
	if b.Name != "" {
		fmt.Fprintf(&s, "\n\nThis Bot's name is %s.", b.Name)
	}
	if d := strings.TrimSpace(b.Description); d != "" {
		fmt.Fprintf(&s, " Description: %s.", d)
	}
	if connectors != "" {
		s.WriteString(connectors)
	}
	s.WriteString("\n\n## SOUL\n")
	if t := strings.TrimSpace(b.Soul); t != "" {
		s.WriteString(t)
	} else {
		s.WriteString("(empty — write it with the soul tool.)")
	}
	s.WriteString("\n\n## MEMORY\n")
	if t := strings.TrimSpace(b.Memory); t != "" {
		s.WriteString(t)
	} else {
		s.WriteString("(empty)")
	}
	if len(b.Memory) > memoryMax {
		fmt.Fprintf(&s, "\n\nMEMORY is over the %d-character cap (now %d). Compact it with `memory` (replace redundant facts with a shorter summary) before adding more.", memoryMax, len(b.Memory))
	}
	return s.String()
}

func applyPatch(cur, old, neu string) (string, error) {
	if old == "" {
		return "", errors.New("old_text required")
	}
	n := strings.Count(cur, old)
	if n == 0 {
		return "", errors.New("old_text not found")
	}
	if n > 1 {
		return "", fmt.Errorf("old_text matches %d times; make it unique", n)
	}
	return strings.Replace(cur, old, neu, 1), nil
}

func applySoul(cur, content, old, neu string) (string, error) {
	var next string
	switch {
	case old != "":
		out, err := applyPatch(cur, old, neu)
		if err != nil {
			return "", err
		}
		next = out
	case content != "":
		next = content
	default:
		return "", errors.New("pass content to replace, or old_text/new_text to patch")
	}
	if len(next) > soulMax && len(next) >= len(cur) {
		return "", fmt.Errorf("SOUL is %d characters; cap is %d. Shorten it, then retry.", max(len(cur), len(next)), soulMax)
	}
	return next, nil
}

func applyMemory(cur, appendText, old, neu string) (string, error) {
	var next string
	switch {
	case old != "":
		out, err := applyPatch(cur, old, neu)
		if err != nil {
			return "", err
		}
		next = out
	case appendText != "":
		if cur == "" {
			next = appendText
		} else {
			next = strings.TrimRight(cur, "\n") + "\n" + appendText
		}
	default:
		return "", errors.New("pass append, or old_text/new_text to edit or compact")
	}
	if len(next) > memoryMax && len(next) >= len(cur) {
		return "", fmt.Errorf("MEMORY is %d/%d characters. Compact it: replace redundant entries with a shorter summary using old_text/new_text, then retry.", len(cur), memoryMax)
	}
	return next, nil
}

func (a *App) execDoc(botID, name string, args map[string]any) (string, error) {
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	var b db.Bot
	if err := a.DB.First(&b, "id = ?", botID).Error; err != nil {
		return "", err
	}
	var next string
	var err error
	switch name {
	case "soul":
		next, err = applySoul(b.Soul, str("content"), str("old_text"), str("new_text"))
		if err != nil {
			return "", err
		}
		b.Soul = next
	case "memory":
		next, err = applyMemory(b.Memory, str("append"), str("old_text"), str("new_text"))
		if err != nil {
			return "", err
		}
		b.Memory = next
	default:
		return "", fmt.Errorf("unknown doc %s", name)
	}
	if err := a.DB.Save(&b).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("ok (%d chars)", len(next)), nil
}
