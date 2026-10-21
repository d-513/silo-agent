package dashboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

const (
	// Tick is how often the sweep looks for probes that are due.
	Tick = 20 * time.Second
	// probeSlots is how many probes run at once across every Bot.
	probeSlots = 4
	// perTick bounds how many probes one sweep starts.
	perTick       = 8
	probeTimeout  = 60 * time.Second
	actionTimeout = 2 * time.Minute
	// errorRetry is the shortest wait after a failed probe.
	errorRetry = 5 * time.Minute
	// offlineRetry is how soon a probe due on a stopped Bot is looked at again.
	offlineRetry = 2 * time.Minute
	errorMax     = 600
)

// busySet is the widgets whose code runs now: one run per widget at a time.
type busySet struct{ m sync.Map }

func (b *busySet) take(id string) bool {
	_, had := b.m.LoadOrStore(id, struct{}{})
	return !had
}
func (b *busySet) done(id string) { b.m.Delete(id) }
func (b *busySet) has(id string) bool {
	_, ok := b.m.Load(id)
	return ok
}

var errNotRunning = errors.New("the Bot's machine is not running")

// harness wraps widget code so it runs with ARGS, STATE and DATA bound and
// reports `result` on a marked line the CP finds whatever else was printed.
// Everything goes in base64 so no value can break out of its string.
func harness(code, marker string, args, state, data string) string {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	return fmt.Sprintf(`import base64 as _b, json as _j
def _d(s): return _b.b64decode(s).decode()
_env = {"__name__": "__widget__", "ARGS": _j.loads(_d(%q)), "STATE": _j.loads(_d(%q)), "DATA": _j.loads(_d(%q))}
exec(compile(_d(%q), "<widget>", "exec"), _env)
print("\n" + %q + _j.dumps(_env.get("result"), default=str, ensure_ascii=False))
`, enc(args), enc(state), enc(data), enc(code), marker)
}

// parseResult finds the result line; with none (`result` unset), the last
// printed line that is a JSON object stands in.
func parseResult(out, marker string) (any, error) {
	i := strings.LastIndex(out, marker)
	if i < 0 {
		tail := strings.TrimSpace(out)
		if r := []rune(tail); len(r) > errorMax {
			tail = "…" + string(r[len(r)-errorMax:])
		}
		if tail == "" {
			tail = "it printed nothing"
		}
		return nil, errors.New(tail)
	}
	line := out[i+len(marker):]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	var r any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &r); err != nil {
		return nil, fmt.Errorf("result is not JSON: %v", err)
	}
	if r != nil {
		return r, nil
	}
	lines := strings.Split(strings.TrimSpace(out[:i]), "\n")
	for k := len(lines) - 1; k >= 0; k-- {
		var m map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(lines[k])), &m) == nil {
			return m, nil
		}
	}
	return nil, nil
}

// runCode runs one piece of a widget's code on the box.
func (s *Service) runCode(ctx context.Context, w *db.DashboardWidget, code, args string, timeout time.Duration) (any, error) {
	if !s.host.Online(w.BotID) {
		return nil, errNotRunning
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	marker := "SILO_WIDGET_RESULT_" + ids.New() + ":"
	out, err := s.host.RunPython(ctx, w.BotID, harness(code, marker, args, orEmpty(w.StateJSON), orEmpty(w.DataJSON)))
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("it ran over %s", timeout)
		}
		return nil, err
	}
	return parseResult(out, marker)
}

func orEmpty(js string) string {
	if strings.TrimSpace(js) == "" || !json.Valid([]byte(js)) {
		return "{}"
	}
	return js
}

// allowed says whether the widget's code may run now: it is the code the rule
// let through, and Python is not set to Deny since.
func (s *Service) allowed(w *db.DashboardWidget) error {
	_, acts := parseSpec(w.SpecJSON)
	if w.ApprovedHash == "" || w.ApprovedHash != codeHash(w.Probe, acts) {
		return errors.New("the widget's code changed without approval: the Bot puts it again")
	}
	if security.Rule(s.host.RuleDecision(w.BotID, security.Python, "run")) == security.Deny {
		return errors.New("Python is set to Deny in Rules")
	}
	return nil
}

