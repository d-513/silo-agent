//go:build webui

package webui

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
)

// The bundle `make web-build` made is in the binary whole: the page, its
// hashed files, and the voice detector's files a call loads from /vad/.
func TestBundleIsEmbedded(t *testing.T) {
	if !Embedded() {
		t.Fatal("built with the webui tag but no bundle")
	}
	page, err := fs.ReadFile(bundle, "index.html")
	if err != nil || !strings.Contains(string(page), "/assets/") {
		t.Fatalf("index.html: %v", err)
	}
	for _, dir := range []string{"assets", "vad"} {
		if list, err := fs.ReadDir(bundle, dir); err != nil || len(list) == 0 {
			t.Fatalf("%s: %v (%d files)", dir, err, len(list))
		}
	}
	h := Handler()
	if rec := get(t, h, http.MethodGet, "/bots/x/rules"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "/assets/") {
		t.Fatalf("a page address: %d", rec.Code)
	}
	if rec := get(t, h, http.MethodGet, "/vad/silero_vad_v5.onnx"); rec.Code != 200 || rec.Body.Len() < 1000 {
		t.Fatalf("the voice model: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}
