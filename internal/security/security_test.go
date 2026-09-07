package security

import "testing"

func TestDescribeSecrets(t *testing.T) {
	p := Describe("secrets", "TEST", `{"name":"TEST"}`)
	if p.Title != "Read a secret" {
		t.Fatalf("title %q", p.Title)
	}
	if len(p.Fields) != 1 || p.Fields[0].Label != "Secret" || p.Fields[0].Value != "TEST" {
		t.Fatalf("fields %+v", p.Fields)
	}
	if p.Summary == "" || p.Summary[0] == '{' {
		t.Fatalf("summary %q", p.Summary)
	}
	p = Describe("secrets", "imap_password", "")
	if len(p.Fields) != 1 || p.Fields[0].Value != "imap_password" {
		t.Fatalf("action name %+v", p.Fields)
	}
}

func TestDescribeUnknown(t *testing.T) {
	p := Describe("mail", "send", `{"to":"a@b.c"}`)
	if p.Title != "Send · Mail" {
		t.Fatalf("title %q", p.Title)
	}
	if len(p.Fields) != 1 || p.Fields[0].Label != "To" || p.Fields[0].Value != "a@b.c" {
		t.Fatalf("fields %+v", p.Fields)
	}
}

func TestRedactDesktopType(t *testing.T) {
	got := Redact("desktop", "type", `{"text":"secret","x":1}`)
	if got != `{"x":"1"}` && got != `{"x":1}` {
		if parseArgs(got)["text"] != "" {
			t.Fatalf("text leaked %s", got)
		}
		if parseArgs(got)["x"] != "1" {
			t.Fatalf("got %s", got)
		}
	}
	if parseArgs(Redact("desktop", "type", `{"text":"pw"}`))["text"] != "" {
		t.Fatal("text")
	}
}

func TestDefault(t *testing.T) {
	if m, ok := Default("python", "run"); !ok || m != Allow {
		t.Fatal("python")
	}
	if m, ok := Default("files", "read"); !ok || m != Allow {
		t.Fatal("files")
	}
	if m, ok := Default("desktop", "type"); !ok || m != Allow {
		t.Fatal("desktop")
	}
	if m, ok := Default("secrets", "vendor_password"); !ok || m != Ask {
		t.Fatal("secret")
	}
	if _, ok := Default("twilio_docs", "search"); ok {
		t.Fatal("mcp")
	}
}

func TestVote(t *testing.T) {
	if Vote("allow_once") != Once || Vote("allow") != Once || Vote("always") != Always || Vote("deny") != Deny {
		t.Fatal("vote")
	}
	if Vote("nope") != "" || !Granted(Once) || Granted(Deny) {
		t.Fatal("grant")
	}
}

func TestReserved(t *testing.T) {
	if !Reserved("desktop") || !Reserved("secrets") || Reserved("github") {
		t.Fatal("reserved")
	}
}
