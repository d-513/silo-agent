package app_test

import (
	"encoding/json"
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

// knowledgeFixture is a Bot with a real worker and a small docs folder.
type knowledgeFixture struct {
	h      *apptest.H
	botID  string
	ws     string
	folder *v1.KnowledgeFolder
}

func (f *knowledgeFixture) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(f.ws, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *knowledgeFixture) sync(t *testing.T) {
	t.Helper()
	if err := f.h.App.SyncKnowledgeFolder(f.h.Ctx(), f.folder.GetId()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func (f *knowledgeFixture) list(t *testing.T) *v1.KnowledgeFolder {
	t.Helper()
	res, err := f.h.Client.ListKnowledge(f.h.Ctx(), connect.NewRequest(&v1.ListKnowledgeRequest{BotId: f.botID}))
	if err != nil {
		t.Fatal(err)
	}
	for _, fo := range res.Msg.GetFolders() {
		if fo.GetId() == f.folder.GetId() {
			return fo
		}
	}
	t.Fatalf("folder %s not listed", f.folder.GetId())
	return nil
}

func (f *knowledgeFixture) search(t *testing.T, q string) []*v1.KnowledgeHit {
	t.Helper()
	res, err := f.h.Client.SearchKnowledge(f.h.Ctx(), connect.NewRequest(&v1.SearchKnowledgeRequest{BotId: f.botID, Query: q}))
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return res.Msg.GetHits()
}

func (f *knowledgeFixture) chunkIDs(path string) string {
	var src db.KnowledgeSource
	if err := f.h.DB.Where("bot_id = ? AND path = ?", f.botID, path).First(&src).Error; err != nil {
		return "(no source)"
	}
	var ids []string
	f.h.DB.Model(&db.KnowledgeChunk{}).Where("source_id = ?", src.ID).Order("ord").Pluck("id", &ids)
	return strings.Join(ids, ",")
}

func newKnowledge(t *testing.T, folder string, files map[string]string) *knowledgeFixture {
	t.Helper()
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Librarian")
	w := h.StartWorker(bot.GetId())
	f := &knowledgeFixture{h: h, botID: bot.GetId(), ws: w.Workspace}
	for rel, body := range files {
		f.write(t, rel, body)
	}
	res, err := h.Client.AddKnowledgeFolder(h.Ctx(), connect.NewRequest(&v1.AddKnowledgeFolderRequest{BotId: bot.GetId(), Path: folder}))
	if err != nil {
		t.Fatalf("add folder: %v", err)
	}
	f.folder = res.Msg
	f.sync(t)
	return f
}

var docsFixture = map[string]string{
	"docs/deploy.md": "# Deploying\n\nWe deploy releases every Friday afternoon using the release pipeline.\n",
	"docs/cats.md":   "# Cats\n\nThe office cat is named Miso and sleeps on the warm printer.\n",
	"docs/parts.txt": "Replacement valve part number XK-4471-B ships from the Rotterdam warehouse.\n",
}

func TestKnowledgeIndexAndSearch(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)

	fo := f.list(t)
	if fo.GetStatus() != "idle" || fo.GetFiles() != 3 || fo.GetChunks() < 3 || fo.GetLastSyncAt() == "" {
		t.Fatalf("folder after sync = %+v", fo)
	}

	hits := f.search(t, "when do we deploy releases")
	if len(hits) == 0 || hits[0].GetPath() != "docs/deploy.md" {
		t.Fatalf("semantic hit = %+v", hits)
	}
	if hits[0].GetLocator() == "" || !strings.Contains(hits[0].GetSnippet(), "Friday") || hits[0].GetIndexedAt() == "" {
		t.Fatalf("hit lacks locator/snippet/indexed_at: %+v", hits[0])
	}

	// An exact part number is a keyword win, not only a vector one.
	hits = f.search(t, "XK-4471-B")
	if len(hits) == 0 || hits[0].GetPath() != "docs/parts.txt" {
		t.Fatalf("keyword hit = %+v", hits)
	}

	if hits := f.search(t, "quantum zebra"); len(hits) != 0 {
		t.Fatalf("unrelated query should find nothing, got %+v", hits)
	}

	// The path filter narrows to a prefix.
	res, err := f.h.Client.SearchKnowledge(f.h.Ctx(), connect.NewRequest(&v1.SearchKnowledgeRequest{
		BotId: f.botID, Query: "office cat Miso", Path: "docs/deploy.md",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Msg.GetHits() {
		if h.GetPath() != "docs/deploy.md" {
			t.Fatalf("path filter leaked %s", h.GetPath())
		}
	}
}

func TestKnowledgeIsPerBot(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	other := f.h.CreateBot("Stranger")
	res, err := f.h.Client.SearchKnowledge(f.h.Ctx(), connect.NewRequest(&v1.SearchKnowledgeRequest{BotId: other.GetId(), Query: "deploy releases"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.GetHits()) != 0 {
		t.Fatalf("another bot's search leaked: %+v", res.Msg.GetHits())
	}
	if _, err := f.h.Client.RemoveKnowledgeFolder(f.h.Ctx(), connect.NewRequest(&v1.RemoveKnowledgeFolderRequest{BotId: other.GetId(), Id: f.folder.GetId()})); err == nil {
		t.Fatal("another bot must not remove this folder")
	}
}

func TestKnowledgeResyncOnlyTouchesChangedFiles(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	deploy, cats := f.chunkIDs("docs/deploy.md"), f.chunkIDs("docs/cats.md")

	// Nothing changed: nothing is re-embedded.
	f.sync(t)
	if f.chunkIDs("docs/deploy.md") != deploy || f.chunkIDs("docs/cats.md") != cats {
		t.Fatal("an unchanged sync rewrote chunks")
	}

	// A touch (new mtime, same bytes) is confirmed by the hash and skipped.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(f.ws, "docs/cats.md"), later, later); err != nil {
		t.Fatal(err)
	}
	f.sync(t)
	if f.chunkIDs("docs/cats.md") != cats {
		t.Fatal("a touch without an edit re-embedded the file")
	}
	var src db.KnowledgeSource
	f.h.DB.First(&src, "bot_id = ? AND path = ?", f.botID, "docs/cats.md")
	if src.Mtime != later.UnixNano() {
		t.Fatalf("mtime not refreshed after a confirmed touch: %d", src.Mtime)
	}

	// A real edit re-embeds that file only.
	f.write(t, "docs/deploy.md", "# Deploying\n\nWe now deploy releases every Tuesday morning instead.\n")
	f.sync(t)
	if f.chunkIDs("docs/deploy.md") == deploy {
		t.Fatal("an edited file kept its old chunks")
	}
	if f.chunkIDs("docs/cats.md") != cats {
		t.Fatal("an unrelated file was re-embedded")
	}
	hits := f.search(t, "deploy releases Tuesday")
	if len(hits) == 0 || !strings.Contains(hits[0].GetSnippet(), "Tuesday") || strings.Contains(hits[0].GetSnippet(), "Friday") {
		t.Fatalf("search still sees the old text: %+v", hits)
	}
}

func TestKnowledgeRenameKeepsChunksAndDeleteDropsThem(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	deploy := f.chunkIDs("docs/deploy.md")

	if err := os.MkdirAll(filepath.Join(f.ws, "docs/ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.ws, "docs/deploy.md"), filepath.Join(f.ws, "docs/ops/release.md")); err != nil {
		t.Fatal(err)
	}
	f.sync(t)
	if got := f.chunkIDs("docs/ops/release.md"); got != deploy {
		t.Fatalf("a rename re-embedded: %q vs %q", got, deploy)
	}
	if got := f.chunkIDs("docs/deploy.md"); got != "(no source)" {
		t.Fatalf("old path still indexed: %q", got)
	}
	if hits := f.search(t, "deploy releases Friday"); len(hits) == 0 || hits[0].GetPath() != "docs/ops/release.md" {
		t.Fatalf("hit after rename = %+v", hits)
	}

	if err := os.Remove(filepath.Join(f.ws, "docs/cats.md")); err != nil {
		t.Fatal(err)
	}
	f.sync(t)
	var n int64
	f.h.DB.Model(&db.KnowledgeChunk{}).Joins("JOIN knowledge_sources s ON s.id = knowledge_chunks.source_id").
		Where("s.bot_id = ? AND s.path = ?", f.botID, "docs/cats.md").Count(&n)
	if n != 0 || f.chunkIDs("docs/cats.md") != "(no source)" {
		t.Fatalf("deleted file left %d chunks", n)
	}
	if fo := f.list(t); fo.GetFiles() != 2 {
		t.Fatalf("files = %d, want 2", fo.GetFiles())
	}
}

func TestKnowledgeEmbedModelChangeReembeds(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	before := f.chunkIDs("docs/cats.md")
	f.h.DB.Model(&db.KnowledgeSource{}).Where("bot_id = ?", f.botID).Update("embed_model", "old/model")
	f.sync(t)
	if f.chunkIDs("docs/cats.md") == before {
		t.Fatal("a changed embedding model must re-embed")
	}
	var src db.KnowledgeSource
	f.h.DB.First(&src, "bot_id = ? AND path = ?", f.botID, "docs/cats.md")
	if src.EmbedModel != "dummy/embed" {
		t.Fatalf("embed_model = %q", src.EmbedModel)
	}
}

func TestKnowledgeSkipsBinaryAndReportsIssues(t *testing.T) {
	f := newKnowledge(t, "docs", map[string]string{
		"docs/ok.md":    "# Fine\n\nplain readable notes about turbines\n",
		"docs/blob.bin": "ab\x00\x01\x02cd",
	})
	fo := f.list(t)
	if fo.GetFiles() != 1 || fo.GetSkipped() != 1 {
		t.Fatalf("folder = %+v", fo)
	}
	if len(fo.GetIssues()) != 1 || fo.GetIssues()[0].GetPath() != "docs/blob.bin" || fo.GetIssues()[0].GetDetail() == "" {
		t.Fatalf("issues = %+v", fo.GetIssues())
	}
}

func TestKnowledgeDriveFolderNeverWipedByAFlakyMount(t *testing.T) {
	f := newKnowledge(t, "drives/gd", map[string]string{
		"drives/gd/plan.md": "# Plan\n\nThe quarterly roadmap focuses on turbines.\n",
	})
	if !f.list(t).GetDrive() {
		t.Fatal("a folder under drives/ must be flagged as a drive")
	}
	before := f.chunkIDs("drives/gd/plan.md")

	// The mount comes up empty: the folder exists but shows no files.
	if err := os.Remove(filepath.Join(f.ws, "drives/gd/plan.md")); err != nil {
		t.Fatal(err)
	}
	if err := f.h.App.SyncKnowledgeFolder(f.h.Ctx(), f.folder.GetId()); err == nil {
		t.Fatal("an empty drive folder with an index must be treated as disconnected")
	}
	if fo := f.list(t); fo.GetStatus() != "error" || !strings.Contains(strings.ToLower(fo.GetDetail()), "drive") {
		t.Fatalf("folder = %+v", fo)
	}
	f.write(t, "drives/gd/plan.md", "# Plan\n\nThe quarterly roadmap focuses on turbines.\n")
	if f.chunkIDs("drives/gd/plan.md") != before {
		t.Fatal("the index was wiped while the drive looked empty")
	}

	// The mount vanishes entirely: still an error, still no wipe.
	if err := os.RemoveAll(filepath.Join(f.ws, "drives/gd")); err != nil {
		t.Fatal(err)
	}
	if err := f.h.App.SyncKnowledgeFolder(f.h.Ctx(), f.folder.GetId()); err == nil {
		t.Fatal("a missing folder must be an error")
	}
	if f.chunkIDs("drives/gd/plan.md") != before {
		t.Fatal("the index was wiped while the folder was missing")
	}
	if fo := f.list(t); fo.GetStatus() != "error" {
		t.Fatalf("folder = %+v", fo)
	}
}

func TestKnowledgeAddFolderRules(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Picky")
	w := h.StartWorker(bot.GetId())
	for _, d := range []string{"docs/sub", "drives/gd", "bot", "tmp"} {
		if err := os.MkdirAll(filepath.Join(w.Workspace, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	add := func(p string) error {
		_, err := h.Client.AddKnowledgeFolder(h.Ctx(), connect.NewRequest(&v1.AddKnowledgeFolderRequest{BotId: bot.GetId(), Path: p}))
		return err
	}
	if err := add("docs"); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"duplicate":          "docs",
		"nested inside":      "docs/sub",
		"workspace root":     "/workspace",
		"scratch":            "bot",
		"tmp is input only":  "tmp",
		"all drives at once": "drives",
		"missing":            "nope",
		"escapes":            "../etc",
	} {
		if err := add(p); err == nil {
			t.Fatalf("%s (%q) should be refused", name, p)
		}
	}
	if err := add("/workspace/docs/"); err == nil {
		t.Fatal("the same folder spelled another way is a duplicate")
	}
	if err := add("drives/gd"); err != nil {
		t.Fatalf("one drive folder is fine: %v", err)
	}
}

func TestKnowledgeRemoveDropsEverything(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	if _, err := f.h.Client.RemoveKnowledgeFolder(f.h.Ctx(), connect.NewRequest(&v1.RemoveKnowledgeFolderRequest{BotId: f.botID, Id: f.folder.GetId()})); err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]any{"folders": &db.KnowledgeFolder{}, "sources": &db.KnowledgeSource{}, "chunks": &db.KnowledgeChunk{}} {
		var n int64
		f.h.DB.Model(model).Where("bot_id = ?", f.botID).Count(&n)
		if n != 0 {
			t.Fatalf("%s left %d rows", name, n)
		}
	}
	if hits := f.search(t, "deploy releases"); len(hits) != 0 {
		t.Fatalf("removed folder still searchable: %+v", hits)
	}
}

func TestKnowledgeDeleteBotDropsIndex(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	if _, err := f.h.Client.DeleteBot(f.h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: f.botID})); err != nil {
		t.Fatal(err)
	}
	var n int64
	f.h.DB.Model(&db.KnowledgeChunk{}).Where("bot_id = ?", f.botID).Count(&n)
	if n != 0 {
		t.Fatalf("delete bot left %d chunks", n)
	}
}

func TestKnowledgeSweepRespectsDirtyAndInterval(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	now := time.Now()
	if got := f.h.App.SweepKnowledge(now); got != 0 {
		t.Fatalf("a just-synced folder was swept again (%d)", got)
	}
	f.write(t, "docs/cats.md", "# Cats\n\nThe office cat is now named Pixel.\n")
	f.h.DB.Model(&db.KnowledgeFolder{}).Where("id = ?", f.folder.GetId()).Update("dirty_at", now.Add(-time.Minute))
	if got := f.h.App.SweepKnowledge(now); got != 1 {
		t.Fatalf("a dirty folder was not swept (%d)", got)
	}
	if hits := f.search(t, "office cat Pixel"); len(hits) == 0 || !strings.Contains(hits[0].GetSnippet(), "Pixel") {
		t.Fatalf("sweep did not index the edit: %+v", hits)
	}
	// Past the interval an idle folder is re-checked even when nothing is dirty.
	if got := f.h.App.SweepKnowledge(now.Add(time.Hour)); got != 1 {
		t.Fatalf("a stale folder was not re-checked (%d)", got)
	}
}

func TestKnowledgeToolAndRule(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	dummy.Script("Test_KN1",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "search_docs", Arguments: `{"query":"when do we deploy releases"}`}}},
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "search_docs", Arguments: `{"query":"quantum zebra"}`}}},
		dummy.Turn{Text: "done"},
	)
	run, _ := f.h.Send(f.botID, f.h.FirstChat(f.botID), "Test_KN1_Input")
	f.h.WaitRun(run)
	var results []string
	for _, ev := range f.h.Events(run) {
		if ev.Kind == "tool_result" {
			results = append(results, ev.Body)
		}
	}
	if len(results) != 2 {
		t.Fatalf("tool results = %q\n%s", results, f.h.RunBody(run))
	}
	if !strings.Contains(results[0], "docs/deploy.md") || !strings.Contains(results[0], "Friday") {
		t.Fatalf("search_docs result lacks the cited path/snippet: %q", results[0])
	}
	if !strings.Contains(strings.ToLower(results[1]), "no matches") {
		t.Fatalf("empty search should say so: %q", results[1])
	}
}

