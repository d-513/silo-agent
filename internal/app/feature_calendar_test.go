package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/builtin/calendar/caldavtest"
)

// TestBuiltinCalendarConnector attaches the CalDAV Calendar from the library,
// reads events from the worker, and gates event creation on a slip.
func TestBuiltinCalendarConnector(t *testing.T) {
	srv := caldavtest.Start(t)
	srv.Put(t, caldavtest.Work, "a.ics", caldavtest.Event("a", "SUMMARY:Review",
		"DTSTART:20261005T080000Z", "DTEND:20261005T090000Z"))
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)+"debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	bot := h.CreateBot("Scheduler")

	res, err := h.Client.ListConnectors(h.Ctx(), connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var lib *v1.Connector
	for _, c := range res.Msg.GetConnectors() {
		if c.GetBuiltin() == "calendar" {
			lib = c
		}
	}
	if lib == nil || lib.GetTransport() != "builtin" || lib.GetConfig()["provider"] != "icloud" {
		t.Fatalf("calendar preset not seeded: %+v", lib)
	}

	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: lib.GetId(), Config: srv.Config(),
	})); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h, bot.GetId(), "authorized")

	// The code-owned usage notes reach the model with the real slug, so it
	// does not guess `calendar__list_events` or a ten-year window.
	req := drivePromptFor(t, h, bot.GetId(), "Calendar_Prompt_1")
	if !strings.Contains(req, "tools.calendar.list_events(start=") || !strings.Contains(req, "1 year") {
		t.Fatal("calendar usage notes missing from the system prompt")
	}

	wc := h.WorkerClient(bot.GetId())
	out, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "calendar", Action: "list_events", ArgsJson: `{"start":"2026-10-05","end":"2026-10-06"}`,
	}))
	if err != nil || out.Msg.GetError() != "" {
		t.Fatalf("list_events: %v %+v", err, out.Msg)
	}
	var got struct {
		Events []struct {
			Summary string `json:"summary"`
			Start   string `json:"start"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(out.Msg.GetResultJson()), &got); err != nil || len(got.Events) != 1 ||
		got.Events[0].Summary != "Review" || got.Events[0].Start != "2026-10-05T10:00:00+02:00" {
		t.Fatalf("list_events result %s", out.Msg.GetResultJson())
	}

	done := make(chan *v1.ToolRes, 1)
	go func() {
		r, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
			Connector: "calendar", Action: "create_event",
			ArgsJson: `{"summary":"Lunch","start":"2026-10-06T12:00","calendar":"Work"}`,
		}))
		if err != nil {
			done <- &v1.ToolRes{Error: err.Error()}
			return
		}
		done <- r.Msg
	}()
	ap := h.WaitApproval(bot.GetId())
	if ap.GetConnector() != "calendar" || ap.GetAction() != "create_event" {
		t.Fatalf("approval %+v", ap)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow_once"})); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.GetError() != "" || !strings.Contains(r.GetResultJson(), caldavtest.Work) {
		t.Fatalf("create_event: %+v", r)
	}
	if len(srv.Paths()) != 2 {
		t.Fatalf("server objects %v", srv.Paths())
	}
}
