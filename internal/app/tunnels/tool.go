package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
	"silo.agent/internal/tunnel"
)

// Usable reports whether tunnels are on and have a domain: the Bot's tunnel
// tools are offered, and the prompt mentions them, only then.
func (s *Service) Usable() bool {
	st, _ := State(s.cfg())
	return st == StateOK
}

// Prompt is the system-prompt note for a Bot that has the tunnel tools; empty
// when tunnels are unusable. It carries nothing per Bot or per run, so it sits
// in the cached prefix.
func (s *Service) Prompt(text string) string {
	if !s.Usable() {
		return ""
	}
	return text
}

// Tool runs the open_tunnel, list_tunnels and close_tunnel chat tools, and the
// same three from Python (structured: JSON instead of prose). Each is gated by
// its own rule, and exposing a service to people other than the owner is a
// separate, asking rule: tunnels.public.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any, structured bool) (string, error) {
	argsJSON, _ := json.Marshal(args)
	auth := func(action string) error {
		_, err := s.host.AuthorizeAction(ctx, bot, runID, security.Tunnels, action, string(argsJSON), "")
		return err
	}
	if err := s.usable(); err != nil {
		return "", plain(err)
	}
	cfg := s.cfg()
	switch name {
	case "open_tunnel":
		return s.open(bot, args, structured, cfg, auth)
	case "list_tunnels":
		if err := auth("list"); err != nil {
			return "", err
		}
		rows, err := s.ForBot(bot.ID)
		if err != nil {
			return "", err
		}
		return listResult(rows, cfg, structured)
	case "close_tunnel":
		if err := auth("close"); err != nil {
			return "", err
		}
		return s.closeTool(bot.ID, args, structured)
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func (s *Service) open(bot *db.Bot, args map[string]any, structured bool, cfg config.Config, auth func(string) error) (string, error) {
	port := toolarg.Int(args, "port")
	public, _ := args["public"].(bool)
	if err := tunnel.CheckPort(port); err != nil {
		return "", err
	}
	if err := auth("open"); err != nil {
		return "", err
	}
	existing, err := s.byPort(bot.ID, port)
	if err != nil {
		return "", err
	}
	// Making something public is asked before it happens, new or not. An
	// existing public tunnel is never made private by asking again.
	if public && (existing == nil || !existing.Public) {
		if err := auth("public"); err != nil {
			return "", err
		}
	}
	if existing != nil {
		if public && !existing.Public {
			if err := s.SetPublic(existing, true); err != nil {
				return "", err
			}
		}
		return openResult(existing, false, cfg, structured)
	}
	t, created, err := s.Declare(bot.ID, port, public, "bot")
	if err != nil {
		return "", plain(err)
	}
	return openResult(t, created, cfg, structured)
}

func (s *Service) closeTool(botID string, args map[string]any, structured bool) (string, error) {
	var t *db.Tunnel
	var err error
	name, _ := args["name"].(string)
	switch name = strings.TrimSpace(name); {
	case name != "":
		if t, err = s.ByName(name); err == nil && t != nil && t.BotID != botID {
			t = nil // another Bot's: as good as absent
		}
	case toolarg.Int(args, "port") != 0:
		t, err = s.byPort(botID, toolarg.Int(args, "port"))
	default:
		return "", errors.New("name or port required")
	}
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", errors.New("no tunnel with that name or port")
	}
	if err := s.Delete(t); err != nil {
		return "", err
	}
	if structured {
		return jsonOf(map[string]any{"closed": t.Name, "port": t.Port})
	}
	return fmt.Sprintf("Closed %s (port %d).", t.Name, t.Port), nil
}

func who(public bool) string {
	if public {
		return "public (anyone with the link can open it)"
	}
	return "private (only the owner, signed in to Silo, can open it)"
}

func view(t *db.Tunnel, cfg config.Config) map[string]any {
	return map[string]any{"name": t.Name, "url": cfg.TunnelURL(t.Name), "port": t.Port, "public": t.Public}
}

func openResult(t *db.Tunnel, created bool, cfg config.Config, structured bool) (string, error) {
	if structured {
		v := view(t, cfg)
		v["created"] = created
		return jsonOf(v)
	}
	return fmt.Sprintf("%s → port %d, %s", cfg.TunnelURL(t.Name), t.Port, who(t.Public)), nil
}

func listResult(rows []db.Tunnel, cfg config.Config, structured bool) (string, error) {
	if structured {
		out := []map[string]any{}
		for i := range rows {
			out = append(out, view(&rows[i], cfg))
		}
		return jsonOf(map[string]any{"tunnels": out})
	}
	if len(rows) == 0 {
		return "No tunnels open.", nil
	}
	var b strings.Builder
	for i := range rows {
		fmt.Fprintf(&b, "%s → port %d, %s\n", cfg.TunnelURL(rows[i].Name), rows[i].Port, who(rows[i].Public))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func jsonOf(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// plain is the human sentence of an error: tool results should read "port 5900
// is reserved", not "invalid_argument: port 5900 is reserved".
func plain(err error) error {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return errors.New(ce.Message())
	}
	return err
}
