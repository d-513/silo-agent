package app

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"

	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
)

// socketOrigin reports whether a browser page at this Origin may open a Bot's
// desktop or console socket. CORS does not hold a WebSocket back and the
// session cookie rides along from any page of the same site (a tunnelled app
// on a sibling subdomain is one), so the Origin is the only thing that tells
// the console's own page from somebody else's. It must be the host the request
// came to (Vite and a reverse proxy both keep it) or the operator's public_url.
// No Origin at all is a native client, which no page can pretend to be.
func socketOrigin(r *http.Request, publicURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	o, err := url.Parse(origin)
	if err != nil || o.Host == "" {
		return false
	}
	if strings.EqualFold(o.Host, r.Host) {
		return true
	}
	pub, err := url.Parse(strings.TrimSpace(publicURL))
	return err == nil && pub.Host != "" && strings.EqualFold(o.Host, pub.Host) && strings.EqualFold(o.Scheme, pub.Scheme)
}

func (a *App) openBotWS(w http.ResponseWriter, r *http.Request, what string) (*websocket.Conn, *hub.Session, string, bool) {
	if !socketOrigin(r, a.cfg().PublicURL) {
		log.Printf("%s ws refused: origin %q is not this site", what, r.Header.Get("Origin"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, nil, "", false
	}
	u, err := auth.UserFromRequest(a.DB, r, a.cfg().HostCookies())
	if err != nil {
		access.HTTPSessionError(w, err)
		return nil, nil, "", false
	}
	botID := r.URL.Query().Get("bot")
	var b db.Bot
	if err := a.DB.First(&b, "id = ? AND user_id = ?", botID, u.ID).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, "", false
	}
	sess := a.Hub.Get(botID)
	if sess == nil {
		http.Error(w, "no worker", http.StatusServiceUnavailable)
		return nil, nil, "", false
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: true, // socketOrigin has checked it, against public_url too
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
