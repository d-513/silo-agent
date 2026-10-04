package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"silo.agent/internal/drives"
)

// DriveSystemKey names a drive template's system value in silo.yaml.
func DriveSystemKey(template, name string) string {
	return "drives.providers." + template + "." + name
}

// driveSystemKey is true for drives.providers.<template>.<system var>.
func driveSystemKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "drives.providers.")
	if !ok {
		return false
	}
	tk, v, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	t, ok := drives.Builtin().Get(tk)
	if !ok {
		return false
	}
	_, ok = t.Var(drives.KindSystem, v)
	return ok
}

// DriveSystem is one template's system values, env winning over yaml.
func (s *Store) DriveSystem(template string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	t, ok := drives.Builtin().Get(template)
	if !ok || s.merged == nil {
		return out
	}
	for _, v := range t.VarsOf(drives.KindSystem) {
		if val := strings.TrimSpace(s.merged.String(DriveSystemKey(template, v.Key))); val != "" {
			out[v.Key] = val
		}
	}
	return out
}

// ConnectorVarMap returns the effective connector variables with the
// environment winning over silo.yaml. Environment keys are lowercased by the
// koanf env provider, so an env override is matched to its YAML key
// case-insensitively to avoid a duplicate entry.
func (s *Store) ConnectorVarMap() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	yamlVars := map[string]string{}
	if s.yaml != nil {
		yamlVars = s.yaml.StringMap("connector_vars")
	}
	envVars := map[string]string{}
	if s.env != nil {
		envVars = s.env.StringMap("connector_vars")
	}
	out := make(map[string]string, len(yamlVars)+len(envVars))
	lowerToKey := map[string]string{}
	for k, v := range yamlVars {
		out[k] = v
		lk := strings.ToLower(k)
		if _, dup := lowerToKey[lk]; !dup {
			lowerToKey[lk] = k
		}
	}
	for k, v := range envVars {
		lk := strings.ToLower(k)
		if yk, ok := lowerToKey[lk]; ok {
			out[yk] = v
			continue
		}
		out[k] = v
		lowerToKey[lk] = k
	}
	return out
}

// ConnectorVars returns the effective connector variables (env over yaml),
// sorted by name so the UI and YAML stay stable.
func (s *Store) ConnectorVars() []NamedVar {
	m := s.ConnectorVarMap()
	out := make([]NamedVar, 0, len(m))
	for k, v := range m {
		out = append(out, NamedVar{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if li == lj {
			return out[i].Name < out[j].Name
		}
		return li < lj
	})
	return out
}

// ConnectorVarSource reports where a variable's effective value comes from.
func (s *Store) ConnectorVarSource(name string) Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.varSourceLocked(name)
}

func (s *Store) varSourceLocked(name string) Source {
	key := "connector_vars." + name
	if s.env != nil && (s.env.Exists(key) || s.env.Exists(strings.ToLower(key))) {
		return SourceEnv
	}
	if s.yaml != nil && s.yaml.Exists(key) {
		return SourceYAML
	}
	return SourceDefault
}

// SetConnectorVars replaces the connector_vars mapping and reloads. Names must
// be simple identifiers; values are stored verbatim (they are not secrets).
func (s *Store) SetConnectorVars(vars []NamedVar) error {
	seen := map[string]bool{}
	for _, v := range vars {
		if !ValidVarName(v.Name) {
			return fmt.Errorf("invalid variable name %q", v.Name)
		}
		if seen[v.Name] {
			return fmt.Errorf("duplicate variable %q", v.Name)
		}
		seen[v.Name] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	node, err := parseOrEmpty(raw)
	if err != nil {
		return err
	}
	if err := setNodeMap(node, "connector_vars", vars); err != nil {
		return err
	}
	out, err := encodeNode(node)
	if err != nil {
		return err
	}
	if err := validateYAML(out); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, out, 0o600); err != nil {
		return err
	}
	return s.reloadLocked()
}
