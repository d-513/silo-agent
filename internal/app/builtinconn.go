package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/builtin"
	_ "silo.agent/internal/builtin/email"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
)

// builtinCallWait bounds one built-in action (an IMAP fetch, an SMTP send).
const builtinCallWait = 2 * time.Minute

// builtinOf returns the registry entry behind a builtin connector row.
func builtinOf(c *db.Connector) (builtin.Connector, bool) {
	if c == nil || c.Transport != transportBuiltin {
		return nil, false
	}
	return builtin.Lookup(c.Builtin)
}

func decodeStrMap(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// builtinConfig merges a row's plain and secret values over field defaults.
func builtinConfig(d builtin.Descriptor, c *db.Connector) builtin.Config {
	vals := decodeStrMap(c.ConfigJSON)
	for k, v := range decodeStrMap(c.SecretsJSON) {
		vals[k] = v
	}
	return builtin.Resolve(d, vals)
}

// applyBuiltinConfig writes submitted field values onto the row: secrets to
// SecretsJSON, the rest to ConfigJSON. A blank secret keeps the stored one,
// so the form never has to echo it back.
func applyBuiltinConfig(c *db.Connector, in map[string]string) error {
	if len(in) == 0 {
		return nil
	}
	bc, ok := builtinOf(c)
	if !ok {
		return errors.New("config is only for built-in connectors")
	}
	d := bc.Descriptor()
	plain := decodeStrMap(c.ConfigJSON)
	secret := decodeStrMap(c.SecretsJSON)
	for k, v := range in {
		f, ok := d.Field(k)
		if !ok {
			return fmt.Errorf("unknown field %q", k)
		}
		if f.Type == builtin.FieldSecret {
			if v != "" {
				secret[k] = v
			}
			continue
		}
		v = strings.TrimSpace(v)
		if f.Type == builtin.FieldSelect && v != "" && !hasOption(f, v) {
			return fmt.Errorf("%s: %q is not an option", f.Label, v)
		}
		plain[k] = v
	}
	c.ConfigJSON = mustJSON(plain)
	c.SecretsJSON = mustJSON(secret)
	return nil
}

func hasOption(f builtin.Field, v string) bool {
	for _, o := range f.Options {
		if o.Value == v {
			return true
		}
	}
	return false
}

// builtinTools is the tool list a builtin exposes, in the shape the MCP path
// caches, so pushTools, rules, and the prompt treat both alike.
func builtinTools(d builtin.Descriptor) []mcpx.Tool {
	out := make([]mcpx.Tool, 0, len(d.Tools))
	for _, t := range d.Tools {
		out = append(out, mcpx.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Params})
	}
	return out
}

// refreshBuiltin checks a builtin's config and caches its tools. A config
// with a required field unset waits for the human (needs_auth) instead of
// failing.
func (a *App) refreshBuiltin(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	bc, ok := builtinOf(c)
	if !ok {
		return fmt.Errorf("unknown built-in connector %q", c.Builtin)
	}
	d := bc.Descriptor()
	cfg := builtinConfig(d, c)
	if f := builtin.Missing(d, cfg); f != nil {
		row.AuthStatus = statusNeedsAuth
		row.StatusDetail = "Fill in " + f.Label + " in the connector's settings."
		row.LastError = ""
		a.DB.Save(row)
		return nil
	}
	if err := bc.Check(ctx, cfg); err != nil {
		return err
	}
	return a.storeTools(row, c, builtinTools(d))
}

// builtinMode is a builtin action's default rule: the tool's own mode, then
// the connector's.
func builtinMode(c *db.Connector, action string) string {
	if bc, ok := builtinOf(c); ok {
		if t, ok := bc.Descriptor().Tool(action); ok {
			return t.Mode
		}
	}
	return c.DefaultMode
}

