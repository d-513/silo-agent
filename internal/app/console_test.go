package app

import (
	"testing"

	"github.com/coder/websocket"
)

func TestParseConsoleWS(t *testing.T) {
	resize, ok := parseConsoleWS(websocket.MessageText, []byte(`{"rows":24,"cols":80}`))
	if !ok || resize.Rows != 24 || resize.Cols != 80 || len(resize.Data) != 0 {
		t.Fatalf("resize %+v ok=%v", resize, ok)
	}
	key, ok := parseConsoleWS(websocket.MessageText, []byte("pwd\n"))
	if !ok || string(key.Data) != "pwd\n" || key.Rows != 0 {
		t.Fatalf("keystrokes dropped: %+v ok=%v", key, ok)
	}
	bin, ok := parseConsoleWS(websocket.MessageBinary, []byte{0x03})
	if !ok || len(bin.Data) != 1 || bin.Data[0] != 0x03 {
		t.Fatalf("binary %+v ok=%v", bin, ok)
	}
	if _, ok := parseConsoleWS(websocket.MessageBinary, nil); ok {
		t.Fatal("empty")
	}
}
