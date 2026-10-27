// Package pause is the owner taking a Bot's desktop over: while it is paused
// the Bot's desktop actions (click, type, key, scroll) and its Chromium
// (`chrome_page`, Playwright cells) are refused with a note that the owner is
// in control. `look` still works, so the Bot can see when the screen is
// handed back.
package pause

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/db"
)

// ErrPaused is what the Bot reads when it reaches for a paused desktop.
var ErrPaused = errors.New("the desktop is paused: your owner has taken over the screen and is in control. " +
	"Desktop actions (click, type, key, scroll) and Chromium are refused until they resume you. " +
	"Do not drive the screen or the browser another way; carry on with work that does not need the desktop, " +
	"or say what you need from them and wait")

// Service owns the pause switch.
type Service struct {
	db *gorm.DB
}

func New(gdb *gorm.DB) *Service { return &Service{db: gdb} }

// Paused reports whether the owner holds the Bot's desktop.
func (s *Service) Paused(botID string) bool {
	var n int64
	s.db.Model(&db.DesktopPause{}).Where("bot_id = ?", botID).Count(&n)
	return n > 0
}

// Check is ErrPaused while the Bot's desktop is held, else nil.
func (s *Service) Check(botID string) error {
	if s.Paused(botID) {
		return ErrPaused
	}
	return nil
}

// Gate is the tool.Def gate for the desktop's actions.
func (s *Service) Gate(c *tool.Call) error {
	if c.Bot == nil {
		return nil
	}
	return s.Check(c.Bot.ID)
}

// Set holds or hands back the Bot's desktop.
func (s *Service) Set(botID string, paused bool) error {
	if !paused {
		return s.Drop(botID)
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&db.DesktopPause{BotID: botID}).Error
}

// Drop forgets the switch (a deleted Bot, or a resume).
func (s *Service) Drop(botID string) error {
	return s.db.Where("bot_id = ?", botID).Delete(&db.DesktopPause{}).Error
}

func (s *Service) SetDesktopPaused(ctx context.Context, req *connect.Request[v1.SetDesktopPausedRequest]) (*connect.Response[v1.SetDesktopPausedResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	if err := s.Set(b.ID, req.Msg.GetPaused()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.SetDesktopPausedResponse{Paused: s.Paused(b.ID)}), nil
}
