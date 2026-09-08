package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

func skillApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a := testApp(t, nil)
	a.Store = testStore(t)
	if err := a.Store.Patch(map[string]string{"data_dir": dir}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SeedSkills(dir); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSkillBlurbAndLoad(t *testing.T) {
	a := skillApp(t)
	a.DB.Create(&db.User{ID: "u"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.ensureDefaultSkill("b1")
	blurb := a.skillBlurb("b1")
	if !strings.Contains(blurb, "`product-self-knowledge`") || strings.Contains(blurb, "## Silo product") {
		t.Fatal(blurb)
	}
	sys := buildSystem(&db.Bot{ID: "b1", Name: "Scout"}, blurb)
	if !strings.Contains(sys, "product-self-knowledge") || strings.Contains(sys, "Two hops") {
		t.Fatal("body leaked")
	}
	out, err := a.loadSkill("b1", catalog.DefaultSkill, "")
	if err != nil || !strings.Contains(out, "Two hops") || !strings.Contains(out, "/opt/silo/skills/product-self-knowledge/") {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := a.loadSkill("b1", "nope", ""); err == nil {
		t.Fatal("disabled")
	}
}

func TestSetBotSkillUniqueName(t *testing.T) {
	a := skillApp(t)
	u := &db.User{ID: "u", Admin: true}
	a.DB.Create(u)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ctx := context.WithValue(context.Background(), userKey, u)
	root := skills.PersonalDir(a.cfg().DataDir, "u")
	writePersonal := func() {
		dir := filepath.Join(root, catalog.DefaultSkill)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: product-self-knowledge\ndescription: Personal copy of product knowledge. Use when testing clash.\n---\n\n# x\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePersonal()
	a.ensureDefaultSkill("b1")
	_, err := a.SetBotSkill(ctx, connect.NewRequest(&v1.SetBotSkillRequest{
		BotId: "b1", Kind: skills.KindPersonal, Name: catalog.DefaultSkill, Enabled: true,
	}))
	if err == nil {
		t.Fatal("expected clash")
	}
}

func TestProposeSkillAllow(t *testing.T) {
	conn, action, ok := chatTool("propose_skill")
	if !ok || conn != "skills" || action != "propose" {
		t.Fatal(conn, action)
	}
	conn, action, ok = chatTool("skill")
	if !ok || conn != "skills" || action != "load" {
		t.Fatal(conn, action)
	}
}

func TestSeededChipNotPersonal(t *testing.T) {
	a := skillApp(t)
	u := &db.User{ID: "u"}
	a.DB.Create(u)
	ctx := context.WithValue(context.Background(), userKey, u)
	lib, err := a.ListSkills(ctx, connect.NewRequest(&v1.ListSkillsRequest{Scope: "library"}))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range lib.Msg.Skills {
		if s.Name == catalog.DefaultSkill {
			found = true
			if !s.Seeded {
				t.Fatal("library embed should be seeded")
			}
		}
	}
	if !found {
		t.Fatal("missing catalog skill")
	}
	root := skills.PersonalDir(a.cfg().DataDir, "u")
	dir := filepath.Join(root, "copied-seed")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: copied-seed\ndescription: Personal copy that stole silo_seed.\nmetadata:\n  silo_seed: product-self-knowledge\n---\n\n# x\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mine, err := a.ListSkills(ctx, connect.NewRequest(&v1.ListSkillsRequest{Scope: "personal"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Msg.Skills) != 1 || mine.Msg.Skills[0].Name != "copied-seed" || mine.Msg.Skills[0].Seeded {
		t.Fatalf("personal seeded %+v", mine.Msg.Skills)
	}
}

func TestListSkillFiles(t *testing.T) {
	a := skillApp(t)
	u := &db.User{ID: "u"}
	a.DB.Create(u)
	ctx := context.WithValue(context.Background(), userKey, u)
	r, err := a.ListSkillFiles(ctx, connect.NewRequest(&v1.ListSkillFilesRequest{
		Scope: "library", Name: catalog.DefaultSkill,
	}))
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range r.Msg.Entries {
		if e.Name == "SKILL.md" && !e.Dir {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("entries %+v", r.Msg.Entries)
	}
	view, err := a.ReadSkillFile(ctx, connect.NewRequest(&v1.ReadSkillFileRequest{
		Scope: "library", Name: catalog.DefaultSkill, Path: "SKILL.md",
	}))
	if err != nil || !strings.Contains(view.Msg.Content, "product-self-knowledge") {
		t.Fatalf("%v %v", view, err)
	}
}
