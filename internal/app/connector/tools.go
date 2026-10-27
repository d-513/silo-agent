package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"silo.agent/internal/app/tool"
	"silo.agent/internal/db"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

// Tools are a Bot's connector tools as tool definitions, so the tool engine
// gates and runs them like any built-in. Every one is a Python tool
// (`tools.<slug>.<fn>`); a connector with ExposeCompletion also offers them to
// the model directly, as `<slug>__<action>`. The order is stable (connector
// slug, then action) because it is part of every request.
func (s *Service) Tools(botID string) []tool.Def {
	views := s.Views(botID)
	sort.SliceStable(views, func(i, j int) bool {
		return toolsgen.Slug(views[i].Conn.Name) < toolsgen.Slug(views[j].Conn.Name)
	})
	var out []tool.Def
	for i := range views {
		v := views[i]
		if v.Link.AuthStatus != StatusOK {
			continue
		}
		var tools []mcpx.Tool
		_ = json.Unmarshal([]byte(v.Link.ToolsJSON), &tools)
		sort.SliceStable(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, t := range tools {
			if t.Name == "" {
				continue
			}
			d := s.toolDef(&v.Link, &v.Conn, t.Name)
			d.Description = directDescription(v.Conn.Name, t.Description)
			d.Params = directSchema(t.InputSchema)
			out = append(out, d)
		}
	}
	return out
}

// Find resolves `tools.<slug>.<action>` from Python. The action need not be
// in the cached list: a server may have gained a tool since the last refresh.
func (s *Service) Find(botID, slug, action string) (*tool.Def, error) {
	link, c, err := s.FindBotConnector(botID, slug)
	if err != nil {
		return nil, err
	}
	if c.Auth == AuthOAuth && link.AuthStatus != StatusOK {
		return nil, errors.New("connector is not authorized")
	}
	d := s.toolDef(link, c, action)
	return &d, nil
}

// toolDef is one connector action as a tool. Its rule is slug.action, with
// the connector's (or the built-in action's) default mode when the Bot has
// set none.
func (s *Service) toolDef(link *db.BotConnector, c *db.Connector, action string) tool.Def {
	slug := toolsgen.Slug(c.Name)
	name := c.Name
	return tool.Def{
		Name:      tool.CompletionName(slug, action),
		Connector: slug, Action: action,
		ExposePython:     true,
		ExposeCompletion: c.ExposeCompletion,
		Mode:             BuiltinMode(c, action),
		OutMode:          security.OutCheck,
		Title:            func(*tool.Call) string { return CallTitle(name, action) },
		Run: func(ctx context.Context, call *tool.Call) (tool.Result, error) {
			out, err := s.Call(ctx, call.Bot, link, c, action, call.ArgsJSON)
			return tool.Result{Text: out}, err
		},
	}
}

// Call runs one connector action that the rules already allowed: Go code for a
// built-in, otherwise the connector's MCP session (redialled once when the
// server dropped it). Secrets are masked out of the result.
func (s *Service) Call(ctx context.Context, bot *db.Bot, link *db.BotConnector, c *db.Connector, action, argsJSON string) (string, error) {
	mask := s.mask(bot.ID)
	if hdr, err := mcpx.HeadersFromJSON(s.Resolve(c, bot.ID).HeadersJSON); err == nil {
		for _, v := range hdr {
			mask.Add(v)
		}
	}
	if c.Transport == TransportBuiltin {
		return s.RunBuiltin(ctx, bot, c, action, argsJSON)
	}
	args := map[string]any{}
	if raw := strings.TrimSpace(argsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", errors.New("invalid args")
		}
	}
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}
	sess, err := s.MCPSession(ctx, link, c)
	if err != nil {
		return "", err
	}
	out, err := mcpx.Call(ctx, sess, action, args)
	if err != nil && errors.Is(err, mcpx.ErrSessionGone) {
		s.DropMCP(link.ID)
		sess, err = s.MCPSession(ctx, link, c)
		if err == nil {
			out, err = mcpx.Call(ctx, sess, action, args)
		}
	}
	if err != nil {
		return "", err
	}
	return mask.Apply(out), nil
}

// CallTitle names a connector call on the thread: "GitHub · create issue".
func CallTitle(name, action string) string {
	a := action
	if i := strings.LastIndex(action, "__"); i >= 0 {
		a = action[i+2:]
	} else if i := strings.LastIndex(action, "."); i >= 0 {
		a = action[i+1:]
	}
	a = strings.ReplaceAll(a, "_", " ")
	if name == "" {
		return a
	}
	return name + " · " + a
}

// directDescription is what the model reads for a connector tool offered
// directly: the server's own words, led by which connector it belongs to.
func directDescription(connector, desc string) string {
	desc = strings.TrimSpace(desc)
	if len(desc) > directDescMax {
		cut := directDescMax
		for cut > 0 && desc[cut]&0xC0 == 0x80 {
			cut--
		}
		desc = desc[:cut] + "…"
	}
	if desc == "" {
		return fmt.Sprintf("%s connector.", connector)
	}
	return fmt.Sprintf("%s connector. %s", connector, desc)
}

// directDescMax caps one tool description: a server may ship pages of it, and
// every exposed tool is sent with every request.
const directDescMax = 1500

// directSchema is a connector tool's input schema as a completion tool's
// parameters: an object schema, whatever the server left out.
func directSchema(schema any) json.RawMessage {
	m, _ := schema.(map[string]any)
	if m == nil {
		return tool.Object(nil)
	}
	out := make(map[string]any, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	out["type"] = "object"
	if _, ok := out["properties"].(map[string]any); !ok {
		out["properties"] = map[string]any{}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return tool.Object(nil)
	}
	return raw
}
