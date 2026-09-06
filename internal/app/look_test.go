package app

import (
	"context"
	"strings"
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

func TestClickOutOfRange(t *testing.T) {
	a := &App{}
	_, _, err := a.execTool(context.Background(), "b", "r", "click", `{"x":1280,"y":10}`)
	if err == nil || !strings.Contains(err.Error(), "1280") {
		t.Fatalf("%v", err)
	}
}

func TestLookAck(t *testing.T) {
	got := lookAck(`{"name":"screen.png","size":2048}`)
	if !strings.Contains(got, "1280×720") || !strings.Contains(got, "no scale") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "Scratch") && !strings.Contains(got, "scratch") {
		t.Fatal(got)
	}
}

func TestVisionImageLookOmitsHigh(t *testing.T) {
	_, part := visionImage("look", "data:image/png;base64,AA")
	if part.Detail == "high" {
		t.Fatal("look must not use detail high")
	}
	_, part = visionImage("present", "data:image/png;base64,AA")
	if part.Detail != "high" {
		t.Fatalf("present detail %q", part.Detail)
	}
}

func TestBotScratchScreen(t *testing.T) {
	if !botScratch(lookPath) {
		t.Fatal(lookPath)
	}
}