// refresh runs the widget's probe and stores what it returned, or why not.
func (s *Service) refresh(ctx context.Context, w *db.DashboardWidget) error {
	if strings.TrimSpace(w.Probe) == "" {
		return errors.New("this widget has no probe")
	}
	if !s.busy.take(w.ID) {
		return errors.New("it is refreshing already")
	}
	defer s.busy.done(w.ID)
	err := s.allowed(w)
	var r any
	if err == nil {
		r, err = s.runCode(ctx, w, w.Probe, "{}", probeTimeout)
	}
	now := time.Now()
	if err == nil {
		var data string
		data, err = object(r, "result", dataMax)
		if err != nil && r == nil {
			err = errors.New("the probe set no result and printed no JSON object")
		}
		if err == nil {
			s.setData(w, data)
		}
	}
	// A stopped machine says nothing about the probe: its last data and error
	// stand.
	upd := map[string]any{}
	if err == nil {
		upd = dataColumns(w)
	} else if !errors.Is(err, errNotRunning) {
		w.Error = textx.ClipRunes(err.Error(), errorMax)
		upd["error"] = w.Error
	}
	if w.Every > 0 {
		wait := time.Duration(w.Every) * time.Second
		if err != nil && wait < errorRetry {
			wait = errorRetry
		}
		next := now.Add(wait)
		upd["next_at"] = next
		w.NextAt = &next
	}
	if len(upd) == 0 {
		return err
	}
	s.db.Model(&db.DashboardWidget{}).Where("id = ?", w.ID).UpdateColumns(upd)
	return err
}

// Sweep runs the probes that are due on Bots whose machine is up. It never
// starts a box: a stopped Bot's widgets wait, showing their last data.
func (s *Service) Sweep(now time.Time) {
	var due []db.DashboardWidget
	s.db.Where("probe <> '' AND every > 0 AND next_at IS NOT NULL AND next_at <= ?", now).
		Order("next_at asc").Limit(perTick * 4).Find(&due)
	started := 0
	for i := range due {
		w := due[i]
		if started >= perTick || s.busy.has(w.ID) {
			continue
		}
		if !s.host.Online(w.BotID) {
			// Moved on, so a stopped Bot's widgets cannot fill every sweep's
			// window and starve the Bots that are up.
			s.db.Model(&db.DashboardWidget{}).Where("id = ?", w.ID).UpdateColumn("next_at", now.Add(offlineRetry))
			continue
		}
		// Claim the slot so a slow probe is not started again next tick.
		next := now.Add(time.Duration(max(w.Every, everyMin)) * time.Second)
		if s.db.Model(&db.DashboardWidget{}).Where("id = ? AND next_at = ?", w.ID, w.NextAt).UpdateColumn("next_at", next).RowsAffected != 1 {
			continue
		}
		started++
		go func() {
			s.slots <- struct{}{}
			defer func() { <-s.slots }()
			if err := s.refresh(context.Background(), &w); err != nil && !errors.Is(err, errNotRunning) {
				log.Printf("widget %s/%s: %v", w.BotID, w.Key, err)
			}
		}()
	}
}

// receipt is what an action leaves on its widget.
type receipt struct {
	Text string `json:"text"`
	OK   bool   `json:"ok"`
	At   string `json:"at"`
	By   string `json:"by"`
}

// runAction runs one of the widget's run actions with the owner's args. The
// code may set result to {"receipt": "...", "data": {...}}: data replaces the
// widget's, else its probe runs again so the widget shows the new state.
func (s *Service) runAction(ctx context.Context, w *db.DashboardWidget, a action, args string) error {
	if !s.busy.take(w.ID) {
		return errors.New("this widget is busy: try again in a moment")
	}
	r, err := s.act(ctx, w, a, args)
	s.busy.done(w.ID)
	if err == nil && r == nil && strings.TrimSpace(w.Probe) != "" {
		_ = s.refresh(ctx, w)
	}
	return err
}

// act runs the action's code and leaves its receipt. It answers the data the
// code set, or nil when the widget should be probed again to show the change.
func (s *Service) act(ctx context.Context, w *db.DashboardWidget, a action, args string) (map[string]any, error) {
	err := s.allowed(w)
	var r any
	if err == nil {
		r, err = s.runCode(ctx, w, a.Code, args, actionTimeout)
	}
	if errors.Is(err, errNotRunning) {
		return nil, err // nothing ran: no receipt
	}
	rc := receipt{Text: a.Label, OK: err == nil, At: time.Now().Format(time.RFC3339), By: "you"}
	upd := map[string]any{}
	var set map[string]any
	if err != nil {
		rc.Text = a.Label + " failed: " + textx.ClipRunes(err.Error(), 200)
	} else if m, ok := r.(map[string]any); ok {
		if t, ok := m["receipt"].(string); ok && strings.TrimSpace(t) != "" {
			rc.Text = textx.ClipRunes(strings.TrimSpace(t), 200)
		}
		if d, ok := m["data"]; ok && d != nil {
			if data, derr := object(d, "data", dataMax); derr == nil {
				s.setData(w, data)
				upd = dataColumns(w)
				set = m
			}
		}
	}
	b, _ := json.Marshal(rc)
	w.ReceiptJSON = string(b)
	upd["receipt_json"] = w.ReceiptJSON
	s.db.Model(&db.DashboardWidget{}).Where("id = ?", w.ID).UpdateColumns(upd)
	return set, err
}
