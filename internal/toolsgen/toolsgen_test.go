package toolsgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "silo.agent/gen/silo/v1"
)

func TestSlug(t *testing.T) {
	if Slug("Wolfram Alpha") != "wolfram_alpha" {
		t.Fatal(Slug("Wolfram Alpha"))
	}
	if Slug("123") != "c_123" {
		t.Fatal(Slug("123"))
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
