package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// Kinds are the widgets Silo knows how to draw, with the size a new one of
// that kind takes when the Bot names none.
var Kinds = map[string]string{
	"note":      "l",
	"stat":      "s",
	"chart":     "m",
	"list":      "m",
	"table":     "m",
	"timeline":  "m",
	"status":    "m",
	"checklist": "m",
	"progress":  "s",
	"app":       "m",
}

const (
	widgetMax  = 40
	codeMax    = 20000
	dataMax    = 32000
	stateMax   = 16000
	actionsMax = 8
	everyMin   = 60
	everyMax   = 7 * 24 * 3600
)

var (
	keyRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,59}$`)
	actionRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
)

// action is one button a widget offers. run executes the widget's stored code
// on the box; hand opens a chat that hands the widget to the Bot. row puts it
// on each item of a list or table instead of the widget's foot.
type action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Code    string `json:"code,omitempty"`
	Row     bool   `json:"row,omitempty"`
	Confirm bool   `json:"confirm,omitempty"`
}

// spec is the widget's settings as the Bot wrote them; actions are read out
// of it, everything else is the kind's to interpret.
func parseSpec(raw string) (map[string]any, []action) {
	spec := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &spec)
	var acts []action
	if b, err := json.Marshal(spec["actions"]); err == nil {
		_ = json.Unmarshal(b, &acts)
	}
	return spec, acts
}

func checkActions(acts []action) error {
	if len(acts) > actionsMax {
		return fmt.Errorf("a widget has at most %d actions", actionsMax)
	}
	seen := map[string]bool{}
	for _, a := range acts {
		if !actionRe.MatchString(a.ID) {
			return fmt.Errorf("action id %q: use lowercase letters, digits, - and _", a.ID)
		}
		if seen[a.ID] {
			return fmt.Errorf("action id %q is used twice", a.ID)
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Label) == "" {
			return fmt.Errorf("action %q needs a label", a.ID)
		}
		switch a.Kind {
		case "run":
			if strings.TrimSpace(a.Code) == "" {
				return fmt.Errorf("action %q is kind run and needs code", a.ID)
			}
			if utf8.RuneCountInString(a.Code) > codeMax {
				return fmt.Errorf("action %q code is over %d characters", a.ID, codeMax)
			}
		case "hand":
		default:
			return fmt.Errorf("action %q: kind is run or hand", a.ID)
		}
	}
	return nil
}

// codeHash is what an approval of a widget's code covers: its probe and the
// code of every run action, so changing any of them is a new approval.
func codeHash(probe string, acts []action) string {
	h := sha256.New()
	h.Write([]byte(probe))
	for _, a := range acts {
		if a.Kind == "run" {
			h.Write([]byte{0})
			h.Write([]byte(a.ID))
			h.Write([]byte{0})
			h.Write([]byte(a.Code))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hasCode(w *db.DashboardWidget) bool {
	if strings.TrimSpace(w.Probe) != "" {
		return true
	}
	_, acts := parseSpec(w.SpecJSON)
	for _, a := range acts {
		if a.Kind == "run" {
			return true
		}
	}
	return false
}

// parseEvery reads an interval: seconds, or "90s", "15m", "2h", "1d". 0 means
// the probe runs only when asked.
func parseEvery(v any) (int, error) {
	var n int
	switch x := v.(type) {
	case nil:
		return 0, nil
	case float64:
		n = int(x)
	case string:
		x = strings.TrimSpace(strings.ToLower(x))
		if x == "" || x == "0" {
			return 0, nil
		}
		unit := 1
		switch x[len(x)-1] {
		case 's':
			x = x[:len(x)-1]
		case 'm':
			unit, x = 60, x[:len(x)-1]
		case 'h':
			unit, x = 3600, x[:len(x)-1]
		case 'd':
			unit, x = 86400, x[:len(x)-1]
		}
		k, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("every %q: use seconds or 15m, 2h, 1d", v)
		}
		n = k * unit
	default:
		return 0, fmt.Errorf("every: use seconds or 15m, 2h, 1d")
	}
	if n <= 0 {
		return 0, nil
	}
	if n < everyMin {
		return 0, fmt.Errorf("every is at least %d seconds", everyMin)
	}
	return min(n, everyMax), nil
}

// object reads a JSON object argument, given as an object or as JSON text.
func object(v any, name string, max int) (string, error) {
	if s, ok := v.(string); ok {
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return "", fmt.Errorf("%s must be an object", name)
		}
		v = m
	}
	m, ok := v.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%s must be an object", name)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	if len(b) > max {
		return "", fmt.Errorf("%s is %d bytes; cap is %d. Keep what the widget shows, put the rest in a file", name, len(b), max)
	}
	return string(b), nil
}

func (s *Service) widgets(botID string) []db.DashboardWidget {
	var out []db.DashboardWidget
	s.db.Where("bot_id = ?", botID).Order("position asc, created_at asc").Find(&out)
	return out
}

func (s *Service) widgetByKey(botID, key string) *db.DashboardWidget {
	var w db.DashboardWidget
	if s.db.Where("bot_id = ? AND key = ?", botID, key).Limit(1).Find(&w).RowsAffected == 0 {
		return nil
	}
	return &w
}

// setData stores new data, noting a change only when it differs.
func (s *Service) setData(w *db.DashboardWidget, data string) {
	now := time.Now()
	if canonical(data) != canonical(w.DataJSON) {
		w.ChangedAt = &now
	}
	w.DataJSON = data
	w.DataAt = &now
	w.Error = ""
}

func canonical(js string) string {
	var v any
	if json.Unmarshal([]byte(js), &v) != nil {
		return js
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// putWidget creates or updates a widget by key. Code it carries (a probe, a
// run action) goes through the python.run rule once, here, and only that code
// runs later; a new probe runs at once so the Bot sees what it returns.
func (s *Service) putWidget(ctx context.Context, bot *db.Bot, runID string, args map[string]any) (string, error) {
	key := strings.ToLower(strings.TrimSpace(str(args, "key")))
	if !keyRe.MatchString(key) {
		return "", errors.New("key required: lowercase letters, digits and - _ . : (up to 60)")
	}
	w := s.widgetByKey(bot.ID, key)
	creating := w == nil
	now := time.Now()
	if creating {
		var n int64
		s.db.Model(&db.DashboardWidget{}).Where("bot_id = ?", bot.ID).Count(&n)
		if n >= widgetMax {
			return "", fmt.Errorf("the Dashboard holds %d widgets: remove one first", widgetMax)
		}
		var last struct{ P int }
		s.db.Model(&db.DashboardWidget{}).Select("COALESCE(MAX(position), -1) AS p").Where("bot_id = ?", bot.ID).Scan(&last)
		w = &db.DashboardWidget{ID: ids.New(), BotID: bot.ID, Key: key, SpecJSON: "{}", DataJSON: "{}", StateJSON: "{}",
			Rows: 1, Position: last.P + 1, ChatID: s.host.ChatOfRun(runID), RunID: runID, CreatedAt: now}
	}
	if v := strings.TrimSpace(str(args, "kind")); v != "" {
		if _, ok := Kinds[v]; !ok {
			return "", fmt.Errorf("kind %q: one of note, stat, chart, list, table, timeline, status, checklist, progress, app", v)
		}
		w.Kind = v
	}
	if w.Kind == "" {
		return "", errors.New("kind required for a new widget")
	}
	if v := strings.TrimSpace(str(args, "title")); v != "" {
		w.Title = textx.ClipRunes(strings.ReplaceAll(v, "\n", " "), titleMax)
	}
	if w.Title == "" {
		return "", errors.New("title required for a new widget")
	}
	if v, ok := args["source"].(string); ok {
		w.Source = textx.ClipRunes(strings.TrimSpace(v), 60)
	}
	if !w.OwnerPlaced {
		if v := strings.TrimSpace(str(args, "size")); v != "" {
			if v != "s" && v != "m" && v != "l" {
				return "", errors.New("size is s, m or l")
			}
			w.Size = v
		} else if creating {
			w.Size = Kinds[w.Kind]
		}
		if v, ok := args["rows"].(float64); ok {
			w.Rows = min(max(int(v), 1), 2)
		}
	}
	if v, ok := args["spec"]; ok && v != nil {
		spec, err := object(v, "spec", dataMax)
		if err != nil {
			return "", err
		}
		w.SpecJSON = spec
	}
	_, acts := parseSpec(w.SpecJSON)
	if err := checkActions(acts); err != nil {
		return "", err
	}
	probeChanged := false
	if v, ok := args["probe"].(string); ok {
		if utf8.RuneCountInString(v) > codeMax {
			return "", fmt.Errorf("probe is over %d characters", codeMax)
		}
		probeChanged = v != w.Probe
		w.Probe = v
	}
	if v, ok := args["every"]; ok {
		n, err := parseEvery(v)
		if err != nil {
			return "", err
		}
		w.Every = n
	}
	if v, ok := args["artifact"].(string); ok && strings.TrimSpace(v) != "" {
		id, err := s.appArtifact(bot.ID, strings.TrimSpace(v))
		if err != nil {
			return "", err
		}
		w.ArtifactID = id
	}
	if w.Kind == "app" && w.ArtifactID == "" {
		return "", errors.New("an app widget needs artifact: the id or path of an app artifact")
	}
	if v, ok := args["data"]; ok && v != nil {
		data, err := object(v, "data", dataMax)
		if err != nil {
			return "", err
		}
		s.setData(w, data)
	}

	// The python.run rule decides once for this exact code.
	if hasCode(w) {
		if h := codeHash(w.Probe, acts); h != w.ApprovedHash {
			argsJSON, _ := json.Marshal(map[string]any{"code": bundle(w.Probe, acts), "widget": w.Key})
			if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Python, "run", string(argsJSON), ""); err != nil {
				return "", fmt.Errorf("the widget's code was not allowed: %w", err)
			}
			w.ApprovedHash = h
		}
	} else {
		w.ApprovedHash = ""
	}
	w.NextAt = nil
	if w.Every > 0 && strings.TrimSpace(w.Probe) != "" {
		next := now.Add(time.Duration(w.Every) * time.Second)
		w.NextAt = &next
	}
	w.UpdatedAt = now
	if creating {
		if err := s.db.Create(w).Error; err != nil {
			return "", err
		}
	} else if err := s.db.Save(w).Error; err != nil {
		return "", err
	}

	out := map[string]any{"key": w.Key, "created": creating, "updated": !creating}
	if strings.TrimSpace(w.Probe) != "" && (probeChanged || w.DataAt == nil) {
		if err := s.refresh(ctx, w); err != nil {
			out["probe_error"] = err.Error()
		} else {
			out["data"] = json.RawMessage(w.DataJSON)
		}
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// bundle is the code an approval shows: the probe, then each run action.
func bundle(probe string, acts []action) string {
	var b strings.Builder
	if strings.TrimSpace(probe) != "" {
		b.WriteString("# probe\n" + strings.TrimSpace(probe) + "\n")
	}
	for _, a := range acts {
		if a.Kind == "run" {
			fmt.Fprintf(&b, "\n# action %s (%s)\n%s\n", a.ID, a.Label, strings.TrimSpace(a.Code))
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) appArtifact(botID, ref string) (string, error) {
	var art db.Artifact
	s.db.Where("bot_id = ? AND (id = ? OR path = ? OR path = ?)", botID, ref, ref, strings.TrimPrefix(ref, "/workspace/")).
		Order("updated_at desc").Limit(1).Find(&art)
	if art.ID == "" {
		return "", fmt.Errorf("no artifact %q: emit the app with artifact first", ref)
	}
	if art.Type != "app" {
		return "", fmt.Errorf("artifact %q is a %s, not an app", ref, art.Type)
	}
	return art.ID, nil
}

// setWidget pushes data from code: no model, no approval.
func (s *Service) setWidget(botID string, args map[string]any) (string, error) {
	key := strings.ToLower(strings.TrimSpace(str(args, "key")))
	w := s.widgetByKey(botID, key)
	if w == nil {
		return "", fmt.Errorf("no widget %q: create it with put_widget", key)
	}
	data, err := object(args["data"], "data", dataMax)
	if err != nil {
		return "", err
	}
	if merge, _ := args["merge"].(bool); merge {
		cur := map[string]any{}
		_ = json.Unmarshal([]byte(w.DataJSON), &cur)
		add := map[string]any{}
		_ = json.Unmarshal([]byte(data), &add)
		for k, v := range add {
			cur[k] = v
		}
		b, _ := json.Marshal(cur)
		if len(b) > dataMax {
			return "", fmt.Errorf("data is over %d bytes", dataMax)
		}
		data = string(b)
	}
	before := w.ChangedAt
	s.setData(w, data)
	if err := s.db.Save(w).Error; err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"key": w.Key, "changed": w.ChangedAt != before})
	return string(b), nil
}

func (s *Service) removeWidget(botID, key string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	w := s.widgetByKey(botID, key)
	if w == nil {
		return "", fmt.Errorf("no widget %q", key)
	}
	if err := s.db.Delete(w).Error; err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"removed": w.Key})
	return string(b), nil
}

// listForBot is the Dashboard as the Bot reads it: widgets with their code,
// state and last receipt, open asks, and recent dismissals to respect.
func (s *Service) listForBot(botID string) (string, error) {
	type widgetView struct {
		Key         string          `json:"key"`
		Kind        string          `json:"kind"`
		Title       string          `json:"title"`
		Source      string          `json:"source,omitempty"`
		Size        string          `json:"size"`
		Rows        int             `json:"rows"`
		OwnerPlaced bool            `json:"owner_placed,omitempty"`
		Spec        json.RawMessage `json:"spec"`
		Data        json.RawMessage `json:"data"`
		State       json.RawMessage `json:"state"`
		Probe       string          `json:"probe,omitempty"`
		Every       int             `json:"every,omitempty"`
		DataAt      string          `json:"data_at,omitempty"`
		Error       string          `json:"error,omitempty"`
		Receipt     json.RawMessage `json:"last_receipt,omitempty"`
	}
	type askView struct {
		ID          string          `json:"id"`
		Key         string          `json:"key,omitempty"`
		Title       string          `json:"title"`
		Summary     string          `json:"summary,omitempty"`
		Priority    int             `json:"priority"`
		Blocks      json.RawMessage `json:"blocks"`
		SubmitLabel string          `json:"submit_label,omitempty"`
		UpdatedAt   string          `json:"updated_at"`
	}
	type dismissalView struct {
		Key         string `json:"key,omitempty"`
		Title       string `json:"title"`
		Suppressed  bool   `json:"suppressed"`
		DismissedAt string `json:"dismissed_at,omitempty"`
	}
	out := struct {
		Widgets          []widgetView    `json:"widgets"`
		Asks             []askView       `json:"asks"`
		RecentDismissals []dismissalView `json:"recent_dismissals"`
	}{Widgets: []widgetView{}, Asks: []askView{}, RecentDismissals: []dismissalView{}}
	raw := func(s string) json.RawMessage {
		if !json.Valid([]byte(s)) {
			return json.RawMessage("{}")
		}
		return json.RawMessage(s)
	}
	for _, w := range s.widgets(botID) {
		v := widgetView{Key: w.Key, Kind: w.Kind, Title: w.Title, Source: w.Source, Size: w.Size, Rows: w.Rows, OwnerPlaced: w.OwnerPlaced,
			Spec: raw(w.SpecJSON), Data: raw(w.DataJSON), State: raw(w.StateJSON), Probe: w.Probe, Every: w.Every, Error: w.Error,
			DataAt: textx.RFC3339(w.DataAt)}
		if w.ReceiptJSON != "" {
			v.Receipt = raw(w.ReceiptJSON)
		}
		out.Widgets = append(out.Widgets, v)
	}
	var open []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'open'", botID).Order("priority asc, updated_at desc").Find(&open)
	for _, c := range open {
		blocks := c.BlocksJSON
		if !json.Valid([]byte(blocks)) {
			blocks = "[]"
		}
		out.Asks = append(out.Asks, askView{ID: c.ID, Key: c.Key, Title: c.Title, Summary: c.Summary, Priority: c.Priority,
			Blocks: json.RawMessage(blocks), SubmitLabel: c.SubmitLabel, UpdatedAt: c.UpdatedAt.Format(time.RFC3339)})
	}
	var dismissed []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'dismissed'", botID).Order("dismissed_at desc").Limit(dismissalsMax).Find(&dismissed)
	for _, d := range dismissed {
		out.RecentDismissals = append(out.RecentDismissals, dismissalView{Key: d.Key, Title: d.Title, Suppressed: d.Suppressed, DismissedAt: textx.RFC3339(d.DismissedAt)})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// publicSpec is the spec without action code: the page needs the buttons,
// not what they run.
func publicSpec(raw string) string {
	spec, acts := parseSpec(raw)
	if len(acts) == 0 {
		return raw
	}
	for i := range acts {
		acts[i].Code = ""
	}
	spec["actions"] = acts
	b, _ := json.Marshal(spec)
	return string(b)
}

func (s *Service) protoWidget(w *db.DashboardWidget) *v1.DashboardWidget {
	fresh := w.ChangedAt != nil && (w.SeenAt == nil || w.ChangedAt.After(*w.SeenAt))
	return &v1.DashboardWidget{
		Id: w.ID, Key: w.Key, Kind: w.Kind, Title: w.Title, Source: w.Source,
		SpecJson: publicSpec(w.SpecJSON), DataJson: w.DataJSON, StateJson: w.StateJSON,
		Size: w.Size, Rows: int32(w.Rows), Position: int32(w.Position),
		HasProbe: strings.TrimSpace(w.Probe) != "", Every: int32(w.Every), DataAt: textx.RFC3339(w.DataAt),
		Error: w.Error, UpdatedAt: w.UpdatedAt.Format(time.RFC3339), Fresh: fresh, ReceiptJson: w.ReceiptJSON,
		Busy: s.busy.has(w.ID), ArtifactId: w.ArtifactID,
	}
}
