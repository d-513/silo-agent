package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
)

// The /artifacts/ route must reach the download handler (401 unauthenticated),
// not a SPA fallback that would return HTML.
func TestArtifactRouteMounted(t *testing.T) {
	a := testApp(t, nil)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/artifacts/file?bot_id=x&path=y")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestArtifactJSONRoundTrip(t *testing.T) {
	raw := artifactJSON(artifactInfo{
		Type: "file", Name: "q3.pdf", Title: "Q3", Path: "reports/q3.pdf",
		Scope: "workspace", Status: "ready", Size: 2048,
	})
	var got artifactInfo
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "file" || got.Name != "q3.pdf" || got.Size != 2048 || got.Status != "ready" {
		t.Fatalf("%+v", got)
	}
}

func TestDescribeArtifactValidation(t *testing.T) {
	a := testApp(t, nil)
	if _, err := a.describeArtifact(context.Background(), "b", "   ", "", ""); err == nil {
		t.Fatal("path required")
	}
	if _, err := a.describeArtifact(context.Background(), "b", "x", "bogus", ""); err == nil {
		t.Fatal("kind must be skill or file")
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"q3 report.pdf": "q3_report.pdf",
		"../../etc":     "_.._etc",
		"":              "artifact",
		"skill":         "skill",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestArtifactMIME(t *testing.T) {
	if artifactMIME("a.pdf") != "application/pdf" {
		t.Fatal("pdf")
	}
	if artifactMIME("a.png") != "image/png" {
		t.Fatal("png")
	}
	if artifactMIME("weird.xyz") != "" {
		t.Fatal("unknown")
	}
}

func TestDownloadSkillZipLibrary(t *testing.T) {
	a := skillApp(t)
	u := &db.User{ID: "u"}
	a.DB.Create(u)
	req := httptest.NewRequest("GET", "/artifacts/skill.zip?scope=library&name="+catalog.DefaultSkill, nil)
	rec := httptest.NewRecorder()
	a.downloadSkillZip(rec, req, u)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type %s", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "SKILL.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SKILL.md missing from zip: %+v", zr.File)
	}
}

func TestArtifactEmitEvent(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Run{ID: "r1", BotID: "b1", ChatID: "c1"})
	a.emitArtifact("b1", "r1", artifactInfo{Type: "file", Name: "q3.pdf", Path: "q3.pdf", Status: "ready"})
	var rows []db.RunEvent
	a.DB.Where("run_id = ? AND kind = ?", "r1", "artifact").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("events %+v", rows)
	}
	var got artifactInfo
	if err := json.Unmarshal([]byte(rows[0].Body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "file" || got.Name != "q3.pdf" {
		t.Fatalf("%+v", got)
	}
}

func TestArtifactReservedGate(t *testing.T) {
	if !security.Reserved(security.Artifact) {
		t.Fatal("artifact must be reserved")
	}
	if m, ok := security.Default(security.Artifact, "emit"); !ok || m != security.Allow {
		t.Fatalf("artifact emit default %q %v", m, ok)
	}
}
