package app

import (
	"log"
	"net/http"

	"github.com/coder/websocket"

	"silo.agent/internal/auth"
	"silo.agent/internal/db"
)

func (a *App) handleVNC(w http.ResponseWriter, r *http.Request) {
	u, err := auth.UserFromRequest(a.DB, r)
	if err != nil {
		http.Error(w, "auth", 401)
		return
	}
	botID := r.URL.Query().Get("bot")
	var b db.Bot
	if err := a.DB.First(&b, "id = ? AND user_id = ?", botID, u.ID).Error; err != nil {
		http.Error(w, "not found", 404)
		return
	}
	sess := a.Hub.Get(botID)
	if sess == nil {
		http.Error(w, "no worker", 503)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: true, // session cookie is the gate; Vite proxies from :5173
	})
	if err != nil {
		log.Printf("vnc ws accept bot=%s: %v", botID, err)
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	viewID, toBrowser, toWorker := sess.BeginViewer()
	defer sess.EndViewer(viewID)
	log.Printf("vnc ws open bot=%s view=%d", botID, viewID)
	ctx := r.Context()
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			select {
			case toWorker <- data:
			case <-ctx.Done():
				return
			case <-sess.Done():
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sess.Done():
			return
		case frame := <-toBrowser:
			if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
				return
			}
		}
	}
}
