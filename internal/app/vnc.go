package app

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/coder/websocket"

	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
)

func (a *App) openBotWS(w http.ResponseWriter, r *http.Request, what string) (*websocket.Conn, *hub.Session, string, bool) {
	u, err := auth.UserFromRequest(a.DB, r)
	if err != nil {
		http.Error(w, "auth", 401)
		return nil, nil, "", false
	}
	botID := r.URL.Query().Get("bot")
	var b db.Bot
	if err := a.DB.First(&b, "id = ? AND user_id = ?", botID, u.ID).Error; err != nil {
		http.Error(w, "not found", 404)
		return nil, nil, "", false
	}
	sess := a.Hub.Get(botID)
	if sess == nil {
		http.Error(w, "no worker", 503)
		return nil, nil, "", false
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: true, // session cookie is the gate; Vite proxies from :5173
	})
	if err != nil {
		log.Printf("%s ws accept bot=%s: %v", what, botID, err)
		return nil, nil, "", false
	}
	return c, sess, botID, true
}

func (a *App) handleVNC(w http.ResponseWriter, r *http.Request) {
	c, sess, botID, ok := a.openBotWS(w, r, "vnc")
	if !ok {
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

func parseConsoleWS(typ websocket.MessageType, data []byte) (hub.ConsoleMsg, bool) {
	if typ == websocket.MessageText {
		var sz struct {
			Rows int32 `json:"rows"`
			Cols int32 `json:"cols"`
		}
		if json.Unmarshal(data, &sz) == nil && sz.Rows > 0 && sz.Cols > 0 {
			return hub.ConsoleMsg{Rows: sz.Rows, Cols: sz.Cols}, true
		}
	}
	if len(data) == 0 {
		return hub.ConsoleMsg{}, false
	}
	return hub.ConsoleMsg{Data: data}, true
}

func (a *App) handleConsole(w http.ResponseWriter, r *http.Request) {
	c, sess, botID, ok := a.openBotWS(w, r, "console")
	if !ok {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	viewID, toBrowser, toWorker := sess.BeginConsole()
	defer sess.EndConsole(viewID)
	log.Printf("console ws open bot=%s view=%d", botID, viewID)
	ctx := r.Context()
	go func() {
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			msg, ok := parseConsoleWS(typ, data)
			if !ok {
				continue
			}
			select {
			case toWorker <- msg:
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
		case m := <-toBrowser:
			if len(m.Data) == 0 {
				continue
			}
			if err := c.Write(ctx, websocket.MessageBinary, m.Data); err != nil {
				return
			}
		}
	}
}
