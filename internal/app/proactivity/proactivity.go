// Package proactivity is how much a Bot does unasked: one level per Bot
// (bots.proactivity) that decides whether a background run may write into the
// main chat, and puts the Heartbeat on the level's schedule.
package proactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/db"
)

// The levels (bots.proactivity).
const (
	Off    = "off"
	Quiet  = "quiet"
	Active = "active"
)

// Levels are the levels in order, from doing nothing unasked to the most.
var Levels = []string{Off, Quiet, Active}

// schedules are the Heartbeat's schedules for the levels, in the Bot's time
// zone: none when off, a morning check when quiet, every three hours of the
// day (and never at night) when active.
var schedules = map[string]string{
	Off:    "",
	Quiet:  "0 9 * * *",
	Active: "0 9-21/3 * * *",
}

// Service owns the level and the Heartbeat schedule it implies.
type Service struct {
	db   *gorm.DB
	auto *automation.Service
}

func New(gdb *gorm.DB, auto *automation.Service) *Service {
	return &Service{db: gdb, auto: auto}
}

// Normalize is a level as stored, or an error naming the levels.
func Normalize(level string) (string, error) {
	l := strings.ToLower(strings.TrimSpace(level))
	if _, ok := schedules[l]; !ok {
		return "", fmt.Errorf("proactivity is %s, not %q", strings.Join(Levels, ", "), level)
	}
	return l, nil
}

// Set moves a Bot to a level: the Heartbeat first, then the column, so a
// failed schedule write leaves the Bot where it was.
func (s *Service) Set(botID, level string) (string, string, error) {
	l, err := Normalize(level)
	if err != nil {
		return "", "", err
	}
	sched := schedules[l]
	if err := s.auto.SetHeartbeatSchedule(botID, sched); err != nil {
		return "", "", err
	}
	if err := s.db.Model(&db.Bot{}).Where("id = ?", botID).Update("proactivity", l).Error; err != nil {
		return "", "", err
	}
	return l, sched, nil
}

// Proactive reports whether the Bot may write into its main chat unasked.
func (s *Service) Proactive(botID string) bool {
	var b db.Bot
	s.db.Select("proactivity").Where("id = ?", botID).Limit(1).Find(&b)
	return b.Proactivity == Active
}

// Custom reports whether the Bot's Heartbeat runs on a schedule of its own
// rather than its level's (someone set it by hand).
func (s *Service) Custom(botID, level string) bool {
	sched, enabled, ok := s.auto.Heartbeat(botID)
	if !ok {
		return false
	}
	want, known := schedules[level]
	return known && (sched != want || !enabled && want != "")
}

// Tool is set_proactivity: the Bot moves itself to the level its owner
// chose (in the interview, or when asked in a chat).
func (s *Service) Tool(_ context.Context, c *tool.Call) (string, error) {
	level, sched, err := s.Set(c.Bot.ID, c.Str("level"))
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]any{"proactivity": level, "heartbeat": sched})
	return string(out), nil
}
