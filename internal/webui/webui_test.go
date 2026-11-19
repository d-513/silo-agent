package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestConsoleIsTheFallbackForPagesOnly(t *testing.T) {
	h := New(fstest.MapFS{
		"index.html":           {Data: []byte("<html>app</html>")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"vad/model.onnx":       {Data: []byte("onnx")},
	})

	// Every page address is the app, never cached.
	for _, p := range []string{"/", "/index.html", "/bots/abc/rules", "/w/space/drives/new", "/signin"} {
		rec := get(t, h, http.MethodGet, p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d %q cache=%q", p, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: content type %q", p, ct)
		}
	}

	// A hashed file is itself, kept for good; other files for an hour.
	rec := get(t, h, http.MethodGet, "/assets/app-abc123.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
	if rec := get(t, h, http.MethodGet, "/favicon.svg"); rec.Code != 200 || strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("icon: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get(t, h, http.MethodGet, "/vad/model.onnx"); rec.Code != 200 || rec.Body.String() != "onnx" {
		t.Fatalf("vad file: %d", rec.Code)
	}

	// What is not a page is an error, never the app's HTML.
	for _, p := range []string{
		"/assets/gone-old999.js", // last release's file
		"/silo.v1.Nope/Method",   // an API path no route took
		"/oauth/unknown", "/auth/x", "/voice/x", "/vad/missing.wasm",
		"/robots.txt", "/bots/abc/file.map",
	} {
		if rec := get(t, h, http.MethodGet, p); rec.Code != 404 || strings.Contains(rec.Body.String(), "app") {
			t.Fatalf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get(t, h, http.MethodPost, "/bots/abc"); rec.Code != 405 {
		t.Fatalf("POST: %d", rec.Code)
	}
	if rec := get(t, h, http.MethodHead, "/bots/abc"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %d bytes", rec.Code, rec.Body.Len())
	}
	// A path cannot climb out of the bundle.
	if rec := get(t, h, http.MethodGet, "/../../etc/passwd"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("traversal: %d %q", rec.Code, rec.Body.String())
	}
}

func TestDevBuildHasNoConsole(t *testing.T) {
	if Embedded() || Handler() != nil {
		t.Skip("built with the webui tag")
	}
}
