package app

import (
	"encoding/json"
	"strings"

	"silo.agent/internal/config"
	"silo.agent/internal/db"
)

// connectorVars is the effective connector variable map, nil-safe.
func (a *App) connectorVars() map[string]string {
	if a.Store == nil {
		return nil
	}
	return a.Store.ConnectorVarMap()
}

// resolveConnector returns a copy of c with operator connector variables
// (${NAME}) expanded in every technical field. Unknown references are left
// untouched. The raw row keeps the placeholders; only the connection path sees
// the resolved values, so changing a variable takes effect on the next
// reconnect without rewriting connectors.
func (a *App) resolveConnector(c *db.Connector) *db.Connector {
	if c == nil {
		return c
	}
	vars := a.connectorVars()
	if len(vars) == 0 {
		return c
	}
	out := *c
	out.HTTPURL = config.ExpandVars(c.HTTPURL, vars)
	out.OAuthClientID = config.ExpandVars(c.OAuthClientID, vars)
	out.OAuthClientSecret = config.ExpandVars(c.OAuthClientSecret, vars)
	out.StdioCommand = config.ExpandVars(c.StdioCommand, vars)
	out.StdioImage = config.ExpandVars(c.StdioImage, vars)
	out.Prompt = config.ExpandVars(c.Prompt, vars)
	out.HeadersJSON = expandJSONMap(c.HeadersJSON, vars)
	out.StdioArgsJSON = expandJSONStrings(c.StdioArgsJSON, vars)
	out.EnvJSON = expandEnvValues(c.EnvJSON, vars)
	return &out
}

// expandJSONMap expands ${NAME} in both keys and values of a JSON string map.
func expandJSONMap(raw string, vars map[string]string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	m := map[string]string{}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return raw
	}
	changed := false
	out := make(map[string]string, len(m))
	for k, v := range m {
		nk := config.ExpandVars(k, vars)
		nv := config.ExpandVars(v, vars)
		if nk != k || nv != v {
			changed = true
		}
		out[nk] = nv
	}
	if !changed {
		return raw
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return string(b)
}

// expandJSONStrings expands ${NAME} in a JSON string list (STDIO arguments).
func expandJSONStrings(raw string, vars map[string]string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil {
		return raw
	}
	changed := false
	for i, v := range list {
		nv := config.ExpandVars(v, vars)
		if nv != v {
			list[i] = nv
			changed = true
		}
	}
	if !changed {
		return raw
	}
	b, err := json.Marshal(list)
	if err != nil {
		return raw
	}
	return string(b)
}

// expandEnvValues expands ${NAME} in the values (not the secret names) of a
// connector's STDIO environment.
func expandEnvValues(raw string, vars map[string]string) string {
	entries := parseEnv(raw)
	if len(entries) == 0 {
		return raw
	}
	changed := false
	for i := range entries {
		nv := config.ExpandVars(entries[i].Value, vars)
		if nv != entries[i].Value {
			entries[i].Value = nv
			changed = true
		}
	}
	if !changed {
		return raw
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return raw
	}
	return string(b)
}
