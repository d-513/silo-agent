package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

const contentTestHost = "content.test"

func artifactHarness(t *testing.T) *apptest.H {
	t.Helper()
	dir := t.TempDir()
	return apptest.New(t, apptest.WithDataDir(dir),
		apptest.WithYAML(apptest.DefaultYAML(dir)+"public_url: http://console.test\ncontent:\n  host: "+contentTestHost+"\n"))
}

type cardInfo struct {
	Type       string `json:"type"`
	ArtifactID string `json:"artifact_id"`
	Version    int    `json:"version"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Kind       string `json:"kind"`
	Pinned     bool   `json:"pinned"`
	Network    bool   `json:"network"`
	Entry      string `json:"entry"`
	Preview    *struct {
		Text  string `json:"text"`
		Image string `json:"image"`
	} `json:"preview"`
}

func cards(t *testing.T, h *apptest.H, runID string) []cardInfo {
	t.Helper()
	var out []cardInfo
	for _, ev := range h.Events(runID) {
		if ev.Kind != "artifact" {
			continue
		}
		var c cardInfo
		if err := json.Unmarshal([]byte(ev.Body), &c); err != nil {
			t.Fatalf("artifact json %q: %v", ev.Body, err)
		}
		out = append(out, c)
	}
	return out
}

func writeCall(path, content string) llm.ToolCall {
	return llm.ToolCall{Name: "write", Arguments: `{"path":"` + path + `","content":` + mustJSON(content) + `}`}
}

func artifactCall(args string) llm.ToolCall {
	return llm.ToolCall{Name: "artifact", Arguments: args}
}

// Each emit of a path is a version of one card, and every version stays as it
// was: the Bot rewriting or deleting the file changes nothing a card shows.
func TestArtifactVersionsArePinned(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artver",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			writeCall("report.md", "# Report\n\nfirst draft\n"),
			artifactCall(`{"path":"report.md"}`),
			writeCall("report.md", "# Report\n\nsecond draft\n"),
			artifactCall(`{"path":"report.md","title":"The report"}`),
		}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Versions")
	w := h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_artver_Input")
	h.WaitRun(runID)

	got := cards(t, h, runID)
	if len(got) != 2 || got[0].ArtifactID == "" || got[0].ArtifactID != got[1].ArtifactID || got[0].Version != 1 || got[1].Version != 2 {
		t.Fatalf("cards = %+v", got)
	}
	if !got[0].Pinned || got[0].Type != "file" || got[0].Preview == nil || !strings.Contains(got[0].Preview.Text, "first draft") {
		t.Fatalf("v1 card = %+v", got[0])
	}
	if got[1].Title != "The report" {
		t.Fatalf("v2 title = %q", got[1].Title)
	}
	_ = os.Remove(filepath.Join(w.Workspace, "report.md"))

	read := func(v int32) (*v1.ReadArtifactFileResponse, error) {
		res, err := h.Client.ReadArtifactFile(h.Ctx(), connect.NewRequest(&v1.ReadArtifactFileRequest{BotId: bot.GetId(), ArtifactId: got[0].ArtifactID, Version: v}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	if r, err := read(1); err != nil || string(r.GetData()) != "# Report\n\nfirst draft\n" {
		t.Fatalf("v1 = %v, %v", r, err)
	}
	if r, err := read(2); err != nil || string(r.GetData()) != "# Report\n\nsecond draft\n" {
		t.Fatalf("v2 = %v, %v", r, err)
	}
	meta, err := h.Client.GetArtifact(h.Ctx(), connect.NewRequest(&v1.GetArtifactRequest{BotId: bot.GetId(), ArtifactId: got[0].ArtifactID}))
	if err != nil || len(meta.Msg.GetVersions()) != 2 || meta.Msg.GetVersion().GetN() != 2 || !meta.Msg.GetApps() {
		t.Fatalf("GetArtifact = %v, %v", meta, err)
	}

	// The download is the pinned version too, and never a page.
	q := url.Values{"bot_id": {bot.GetId()}, "artifact_id": {got[0].ArtifactID}, "v": {"1"}}
	res, err := h.HTTP.Get(h.URL + "/artifacts/download?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "# Report\n\nfirst draft\n" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %q %q", res.StatusCode, body, res.Header.Get("Content-Disposition"))
	}

}

// A forged blob is refused on its first read.
func TestArtifactReadChecksTheHash(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_arthash",
		dummy.Turn{ToolCalls: []llm.ToolCall{writeCall("a.txt", "the real bytes"), artifactCall(`{"path":"a.txt"}`)}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Hash")
	w := h.StartWorker(bot.GetId())
	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_arthash_Input")
	h.WaitRun(runID)
	card := cards(t, h, runID)[0]
	_ = filepath.Walk(filepath.Join(w.Artifacts, "blobs"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			_ = os.WriteFile(p, []byte("forged bytes"), 0o600)
		}
		return nil
	})
	_, err := h.Client.ReadArtifactFile(h.Ctx(), connect.NewRequest(&v1.ReadArtifactFileRequest{BotId: bot.GetId(), ArtifactId: card.ArtifactID}))
	if code(err) != connect.CodeDataLoss {
		t.Fatalf("forged read = %v", err)
	}
}

// Save skill installs the version on the card, not what the folder holds now.
func TestSaveSkillInstallsThePinnedVersion(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artskill",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			writeCall("my-skill/SKILL.md", "---\nname: my-skill\ndescription: the first one\n---\n# v1\n"),
			artifactCall(`{"path":"my-skill"}`),
			writeCall("my-skill/SKILL.md", "---\nname: my-skill\ndescription: changed later\n---\n# v2\n"),
		}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Skills")
	h.StartWorker(bot.GetId())
	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_artskill_Input")
	h.WaitRun(runID)
	card := cards(t, h, runID)[0]
	if card.Type != "skill" || card.Name != "my-skill" || card.Status != "pending" || card.Preview == nil || card.Preview.Text != "the first one" {
		t.Fatalf("card = %+v", card)
	}
	if _, err := h.Client.SaveSkill(h.Ctx(), connect.NewRequest(&v1.SaveSkillRequest{BotId: bot.GetId(), ArtifactId: card.ArtifactID, Version: 1, RunId: runID})); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(h.DataDir, "skills", "users", userID(t, h), "my-skill", "SKILL.md"))
	if err != nil || !strings.Contains(string(raw), "# v1") {
		t.Fatalf("installed = %q, %v", raw, err)
	}
	all := cards(t, h, runID)
	if last := all[len(all)-1]; last.Status != "saved" || last.ArtifactID != card.ArtifactID || last.Version != 1 {
		t.Fatalf("saved card = %+v", last)
	}
}

func userID(t *testing.T, h *apptest.H) string {
	t.Helper()
	var u db.User
	if err := h.DB.Where("email = ?", h.Email).First(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func contentGet(t *testing.T, h *apptest.H, host, path string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.URL+path, nil)
	req.Host = host
	res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

// A live app is served on the content host to the owner's pass, sandboxed,
// with its own files only; signing out ends the pass.
func TestArtifactAppIsSandboxedOnTheContentHost(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artapp",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			writeCall("game/index.html", "<h1>game</h1><script src=app.js></script>"),
			writeCall("game/app.js", "console.log('hi')"),
			artifactCall(`{"path":"game"}`),
		}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Apps")
	h.StartWorker(bot.GetId())
	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_artapp_Input")
	h.WaitRun(runID)
	card := cards(t, h, runID)[0]
	if card.Type != "app" || card.Entry != "index.html" || card.Network {
		t.Fatalf("card = %+v", card)
	}
	open, err := h.Client.OpenArtifactApp(h.Ctx(), connect.NewRequest(&v1.OpenArtifactAppRequest{BotId: bot.GetId(), ArtifactId: card.ArtifactID}))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(open.Msg.GetUrl())
	if !strings.HasPrefix(u.Host, "app-") || !strings.HasSuffix(u.Host, "."+contentTestHost) || !strings.HasSuffix(u.Path, "/index.html") {
		t.Fatalf("app url %q", open.Msg.GetUrl())
	}
	res, body := contentGet(t, h, u.Host, u.Path)
	csp := res.Header.Get("Content-Security-Policy")
	if res.StatusCode != 200 || !strings.Contains(body, "<h1>game</h1>") || !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("app: %d %q csp=%q", res.StatusCode, body, csp)
	}
	if strings.Contains(csp, "https:") || !strings.Contains(csp, "frame-ancestors http://console.test") {
		t.Fatalf("an app without network may reach out: %q", csp)
	}
	if res.Header.Get("Set-Cookie") != "" || res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers %v", res.Header)
	}
	js, body := contentGet(t, h, u.Host, strings.TrimSuffix(u.Path, "index.html")+"app.js")
	if js.StatusCode != 200 || body != "console.log('hi')" || !strings.HasPrefix(js.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("app.js: %d %q", js.StatusCode, body)
	}
	// No climbing out, and no other app's host.
	for _, p := range []string{strings.TrimSuffix(u.Path, "index.html") + "../../etc/passwd", "/index.html", "/_v/nope/index.html"} {
		if r, _ := contentGet(t, h, u.Host, p); r.StatusCode == 200 {
			t.Errorf("%s served", p)
		}
	}
	if r, _ := contentGet(t, h, "app-someone-else-123456."+contentTestHost, u.Path); r.StatusCode == 200 {
		t.Fatal("a pass opened another app's host")
	}
	// Signing out ends the pass at once.
	if _, err := h.Client.SignOut(h.Ctx(), connect.NewRequest(&v1.SignOutRequest{})); err != nil {
		t.Fatal(err)
	}
	if r, _ := contentGet(t, h, u.Host, u.Path); r.StatusCode != http.StatusForbidden {
		t.Fatalf("after sign-out: %d", r.StatusCode)
	}
}

// An app that asks for the internet needs its own rule, Ask by default; a
// refusal leaves no card.
func TestArtifactAppNetworkNeedsTheRule(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artnet",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			writeCall("map.html", "<script src=https://cdn.example/x.js></script>"),
			artifactCall(`{"path":"map.html","network":true}`),
		}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Net")
	h.StartWorker(bot.GetId())
	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_artnet_Input")
	ap := h.WaitApproval(bot.GetId())
	if ap.GetConnector() != "artifact" || ap.GetAction() != "app_network" {
		t.Fatalf("approval %s.%s", ap.GetConnector(), ap.GetAction())
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(runID)
	if got := cards(t, h, runID); len(got) != 0 {
		t.Fatalf("a refused app left a card: %+v", got)
	}

	// Allowed, the app's policy opens to https.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.GetId(), Connector: "artifact", Action: "app_network", Decision: "allow"})); err != nil {
		t.Fatal(err)
	}
	dummy.Script("Test_artnet2",
		dummy.Turn{ToolCalls: []llm.ToolCall{artifactCall(`{"path":"map.html","network":true}`)}},
		dummy.Turn{Text: "done"},
	)
	chat, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	runID, _ = h.Send(bot.GetId(), chat.Msg.GetId(), "Test_artnet2_Input")
	h.WaitRun(runID)
	card := cards(t, h, runID)[0]
	if card.Type != "app" || !card.Network || card.Entry != "map.html" {
		t.Fatalf("card = %+v", card)
	}
	open, err := h.Client.OpenArtifactApp(h.Ctx(), connect.NewRequest(&v1.OpenArtifactAppRequest{BotId: bot.GetId(), ArtifactId: card.ArtifactID}))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(open.Msg.GetUrl())
	if res, _ := contentGet(t, h, u.Host, u.Path); !strings.Contains(res.Header.Get("Content-Security-Policy"), "https:") {
		t.Fatalf("network app csp %q", res.Header.Get("Content-Security-Policy"))
	}
}

// A shared link is a copy: it serves with the Bot's machine gone, and
// revoking it ends it.
func TestArtifactShareOutlivesTheMachine(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artshare",
		dummy.Turn{ToolCalls: []llm.ToolCall{writeCall("page.html", "<p>shared</p>"), artifactCall(`{"path":"page.html"}`)}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Share")
	h.StartWorker(bot.GetId())
	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_artshare_Input")
	h.WaitRun(runID)
	card := cards(t, h, runID)[0]
	sh, err := h.Client.ShareArtifact(h.Ctx(), connect.NewRequest(&v1.ShareArtifactRequest{BotId: bot.GetId(), ArtifactId: card.ArtifactID}))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(sh.Msg.GetUrl())
	if !strings.HasPrefix(u.Host, "share-") {
		t.Fatalf("share url %q", sh.Msg.GetUrl())
	}
	h.App.Hub.Drop(bot.GetId())
	res, body := contentGet(t, h, u.Host, "/")
	if res.StatusCode != 200 || body != "<p>shared</p>" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("shared app with the machine gone: %d %q", res.StatusCode, body)
	}
	list, err := h.Client.ListArtifactShares(h.Ctx(), connect.NewRequest(&v1.ListArtifactSharesRequest{BotId: bot.GetId()}))
	if err != nil || len(list.Msg.GetShares()) != 1 || list.Msg.GetShares()[0].GetVersion() != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if _, err := h.Client.DeleteArtifactShare(h.Ctx(), connect.NewRequest(&v1.DeleteArtifactShareRequest{BotId: bot.GetId(), Id: sh.Msg.GetId()})); err != nil {
		t.Fatal(err)
	}
	if res, _ := contentGet(t, h, u.Host, "/"); res.StatusCode != 404 {
		t.Fatalf("after revoke: %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(h.DataDir, "shares", sh.Msg.GetId())); !os.IsNotExist(err) {
		t.Fatalf("the copy outlived the link: %v", err)
	}
}

// ListArtifacts returns the Bot's artifacts newest-first with their versions
// and chat metadata, and DeleteArtifact removes the artifact, its chat cards,
// its public links, and its pinned bytes on the worker.
func TestListAndDeleteArtifacts(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artlist",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			writeCall("first.md", "# First\n\nhello\n"),
			artifactCall(`{"path":"first.md","title":"First doc"}`),
			writeCall("second.md", "# Second\n\nworld\n"),
			artifactCall(`{"path":"second.md","title":"Second doc"}`),
		}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("ListDelete")
	w := h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_artlist_Input")
	h.WaitRun(runID)

	got := cards(t, h, runID)
	if len(got) != 2 {
		t.Fatalf("cards = %+v", got)
	}
	// Make timestamps deterministic so chronological order is unambiguous.
	now := time.Now()
	h.DB.Model(&db.Artifact{}).Where("id = ?", got[0].ArtifactID).Update("updated_at", now.Add(-2*time.Minute))
	h.DB.Model(&db.Artifact{}).Where("id = ?", got[1].ArtifactID).Update("updated_at", now.Add(-1*time.Minute))

	list, err := h.Client.ListArtifacts(h.Ctx(), connect.NewRequest(&v1.ListArtifactsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	items := list.Msg.GetArtifacts()
	if len(items) != 2 || items[0].GetId() != got[1].ArtifactID || items[1].GetId() != got[0].ArtifactID {
		t.Fatalf("ListArtifacts = %+v", items)
	}
	if items[0].GetChatId() != chat || items[0].GetTitle() != "Second doc" || len(items[0].GetVersions()) != 1 || !strings.Contains(items[0].GetVersions()[0].GetPreviewText(), "world") {
		t.Fatalf("item[0] = %+v", items[0])
	}

	sh, err := h.Client.ShareArtifact(h.Ctx(), connect.NewRequest(&v1.ShareArtifactRequest{BotId: bot.GetId(), ArtifactId: got[1].ArtifactID}))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(sh.Msg.GetUrl())

	if _, err := h.Client.DeleteArtifact(h.Ctx(), connect.NewRequest(&v1.DeleteArtifactRequest{BotId: bot.GetId(), Id: got[1].ArtifactID})); err != nil {
		t.Fatal(err)
	}
	list, err = h.Client.ListArtifacts(h.Ctx(), connect.NewRequest(&v1.ListArtifactsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Msg.GetArtifacts()) != 1 || list.Msg.GetArtifacts()[0].GetId() != got[0].ArtifactID {
		t.Fatalf("after delete ListArtifacts = %+v", list.Msg.GetArtifacts())
	}
	if _, err := h.Client.GetArtifact(h.Ctx(), connect.NewRequest(&v1.GetArtifactRequest{BotId: bot.GetId(), ArtifactId: got[1].ArtifactID})); code(err) != connect.CodeNotFound {
		t.Fatalf("GetArtifact after delete = %v", err)
	}
	if res, _ := contentGet(t, h, u.Host, "/"); res.StatusCode != 404 {
		t.Fatalf("share after delete: %d", res.StatusCode)
	}
	if rem := cards(t, h, runID); len(rem) != 1 || rem[0].ArtifactID != got[0].ArtifactID {
		t.Fatalf("cards after delete = %+v", rem)
	}
	if pins, _ := os.ReadDir(filepath.Join(w.Artifacts, "pins")); len(pins) != 1 {
		t.Fatalf("%d pins after delete", len(pins))
	}
}

// Deleting the chat forgets its artifacts on the Bot's machine.
func TestDeleteChatForgetsItsArtifacts(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_artsweep",
		dummy.Turn{ToolCalls: []llm.ToolCall{writeCall("x.txt", "bytes"), artifactCall(`{"path":"x.txt"}`)}},
		dummy.Turn{Text: "done"},
	)
	h := artifactHarness(t)
	bot := h.CreateBot("Sweep")
	w := h.StartWorker(bot.GetId())
	chat := newChat(t, h, bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_artsweep_Input")
	h.WaitRun(runID)
	if pins, _ := os.ReadDir(filepath.Join(w.Artifacts, "pins")); len(pins) != 1 {
		t.Fatalf("%d pins", len(pins))
	}
	// Made more than a minute ago, so the sweep does not wait for its event.
	h.DB.Model(&db.Artifact{}).Where("bot_id = ?", bot.GetId()).Update("updated_at", time.Now().Add(-2*time.Minute))
	if _, err := h.Client.DeleteChat(h.Ctx(), connect.NewRequest(&v1.DeleteChatRequest{BotId: bot.GetId(), Id: chat})); err != nil {
		t.Fatal(err)
	}
	h.App.Artifacts.SweepBot(h.Ctx(), bot.GetId())
	if pins, _ := os.ReadDir(filepath.Join(w.Artifacts, "pins")); len(pins) != 0 {
		t.Fatalf("%d pins left", len(pins))
	}
	var n int64
	h.DB.Model(&db.Artifact{}).Where("bot_id = ?", bot.GetId()).Count(&n)
	if n != 0 {
		t.Fatalf("%d artifact rows left", n)
	}
}

// A quote rides on the message: shown with it, and told to the model at the
// head of that turn, the same bytes on every replay.
func TestQuotesReachTheModel(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Quotes")
	chat := h.FirstChat(bot.GetId())
	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{
		BotId: bot.GetId(), ChatId: chat, Text: "Test_quote_Input what does this mean?",
		Quotes: []*v1.Quote{
			{Source: "file", Ref: "notes/report.md", Label: "report.md", Text: "line one\nline two", Locator: "lines 4–5"},
			{Source: "nonsense", Text: "dropped"},
			{Source: "message", Text: "  "},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(res.Msg.GetRunId())
	ev := h.LastUserEvent(chat)
	if !strings.Contains(ev.Meta, `"quotes"`) || strings.Contains(ev.Meta, "dropped") {
		t.Fatalf("meta = %s", ev.Meta)
	}
	reqs := dummy.Streamed()
	if len(reqs) == 0 {
		t.Fatal("no request")
	}
	var user string
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.Role == llm.RoleUser {
			user = m.Text
		}
	}
	want := "[The human quotes report.md (/workspace/notes/report.md), lines 4–5]\n> line one\n> line two\n\nTest_quote_Input what does this mean?"
	if !strings.Contains(user, want) {
		t.Fatalf("user turn = %q", user)
	}
	// A second message replays the first turn byte for byte.
	res, err = h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.GetId(), ChatId: chat, Text: "and then?"}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(res.Msg.GetRunId())
	reqs = dummy.Streamed()
	found := false
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Text, want) {
			found = true
		}
	}
	if !found {
		t.Fatal("the quote was not replayed")
	}
}