func TestKnowledgeToolOnlyOfferedWithAFolder(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Bare")
	dummy.Script("Test_KN2", dummy.Turn{Text: "hi"})
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_KN2_Input")
	h.WaitRun(run)
	offered := func() bool {
		for _, req := range dummy.Streamed() {
			for _, tl := range req.Tools {
				if tl.Name == "search_docs" {
					return true
				}
			}
		}
		return false
	}
	if offered() {
		t.Fatal("search_docs offered to a Bot with no indexed folder")
	}

	w := h.StartWorker(bot.GetId())
	if err := os.MkdirAll(filepath.Join(w.Workspace, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.AddKnowledgeFolder(h.Ctx(), connect.NewRequest(&v1.AddKnowledgeFolderRequest{BotId: bot.GetId(), Path: "docs"})); err != nil {
		t.Fatal(err)
	}
	dummy.Script("Test_KN3", dummy.Turn{Text: "hi"})
	chat, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	run2, _ := h.Send(bot.GetId(), chat.Msg.GetId(), "Test_KN3_Input")
	h.WaitRun(run2)
	if !offered() {
		t.Fatal("search_docs missing once a folder is indexed")
	}
}

func TestKnowledgeWritesMarkFolderDirty(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	dummy.Script("Test_KN4",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"docs/new.md","content":"fresh note about harbours\n"}`},
		}},
		dummy.Turn{Text: "ok"},
	)
	run, _ := f.h.Send(f.botID, f.h.FirstChat(f.botID), "Test_KN4_Input")
	f.h.WaitRun(run)
	var fo db.KnowledgeFolder
	f.h.DB.First(&fo, "id = ?", f.folder.GetId())
	if fo.DirtyAt == nil {
		t.Fatal("a Bot write under the folder should mark it dirty")
	}

	// A write elsewhere does not.
	f.h.DB.Model(&db.KnowledgeFolder{}).Where("id = ?", f.folder.GetId()).Update("dirty_at", nil)
	dummy.Script("Test_KN5",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"elsewhere/x.md","content":"x\n"}`},
		}},
		dummy.Turn{Text: "ok"},
	)
	chat, _ := f.h.Client.CreateChat(f.h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: f.botID}))
	run2, _ := f.h.Send(f.botID, chat.Msg.GetId(), "Test_KN5_Input")
	f.h.WaitRun(run2)
	var after db.KnowledgeFolder
	f.h.DB.First(&after, "id = ?", f.folder.GetId())
	if after.DirtyAt != nil {
		t.Fatalf("a write outside the folder marked it dirty (%v)", after.DirtyAt)
	}
}

func TestKnowledgePythonPathIsStructuredAndGated(t *testing.T) {
	f := newKnowledge(t, "docs", docsFixture)
	res := pyCall(t, f.h, f.botID, "", "bot", "search_docs", `{"query":"when do we deploy releases","limit":3}`)
	var got struct {
		Results []struct {
			Path, Locator, Snippet string
			Score                  float64
			IndexedAt              string `json:"indexed_at"`
		} `json:"results"`
	}
	if res.GetError() != "" || json.Unmarshal([]byte(res.GetResultJson()), &got) != nil || len(got.Results) == 0 {
		t.Fatalf("search_docs should be structured for Python: %+v", res)
	}
	if got.Results[0].Path != "docs/deploy.md" || got.Results[0].Locator == "" || got.Results[0].IndexedAt == "" {
		t.Fatalf("result = %+v", got.Results[0])
	}

	if _, err := f.h.Client.SetRule(f.h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: f.botID, Connector: "bot", Action: "search_docs", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, f.h, f.botID, "", "bot", "search_docs", `{"query":"deploy"}`); res.GetError() != "denied" {
		t.Fatalf("deny rule: %+v", res)
	}
}
