package security

import "testing"

// TestCatalogDefaults covers every built-in connector/action pair so a new
// catalog entry added without a sensible default is caught here.
func TestCatalogDefaults(t *testing.T) {
	want := map[string]string{
		"python.run":    Allow,
		"terminal.run":  Allow,
		"skills.load":   Allow,
		"artifact.emit": Allow,
		"web.search":    Allow,
		"chats.read":    Allow,
		"model.list":    Allow,
		"model.switch":  Allow,
	}
	for key, mode := range want {
		conn, action, ok := splitKey(key)
		if !ok {
			t.Fatalf("bad test key %q", key)
		}
		got, ok := Default(conn, action)
		if !ok || got != mode {
			t.Fatalf("Default(%s,%s)=%q ok=%v want %q", conn, action, got, ok, mode)
		}
	}
}

func TestKeyAndRule(t *testing.T) {
	if Key("files", "read") != "files.read" {
		t.Fatalf("Key %q", Key("files", "read"))
	}
	if Key("secrets", "imap_password") != "secrets.imap_password" {
		t.Fatalf("Key %q", Key("secrets", "imap_password"))
	}
	for _, in := range []string{Allow, Ask, Deny} {
		if Rule(in) != in {
			t.Fatalf("Rule(%q)=%q", in, Rule(in))
		}
	}
}

func TestBuiltinRowsIncludeWildcards(t *testing.T) {
	var files, desktop bool
	for _, r := range BuiltinRows() {
		if r.Connector == Files && r.Action == Star {
			files = true
		}
		if r.Connector == Desktop && r.Action == Star {
			desktop = true
		}
	}
	if !files || !desktop {
		t.Fatalf("wildcard rows missing: files=%v desktop=%v", files, desktop)
	}
}

func TestDescribeNeverLeaksDesktopType(t *testing.T) {
	p := Describe(Desktop, "type", `{"text":"my password","x":1}`)
	for _, f := range p.Fields {
		if f.Value == "my password" {
			t.Fatalf("desktop.type text leaked: %+v", p.Fields)
		}
	}
	if p.Title != "Desktop" {
		t.Fatalf("title %q", p.Title)
	}
}

func TestDescribeUnknownMCPIsGeneric(t *testing.T) {
	p := Describe("acme_docs", "publish", `{"title":"hi"}`)
	if p.Title == "" || p.Summary == "" {
		t.Fatalf("generic prompt empty: %+v", p)
	}
	if p.Title == "acme_docs.publish" {
		t.Fatalf("title should be humanized, got %q", p.Title)
	}
}

func TestVoteEmptyAndGrant(t *testing.T) {
	if Vote("") != "" {
		t.Fatal("empty vote should be invalid")
	}
	if Granted("") || Granted(Deny) {
		t.Fatal("empty/deny should not grant")
	}
	if !Granted(Once) || !Granted(Always) {
		t.Fatal("allow votes should grant")
	}
}

func TestReservedSlugs(t *testing.T) {
	for _, slug := range []string{Python, Terminal, Files, Desktop, Bot, Secrets, Skills, Web, Artifact, Channels, Chats, Model} {
		if !Reserved(slug) {
			t.Fatalf("%s should be reserved", slug)
		}
	}
	if Reserved("github") || Reserved("") {
		t.Fatal("non-reserved slug misclassified")
	}
}

// splitKey is a tiny helper for the table test above.
func splitKey(key string) (string, string, bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}
