package main

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"silo.agent/internal/desktop"
)

func TestClickButton(t *testing.T) {
	b, err := clickButton("")
	if err != nil || b != "left" {
		t.Fatalf("%s %v", b, err)
	}
	if _, err := clickButton("middle"); err == nil {
		t.Fatal("middle")
	}
}

func TestValidKey(t *testing.T) {
	got, err := normalizeKey("Return")
	if err != nil || got != "Return" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = normalizeKey("ctrl+l")
	if err != nil || got != "ctrl+l" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = normalizeKey("ctrl-shift-t")
	if err != nil || got != "ctrl+shift+t" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = normalizeKey("alt+Tab")
	if err != nil || got != "alt+Tab" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = normalizeKey("Enter")
	if err != nil || got != "Return" {
		t.Fatalf("%s %v", got, err)
	}
	for _, bad := range []string{"ctrl;l", ""} {
		if _, err := normalizeKey(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestLooksLikeChord(t *testing.T) {
	if !looksLikeChord("ctrl+l") || !looksLikeChord("Ctrl-L") || looksLikeChord("hello") {
		t.Fatal("chord")
	}
}

func TestIntArg(t *testing.T) {
	m := map[string]any{"x": float64(12), "y": "7"}
	if intArg(m, "x") != 12 || intArg(m, "y") != 7 || intArg(m, "z") != 0 {
		t.Fatal(m)
	}
}

func TestRunDesktopUnknown(t *testing.T) {
	w := &worker{}
	if _, err := w.runDesktop(context.Background(), "wave", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestImageSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "screen.jpg")
	img := image.NewRGBA(image.Rect(0, 0, desktop.Width, desktop.Height))
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	w, h, err := imageSize(p)
	if err != nil || w != desktop.Width || h != desktop.Height {
		t.Fatalf("%dx%d %v", w, h, err)
	}
}
