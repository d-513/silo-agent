package main

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestScreenPoint(t *testing.T) {
	if err := screenPoint(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := screenPoint(1279, 719); err != nil {
		t.Fatal(err)
	}
	if screenPoint(1280, 0) == nil || screenPoint(-1, 10) == nil || screenPoint(0, 720) == nil {
		t.Fatal("expected out of range")
	}
}

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
	if err := validKey("Return"); err != nil {
		t.Fatal(err)
	}
	got, err := normalizeKey("ctrl+l")
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
	if validKey("ctrl;l") == nil || validKey("") == nil {
		t.Fatal("bad key")
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

func TestPNGSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "screen.png")
	img := image.NewRGBA(image.Rect(0, 0, screenW, screenH))
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	w, h, err := pngSize(p)
	if err != nil || w != screenW || h != screenH {
		t.Fatalf("%dx%d %v", w, h, err)
	}
}
