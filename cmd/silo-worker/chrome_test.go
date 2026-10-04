package main

import (
	"context"
	"net"
	"testing"
)

func TestEnsureChromeAlreadyUp(t *testing.T) {
	ln, err := net.Listen("tcp", cdpAddr)
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	w := &worker{workspace: t.TempDir()}
	st, err := w.ensureChrome(context.Background())
	if err != nil || st != "up" {
		t.Fatalf("%s %v", st, err)
	}
}
