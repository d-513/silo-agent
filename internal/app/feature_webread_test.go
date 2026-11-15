package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/llm/dummy"
	"silo.agent/internal/reader"
)

const readerProse = "The lighthouse at Rozewie has stood on the cliff since 1822 and its lamp was once fed with rapeseed oil carried up the stairs every evening. "

func readerArticle(extra string) string {
	return `<!doctype html><html><head><title>Rozewie Lighthouse</title></head><body><nav><a href="/">Front page of the gazette</a></nav>
<article><h1>Rozewie Lighthouse</h1><p>` + strings.Repeat(readerProse, 5) + extra + `</p></article></body></html>`
}

// webRead calls web.read as the Bot's Python does.
func webRead(t *testing.T, h *apptest.H, botID, runID, args string) *v1.ToolRes {
	t.Helper()
	res, err := h.WorkerClient(botID).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "web", Action: "read", ArgsJson: args, RunId: runID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func readerYAML(t *testing.T, extra string) string {
	return apptest.DefaultYAML(t.TempDir()) + extra
}

// TestWebReadFromPython is silo_runtime.web_read and web_read_many: a page
// comes back as its main content, a batch is answered in order, and the
// operator's and the owner's block lists hold for the control plane's own
// reads as they do for the Bot's machine.
func TestWebReadFromPython(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, readerArticle(" This is "+r.URL.Path+"."))
	}))
	defer site.Close()
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(readerYAML(t, "reader:\n  block_private: false\negress:\n  block:\n    - blocked.example\n")))
	bot := h.CreateBot("Reader")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "hello")
	h.WaitRun(run)
	h.LiveRun(run)

	res := webRead(t, h, bot.GetId(), run, `{"url":"`+site.URL+`/rozewie","max_chars":400}`)
	var page reader.Page
	if err := json.Unmarshal([]byte(res.GetResultJson()), &page); err != nil || res.GetError() != "" {
		t.Fatalf("read: %+v", res)
	}
	if page.Title != "Rozewie Lighthouse" || page.Via != "static" || !strings.Contains(page.Text, "rapeseed oil") || strings.Contains(page.Text, "Front page of the gazette") {
		t.Fatalf("page %+v", page)
	}
	if page.NextOffset == 0 || page.NextOffset > 400 || page.TotalChars < 600 {
		t.Fatalf("a long page was not cut: %+v", page)
	}
	// Reading on from next_offset.
	res = webRead(t, h, bot.GetId(), run, fmt.Sprintf(`{"url":"%s/rozewie","offset":%d,"max_chars":5000}`, site.URL, page.NextOffset))
	var rest reader.Page
	if err := json.Unmarshal([]byte(res.GetResultJson()), &rest); err != nil || !strings.HasSuffix(rest.Text, "This is /rozewie.") || rest.NextOffset != 0 {
		t.Fatalf("rest: %+v", res)
	}

	res = webRead(t, h, bot.GetId(), run, `{"urls":["`+site.URL+`/a"," ","https://blocked.example/x","`+site.URL+`/c"],"max_chars":5000}`)
	var many struct {
		Reads []reader.Page `json:"reads"`
	}
	if err := json.Unmarshal([]byte(res.GetResultJson()), &many); err != nil || res.GetError() != "" || len(many.Reads) != 3 {
		t.Fatalf("batch: %+v", res)
	}
	if !strings.HasSuffix(many.Reads[0].Text, "This is /a.") || !strings.HasSuffix(many.Reads[2].Text, "This is /c.") ||
		many.Reads[1].Problem != "error" || !strings.Contains(many.Reads[1].Note, "block list") {
		t.Fatalf("batch %+v", many.Reads)
	}

	// The owner's own block list is honoured too.
	if _, err := h.Client.AddHostPattern(h.Ctx(), connect.NewRequest(&v1.AddHostPatternRequest{BotId: bot.GetId(), Kind: "block", Pattern: "127.0.0.1"})); err != nil {
		t.Fatal(err)
	}
	if res := webRead(t, h, bot.GetId(), run, `{"url":"`+site.URL+`/rozewie"}`); !strings.Contains(res.GetError(), "block list") {
		t.Fatalf("a host the owner blocked was read: %+v", res)
	}

	for args, want := range map[string]string{
		`{}`: "url required",
		`{"url":"a.example","urls":["b.example"]}`: "give url or urls, not both",
		`{"url":"https://blocked.example/x"}`:      "blocked.example is on this Silo's block list",
		`{"url":"x.example","render":"maybe"}`:     "render is true, false or left out",
		`{"url":"ftp://x.example/file"}`:           "only http and https pages can be read, not ftp:",
	} {
		if res := webRead(t, h, bot.GetId(), run, args); !strings.Contains(res.GetError(), want) {
			t.Fatalf("%s: error %q want %q", args, res.GetError(), want)
		}
	}
}

