package skills

import (
	"strings"
	"testing"
)

func TestParseRejections(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":    "# just a heading\n",
		"unclosed":          "---\nname: ok\ndescription: d\n",
		"bad name":          "---\nname: Bad Name\ndescription: d\n---\nbody\n",
		"empty description": "---\nname: ok\n---\nbody\n",
	}
	for name, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestParseDescriptionCap(t *testing.T) {
	long := strings.Repeat("x", 1025)
	raw := "---\nname: ok\ndescription: " + long + "\n---\nbody\n"
	if _, err := Parse(raw); err == nil {
		t.Fatal("over-long description should fail")
	}
	ok := "---\nname: ok\ndescription: " + strings.Repeat("x", 1024) + "\n---\nbody\n"
	if _, err := Parse(ok); err != nil {
		t.Fatalf("1024-char description should pass: %v", err)
	}
}

func TestValidName(t *testing.T) {
	for _, good := range []string{"a", "pdf", "my-skill-2"} {
		if err := ValidName(good); err != nil {
			t.Fatalf("%q should be valid: %v", good, err)
		}
	}
	for _, bad := range []string{"", "Bad", "under_score", "-leading", "trailing-", "double--hyphen"} {
		if err := ValidName(bad); err == nil {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}

func TestMatchDir(t *testing.T) {
	if err := MatchDir("pdf", "pdf"); err != nil {
		t.Fatal(err)
	}
	if err := MatchDir("pdf", "other"); err == nil {
		t.Fatal("mismatched dir should fail")
	}
}
