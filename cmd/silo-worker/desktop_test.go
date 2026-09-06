package main

import (
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
	if err := validKey("ctrl+l"); err != nil {
		t.Fatal(err)
	}
	if validKey("ctrl;l") == nil || validKey("") == nil {
		t.Fatal("bad key")
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
