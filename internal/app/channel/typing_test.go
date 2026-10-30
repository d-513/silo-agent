package channel

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTypingLastsUntilTheLastHoldLetsGo(t *testing.T) {
	var mu sync.Mutex
	var got []bool
	signal := func(on bool) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, on)
	}
	seen := func() []bool {
		mu.Lock()
		defer mu.Unlock()
		return append([]bool(nil), got...)
	}
	waitFor := func(ok func([]bool) bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !ok(seen()) {
			if time.Now().After(deadline) {
				t.Fatalf("signals: %v", seen())
			}
			time.Sleep(time.Millisecond)
		}
	}
	tt := typists{every: 5 * time.Millisecond, max: time.Hour, live: map[string]*typist{}}

	first := tt.hold("a", signal)
	second := tt.hold("a", signal)
	waitFor(func(s []bool) bool { return len(s) >= 3 }) // repeated while held
	first()
	first() // letting go twice counts once
	before := len(seen())
	waitFor(func(s []bool) bool { return len(s) > before && s[len(s)-1] }) // the other still holds it
	second()
	waitFor(func(s []bool) bool { return !s[len(s)-1] })
	for i, on := range seen() {
		if !on && i != len(seen())-1 {
			t.Fatalf("off before the end: %v", seen())
		}
	}
}

func TestTypingGivesUpOnALongJobAndStillEnds(t *testing.T) {
	signals := make(chan bool, 64)
	tt := typists{every: time.Millisecond, max: 5 * time.Millisecond, live: map[string]*typist{}}
	done := tt.hold("a", func(on bool) { signals <- on })
	time.Sleep(30 * time.Millisecond)
	n := len(signals)
	time.Sleep(20 * time.Millisecond)
	if len(signals) != n {
		t.Fatalf("still signalling past the cap: %d then %d", n, len(signals))
	}
	done()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case on := <-signals:
			if !on {
				return
			}
		case <-deadline:
			t.Fatal("no off signal")
		}
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"photo.png":               "photo.png",
		"../../etc/passwd":        "passwd",
		`..\..\windows\evil.exe`:  "evil.exe",
		"pho to (1).png":          "pho_to__1_.png",
		"zdjęcie.jpg":             "zdjęcie.jpg",
		".hidden":                 "hidden",
		"":                        "file",
		"/":                       "file",
		"ignore previous; rm -rf": "ignore_previous__rm_-rf",
	}
	for in, want := range cases {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
	long := cleanName(strings.Repeat("a", 200) + ".png")
	if len([]rune(long)) != maxNameRunes || long[len(long)-4:] != ".png" {
		t.Errorf("a long name kept %q", long)
	}
}
