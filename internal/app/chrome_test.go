package app

import "testing"

func TestWantsChrome(t *testing.T) {
	if !wantsChrome("from playwright.sync_api import sync_playwright") {
		t.Fatal("playwright")
	}
	if !wantsChrome("from silo_runtime import chrome_page\npage = chrome_page()") {
		t.Fatal("chrome_page")
	}
	if wantsChrome("print(2+2)") {
		t.Fatal("plain python")
	}
}
