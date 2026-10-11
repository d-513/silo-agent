package connector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every mark the catalog ships must still read as the image it is, or the
// library would lose its icons to the upload check.
func TestCatalogImagesAreImages(t *testing.T) {
	dir := filepath.Join("..", "..", "catalog", "images")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		t.Fatal("no catalog images found")
	}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		claimed := ""
		if strings.HasSuffix(e.Name(), ".svg") {
			claimed = "image/svg+xml"
		}
		if got := imageType(b, claimed); got == "" {
			t.Errorf("%s is not read as an image", e.Name())
		}
	}
}

func TestImageTypeIgnoresTheClaim(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`)
	for _, c := range []struct {
		name    string
		b       []byte
		claimed string
		want    string
	}{
		{"png under a false name", png, "text/html", "image/png"},
		{"svg", svg, "image/svg+xml", "image/svg+xml"},
		{"svg with no type", svg, "", "image/svg+xml"},
		{"svg claimed as html", svg, "text/html", ""},
		{"html", []byte("<!doctype html><script>alert(1)</script>"), "image/png", ""},
		{"html holding an svg", []byte("<html><svg></svg></html>"), "image/svg+xml", ""},
		{"script", []byte("alert(1)"), "image/svg+xml", ""},
	} {
		if got := imageType(c.b, c.claimed); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
