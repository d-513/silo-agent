package security

import "testing"

func TestDescribeSecretsGet(t *testing.T) {
	p := Describe("secrets", "get", `{"name":"TEST"}`)
	if p.Title != "Read a secret" {
		t.Fatalf("title %q", p.Title)
	}
	if len(p.Fields) != 1 || p.Fields[0].Label != "Secret" || p.Fields[0].Value != "TEST" {
		t.Fatalf("fields %+v", p.Fields)
	}
	if p.Summary == "" || p.Summary[0] == '{' {
		t.Fatalf("summary %q", p.Summary)
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

func TestVote(t *testing.T) {
	if Vote("allow_once") != Once || Vote("allow") != Once || Vote("always") != Always || Vote("deny") != Deny {
		t.Fatal("vote")
	}
	if Vote("nope") != "" || !Granted(Once) || Granted(Deny) {
		t.Fatal("grant")
	}
}
