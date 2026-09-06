package main

import (
	"context"
	"net"
	"testing"
)

func TestWantsPlaywright(t *testing.T) {
	if !wantsPlaywright("from silo_runtime import chrome_page\npage = chrome_page()") {
		t.Fatal("chrome_page")
	}
	if !wantsPlaywright("from playwright.sync_api import sync_playwright") {
		t.Fatal("playwright")
	}
	if wantsPlaywright("print(2+2)") {
		t.Fatal("plain python")
	}
}

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
