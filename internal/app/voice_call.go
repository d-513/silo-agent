package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/run"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// ViewBot is a Bot as the caller sees it (voice's Host).
func (a *App) ViewBot(ctx context.Context, b *db.Bot) *v1.Bot { return a.viewBot(ctx, b) }

// Subscribe is every event of a Bot as it is published (voice's Host).
func (a *App) Subscribe(botID string) (<-chan *v1.RunEvent, func()) { return a.Bus.Subscribe(botID) }

// Lost closes when the person can no longer use the Bot (voice's Host).
func (a *App) Lost(botID, userID string) (<-chan struct{}, func()) {
	return a.Gone.Lost(botID, userID)
}

// handleVoiceCall opens a call: /voice/call?bot=…&chat=… where chat is a chat
// the caller may write in, empty for their main chat, or "new" for a chat of
// its own. The checks are /console's: the Origin, the session, the Bot.
func (a *App) handleVoiceCall(w http.ResponseWriter, r *http.Request) {
	if !socketOrigin(r, a.cfg().PublicURL) {
		log.Printf("voice ws refused: origin %q is not this site", r.Header.Get("Origin"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	u, err := auth.UserFromRequest(a.DB, r, a.cfg().HostCookies())
	if err != nil {
		access.HTTPSessionError(w, err)
		return
	}
	ctx := access.WithUser(r.Context(), u)
	q := r.URL.Query()
	b, err := a.bot(ctx, q.Get("bot"), access.Use)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if access.Suspended(a.DB, b.ID) {
		http.Error(w, "this Bot is suspended: it has no manager", http.StatusConflict)
		return
	}
	var ch *db.Chat
	switch chatID := strings.TrimSpace(q.Get("chat")); chatID {
	case "":
		ch, err = a.mainChat(b.ID, u.ID)
	case "new":
		ch, err = a.newCallChat(b.ID, u.ID)
	default:
		ch, err = a.chat(ctx, b.ID, chatID, access.Write)
		if err == nil {
			err = writableChat(ch)
		}
	}
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	a.Voice.ServeCall(w, r, u.ID, b, ch)
}

// newCallChat is a chat of its own for a call started from the New chat
// page. Its title comes from the first thing said, like a typed first message.
func (a *App) newCallChat(botID, userID string) (*db.Chat, error) {
	now := time.Now()
	ch := &db.Chat{ID: ids.New(), BotID: botID, UserID: userID, Title: "New chat", CreatedAt: now, UpdatedAt: now}
	return ch, a.DB.Create(ch).Error
}

// RecordSpoken writes an exchange a call's talker answered on its own: what
// the human said and what they heard back, as a run of its own, so the Bot
// reads it in order next time. While a reply runs in the chat it would land
// between that run's turns, so it is refused with run.ErrBusy and the call
// writes it once the run is over.
func (a *App) RecordSpoken(botID, chatID, said, reply string, sp run.Spoken) error {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if a.LiveRunID(botID, chatID) != "" {
		return run.ErrBusy
	}
	var ch db.Chat
	if a.DB.Where("id = ? AND bot_id = ?", chatID, botID).Limit(1).Find(&ch); ch.ID == "" {
		return errors.New("no such chat")
	}
	now := time.Now()
	r := db.Run{ID: ids.New(), BotID: botID, ChatID: chatID, Origin: "voice", Status: "done", UserID: ch.UserID, CreatedAt: now}
	if err := a.DB.Create(&r).Error; err != nil {
		return err
	}
	a.emitUserAt(botID, chatID, r.ID, ids.New(), now, a.botZone(botID), said, run.Extra{Spoken: &sp}, "")
	if strings.TrimSpace(reply) != "" {
		a.Emit(botID, chatID, r.ID, "section", reply, "voice")
	}
	a.Emit(botID, chatID, r.ID, "done", "done", "")
	a.DB.Model(&db.Chat{}).Where("id = ?", chatID).Update("updated_at", now)
	return nil
}

// StopChat stops the reply running in a chat (voice's Host: "never mind").
func (a *App) StopChat(botID, chatID string) { a.StopChatLive(botID, chatID) }