// runBuiltin runs one builtin action after authorizeAction allowed it.
// Every secret is masked out of the result.
func (a *App) runBuiltin(ctx context.Context, bot *db.Bot, c *db.Connector, action, argsJSON string) (string, error) {
	bc, ok := builtinOf(c)
	if !ok {
		return "", fmt.Errorf("unknown built-in connector %q", c.Builtin)
	}
	d := bc.Descriptor()
	t, ok := d.Tool(action)
	if !ok {
		return "", fmt.Errorf("%s has no action %q", c.Name, action)
	}
	cfg := builtinConfig(d, c)
	mask := a.Mask(bot.ID)
	for k, v := range decodeStrMap(c.SecretsJSON) {
		if f, ok := d.Field(k); ok && f.Type == builtin.FieldSecret {
			mask.Add(v)
		}
	}
	if f := builtin.Missing(d, cfg); f != nil {
		return "", fmt.Errorf("%s is not set up: fill in %s on the Connectors tab", c.Name, f.Label)
	}
	raw := strings.TrimSpace(argsJSON)
	if raw == "" {
		raw = "{}"
	}
	cctx, cancel := context.WithTimeout(ctx, builtinCallWait)
	defer cancel()
	res, err := t.Run(cctx, botEnv{a: a, botID: bot.ID}, cfg, json.RawMessage(raw))
	if err != nil {
		return "", errors.New(mask.Apply(err.Error()))
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return mask.Apply(string(b)), nil
}

// botEnv is the Bot's workspace as a builtin sees it, through the worker.
type botEnv struct {
	a     *App
	botID string
}

func envPath(rel string) (string, error) {
	p := relWorkspace(rel)
	if p == "" {
		return "", errors.New("path required")
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("%s is outside /workspace", rel)
	}
	return p, nil
}

func (e botEnv) ReadFile(ctx context.Context, rel string) (string, []byte, error) {
	p, err := envPath(rel)
	if err != nil {
		return "", nil, err
	}
	if !e.a.waitWorker(ctx, e.botID, 2*time.Minute) {
		return "", nil, errors.New("the Bot's machine is not running")
	}
	f, err := e.a.workspaceAttachment(ctx, e.botID, p)
	if err != nil {
		return "", nil, err
	}
	return f.Name, f.Data, nil
}

func (e botEnv) WriteFile(ctx context.Context, rel string, data []byte) error {
	p, err := envPath(rel)
	if err != nil {
		return err
	}
	if len(data) > putFileMax {
		return fmt.Errorf("file too large (max %d MB)", putFileMax>>20)
	}
	if !e.a.waitWorker(ctx, e.botID, 2*time.Minute) {
		return errors.New("the Bot's machine is not running")
	}
	_, err = e.a.callWorker(ctx, e.botID, &v1.Cmd{Body: &v1.Cmd_PutFile{PutFile: &v1.PutFileCmd{Path: p, Data: data}}})
	return err
}

// attachBuiltin copies a builtin library preset onto a Bot with its config.
func (a *App) attachBuiltin(ctx context.Context, botID string, lib *db.Connector, m *v1.CreateBotConnectorRequest) (*v1.BotConnector, error) {
	row := cloneLibrary(lib, botID)
	if n := strings.TrimSpace(m.GetName()); n != "" {
		row.Name = n
	}
	if d := strings.TrimSpace(m.GetDescription()); d != "" {
		row.Description = d
	}
	if p := strings.TrimSpace(m.GetPrompt()); p != "" {
		row.Prompt = p
	}
	if m.GetDefaultMode() != "" {
		row.DefaultMode = security.Rule(m.GetDefaultMode())
	}
	if err := applyBuiltinConfig(&row, m.GetConfig()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return a.putBotConnector(ctx, botID, row)
}

// libraryBuiltin loads a library row when it is a builtin preset.
func (a *App) libraryBuiltin(id string) *db.Connector {
	if id == "" {
		return nil
	}
	var lib db.Connector
	if a.DB.Where("id = ? AND kind = ? AND transport = ?", id, catalog.KindLibrary, transportBuiltin).Limit(1).Find(&lib); lib.ID == "" {
		return nil
	}
	return &lib
}

// protoBuiltin fills the builtin parts of a Connector message: the declared
// fields, the plain values, and which secrets are set (never their values).
func protoBuiltin(c *db.Connector, out *v1.Connector) {
	out.Builtin = c.Builtin
	bc, ok := builtinOf(c)
	if !ok {
		return
	}
	d := bc.Descriptor()
	for _, f := range d.Fields {
		pf := &v1.ChannelField{
			Key: f.Key, Label: f.Label, Description: f.Description, Type: string(f.Type),
			Required: f.Required, Secret: f.Type == builtin.FieldSecret, Advanced: f.Advanced,
		}
		for _, o := range f.Options {
			pf.Options = append(pf.Options, &v1.ChannelFieldOption{Value: o.Value, Label: o.Label})
		}
		out.Fields = append(out.Fields, pf)
	}
	// A library preset stores no values, so it shows the defaults a new copy
	// starts from.
	out.Config = map[string]string{}
	plain := decodeStrMap(c.ConfigJSON)
	for _, f := range d.Fields {
		if f.Type == builtin.FieldSecret {
			continue
		}
		if v, ok := plain[f.Key]; ok {
			out.Config[f.Key] = v
		} else {
			out.Config[f.Key] = f.Default
		}
	}
	for k, v := range decodeStrMap(c.SecretsJSON) {
		if v != "" {
			out.SecretsSet = append(out.SecretsSet, k)
		}
	}
	sort.Strings(out.SecretsSet)
}
