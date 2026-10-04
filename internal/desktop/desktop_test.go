package desktop

import "testing"

func TestCheckPoint(t *testing.T) {
	for _, p := range [][2]int{{0, 0}, {Width - 1, Height - 1}} {
		if err := CheckPoint(p[0], p[1]); err != nil {
			t.Fatalf("%v: %v", p, err)
		}
	}
	for _, p := range [][2]int{{Width, 0}, {-1, 10}, {0, Height}, {0, -1}} {
		if CheckPoint(p[0], p[1]) == nil {
			t.Fatalf("%v accepted", p)
		}
	}
}

func TestWantsChromium(t *testing.T) {
	if !WantsChromium("from silo_runtime import chrome_page\npage = chrome_page()") {
		t.Fatal("chrome_page")
	}
	if !WantsChromium("from playwright.sync_api import sync_playwright") {
		t.Fatal("playwright")
	}
	if !WantsChromium("pw.chromium.CONNECT_OVER_CDP") {
		t.Fatal("connect_over_cdp is case-insensitive")
	}
	if WantsChromium("print(2+2)") {
		t.Fatal("plain python")
	}
}