// By default the page reader does not reach inside: a Bot told to read
// http://127.0.0.1/ by a web page gets nothing of the operator's network.
func TestWebReadRefusesInternalAddressesByDefault(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "the operator's own service")
	}))
	defer inside.Close()
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Reader")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "hello")
	h.WaitRun(run)
	h.LiveRun(run)
	res := webRead(t, h, bot.GetId(), run, `{"url":"`+inside.URL+`"}`)
	if !strings.Contains(res.GetError(), "internal address") || strings.Contains(res.GetResultJson(), "operator's own") {
		t.Fatalf("an internal address was read: %+v", res)
	}
}

// TestWebReadRendersInTheBox is the two jobs the control plane hands down: a
// page that is an empty shell is rendered by the headless browser in the Bot's
// own machine (the real worker, a script standing in for Lightpanda), and a
// document is opened by the machine's tools.
func TestWebReadRendersInTheBox(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notes":
			w.Header().Set("Content-Type", "application/rtf")
			fmt.Fprint(w, "not really rtf")
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><title>App</title><script src="/bundle.js"></script></head><body><div id="root"></div></body></html>`)
		}
	}))
	defer site.Close()

	// The browser's stand-in answers what Lightpanda prints for --json.
	bin := t.TempDir()
	out, _ := json.Marshal(map[string]any{"url": site.URL + "/app", "http_status": 200, "content": readerArticle(" Drawn by scripts."), "error": nil})
	if err := os.WriteFile(filepath.Join(bin, "out.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s ' \"$@\" > " + filepath.Join(bin, "args") + "\ncat " + filepath.Join(bin, "out.json") + "\n"
	if err := os.WriteFile(filepath.Join(bin, "lightpanda"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(readerYAML(t, "reader:\n  block_private: false\n")))
	bot := h.CreateBot("Reader")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "hello")
	h.WaitRun(run)
	h.LiveRun(run)

	// With no machine the answer says what the page needs.
	res := webRead(t, h, bot.GetId(), run, `{"url":"`+site.URL+`/app"}`)
	var page reader.Page
	if err := json.Unmarshal([]byte(res.GetResultJson()), &page); err != nil || page.Problem != reader.ProblemNeedsRender || !strings.Contains(page.Note, "machine is not running") {
		t.Fatalf("no machine: %+v", res)
	}

	h.StartWorker(bot.GetId())
	res = webRead(t, h, bot.GetId(), run, `{"url":"`+site.URL+`/app","language":"pl","max_chars":5000}`)
	page = reader.Page{}
	if err := json.Unmarshal([]byte(res.GetResultJson()), &page); err != nil || res.GetError() != "" {
		t.Fatalf("render: %+v", res)
	}
	if page.Via != "rendered" || page.Problem != "" || !strings.HasSuffix(page.Text, "Drawn by scripts.") {
		t.Fatalf("rendered page %+v", page)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "args"))
	if !strings.HasPrefix(string(args), "fetch "+site.URL+"/app --json --dump html") || !strings.Contains(string(args), "--locale pl") || strings.Contains(string(args), "--block-private-networks") {
		t.Fatalf("lightpanda was run as: %s", args)
	}

	// A document is opened on the machine; one its tools cannot read is
	// said to be so, not returned as nothing.
	res = webRead(t, h, bot.GetId(), run, `{"url":"`+site.URL+`/notes"}`)
	page = reader.Page{}
	if err := json.Unmarshal([]byte(res.GetResultJson()), &page); err != nil || page.Kind != "document" {
		t.Fatalf("document: %+v", res)
	}
	if page.Problem == "" && !strings.Contains(page.Text, "not really rtf") {
		t.Fatalf("document read as %+v", page)
	}
	if page.Problem != "" && (page.Problem != reader.ProblemUnsupported || !strings.Contains(page.Note, "could not be opened")) {
		t.Fatalf("document refused as %+v", page)
	}
}
