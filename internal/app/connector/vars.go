package connector

import (
	"encoding/json"
	"strings"

	"silo.agent/internal/config"
	"silo.agent/internal/db"
)

// connectorVars is the effective connector variable map for one Bot: the
// operator's connector variables plus the Bot's own secrets under
// "secrets.<name>". A reference to a Bot secret resolves to the real value
// here, because the connector call is made by the control plane (or, for a
// STDIO sidecar, which the handoff keeps free of surrogates), not by a box the
// network filter can swap a stand-in for. Nil-safe.
func (s *Service) connectorVars(botID string) map[string]string {
	vars := map[string]string{}
	for k, v := range s.operatorVars() {
		vars[k] = v
	}
	if s.db != nil && botID != "" {
		var secs []db.Secret
		s.db.Where("bot_id = ? AND name NOT LIKE ?", botID, "channel.%").Find(&secs)
		for _, sec := range secs {
			if sec.Value == "" {
				continue
			}
			vars["secrets."+sec.Name] = sec.Value
		}
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

// operatorVars are the operator's connector variables alone: a Bot's secrets
// must never be expanded into a prompt, which the model reads.
func (s *Service) operatorVars() map[string]string {
	if s.store == nil {
		return nil
	}
	return s.store.ConnectorVarMap()
}

// Resolve returns a copy of connection c as the Bot botID reaches it, with
// operator connector variables (${NAME}) and that Bot's own secrets
// (${secrets.NAME}) expanded in every technical field: a Workspace's
// connection is one definition, and a secret in it is each Bot's own. With no
// Bot only the operator's variables are expanded. Unknown references are left untouched. The raw row keeps
// the placeholders; only the connection path sees the resolved values, so
// changing a variable or a secret takes effect on the next reconnect without
// rewriting connectors. The Prompt is expanded with operator variables alone,
// so a Bot's secrets never reach the model.
func (s *Service) Resolve(c *db.Connector, botID string) *db.Connector {
	if c == nil {
		return c
	}
	vars := s.connectorVars(botID)
	if len(vars) == 0 {
		return c
	}
	out := *c
	out.HTTPURL = config.ExpandVars(c.HTTPURL, vars)
	out.OAuthClientID = config.ExpandVars(c.OAuthClientID, vars)
	out.OAuthClientSecret = config.ExpandVars(c.OAuthClientSecret, vars)
	out.StdioCommand = config.ExpandVars(c.StdioCommand, vars)
	out.StdioImage = config.ExpandVars(c.StdioImage, vars)
	out.Prompt = config.ExpandVars(c.Prompt, s.operatorVars())
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
