package toolsgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	v1 "silo.agent/gen/silo/v1"
)

func TestSlug(t *testing.T) {
	if Slug("Wolfram Alpha") != "wolfram_alpha" {
		t.Fatal(Slug("Wolfram Alpha"))
	}
	if Slug("GitHub 2") != "github_2" {
		t.Fatal(Slug("GitHub 2"))
	}
}

func TestPyName(t *testing.T) {
	if PyName("WolframAlpha") != "wolfram_alpha" {
		t.Fatal(PyName("WolframAlpha"))
	}
	if PyName("return") != "return_" {
		t.Fatal(PyName("return"))
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	err := Write(dir, []*v1.ToolStub{{
		Connector:      "Wolfram",
		Action:         "WolframAlpha",
		Description:    "Ask Wolfram|Alpha",
		ArgsSchemaJson: `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "wolfram", "wolfram_alpha.py")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "def wolfram_alpha(*, query: str)") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "Ask Wolfram|Alpha") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "query (str)") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "if v is not None") {
		t.Fatal(s)
	}
	if !strings.Contains(s, `call("wolfram", "WolframAlpha"`) {
		t.Fatal(s)
	}
}

// Cloudflare's get_organizations description ends with a quote; unescaped it
// yields `""""` and the SyntaxError kills the whole package import.
func TestWriteQuoteHeavyDescriptionCompiles(t *testing.T) {
	dir := t.TempDir()
	err := Write(dir, []*v1.ToolStub{{
		Connector:   "Cloudflare",
		Action:      "get_organizations",
		Description: `Returns null when an organization has no parent (i.e. it is a 'root' organization)."` + ` Also """triple""" and a back\slash`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "cloudflare", "get_organizations.py"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), `""""`) {
		t.Fatalf("unescaped quote run:\n%s", src)
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	if out, err := exec.Command(py, "-c", "import ast,sys; ast.parse(open(sys.argv[1]).read())", filepath.Join(dir, "cloudflare", "get_organizations.py")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestWriteArrayAndEnum(t *testing.T) {
	dir := t.TempDir()
	err := Write(dir, []*v1.ToolStub{{
		Connector:   "Twilio Docs",
		Action:      "twilio__retrieve",
		Description: "Fetch docs by id",
		ArgsSchemaJson: `{
			"type":"object",
			"properties":{
				"ids":{"type":"array","description":"Document ids; prefix op::"},
				"product":{"type":"string","enum":["messaging","voice"],"description":"Product filter"}
			},
			"required":["ids"]
		}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "twilio_docs", "twilio__retrieve.py"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "ids: list") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "product: str | None = None") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "one of: messaging, voice") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "prefix op::") {
		t.Fatal(s)
	}
}
