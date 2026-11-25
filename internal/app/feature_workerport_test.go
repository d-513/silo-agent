package app_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"silo.agent/internal/apptest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
	"silo.agent/internal/rpcx"
)

// The workers' port (worker_addr) is where the network filter's gateway
// forwards a box. A real worker does all its work through it, and nothing of
// the console answers there: the forward is a box's one way to the control
// plane, and it must not also be a way to the sign-in, the API people use,
// an artifact or Caddy's certificate question.
func TestWorkersPortServesWorkersOnly(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_Port",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo through-the-workers-port"}`}}},
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	srv := httptest.NewUnstartedServer(h.App.WorkerHandler())
	rpcx.EnableH2C(srv.Config)
	srv.Start()
	t.Cleanup(srv.Close)

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for _, path := range []string{
		"/",
		"/silo.v1.UI/AuthOptions",
		"/silo.v1.UI/SignIn",
		"/silo.v1.EgressHost/Session",
		"/silo.v1.DriveHost/Session",
		"/caddy/ask?domain=x",
		"/artifacts/download",
		"/vnc",
		"/console",
		"/oauth/callback",
	} {
		if code, _ := get(path); code != http.StatusNotFound {
			t.Errorf("%s answers %d on the workers' port", path, code)
		}
	}
	// The same call is served where people are.
	if resp, err := http.Post(h.URL+"/silo.v1.UI/AuthOptions", "application/json", strings.NewReader("{}")); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("AuthOptions on the main port: %v %v", resp, err)
	}
	if code, body := get("/healthz"); code != http.StatusOK || body != "ok" {
		t.Fatalf("healthz: %d %q", code, body)
	}

	// A worker that knows only this port attaches and runs a command.
	bot := h.CreateBot("Porter")
	main := h.URL
	h.URL = srv.URL
	h.StartWorker(bot.GetId())
	h.URL = main
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_Port_Input")
	var result string
	for _, ev := range h.WaitRun(run) {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	if !strings.Contains(result, "through-the-workers-port") {
		t.Fatalf("the worker's command did not come back: %q", result)
	}
}
