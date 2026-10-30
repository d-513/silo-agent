package voice

import (
	"context"
	"testing"
	"time"
)

func TestAudioMIME(t *testing.T) {
	for name, want := range map[string]string{
		"a.MP3": "audio/mpeg", "b.wav": "audio/wav", "c.m4a": "audio/mp4", "d.opus": "audio/ogg",
		"e.webm": "audio/webm", "f.flac": "audio/flac", "g.txt": "", "noext": "",
	} {
		if got := audioMIME(name); got != want {
			t.Errorf("audioMIME(%q) = %q, want %q", name, got, want)
		}
	}
}

// frames gives a piece n made frames.
func frames(p *piece, n int) {
	for range n {
		p.add([]byte{0})
	}
}

// A duck steps back over what was not played yet, so a piece resumes where the
// human stopped hearing it, without being announced a second time.
func TestDuckRewindsWithoutANewStart(t *testing.T) {
	o := newOutbox()
	o.say("one two three four", "run")
	p := o.queue[0]
	frames(p, 100)
	if _, start, _ := o.take(); start != p {
		t.Fatal("the first frame should announce the piece")
	}
	for range 2 {
		o.take()
	}
	o.duck(true)
	if p.sent != 0 {
		t.Fatalf("sent %d after the duck", p.sent)
	}
	o.duck(false)
	if _, start, _ := o.take(); start != nil {
		t.Fatal("a resumed piece was announced twice")
	}
}

// After a duck the frames sent are the ones heard: a flush does not take the
// browser's delay off a second time.
func TestFlushAfterADuckCountsWhatWasHeard(t *testing.T) {
	cut := func(ducked bool) string {
		o := newOutbox()
		o.say("one two three four five six seven eight nine ten", "run")
		p := o.queue[0]
		frames(p, 100)
		p.finish()
		for range 60 {
			o.take()
		}
		o.delay = 10
		if ducked {
			o.duck(true)
		}
		heard, playing, runs := o.flush()
		if !playing || len(runs) != 1 || runs[0] != "run" {
			t.Fatalf("playing %v, runs %v", playing, runs)
		}
		return heard
	}
	plain, ducked := cut(false), cut(true)
	if plain == "" || len(ducked) < len(plain)-len(" five") || len(ducked) > len(plain) {
		t.Fatalf("plain %q, ducked %q", plain, ducked)
	}
}

// A piece that never played was not heard, and is still reported as dropped.
func TestFlushOfAPieceNotPlayedYet(t *testing.T) {
	o := newOutbox()
	o.say("not yet", "run")
	heard, playing, runs := o.flush()
	if heard != "" || playing || len(runs) != 1 {
		t.Fatalf("heard %q, playing %v, runs %v", heard, playing, runs)
	}
}

// A flush stops the synthesis under way: the next sentence must not wait for
// words nobody will hear.
func TestFlushCancelsTheSynthesis(t *testing.T) {
	o := newOutbox()
	stopped := make(chan struct{})
	started := make(chan struct{})
	o.synth = func(ctx context.Context, p *piece) {
		close(started)
		<-ctx.Done()
		close(stopped)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.synthLoop(ctx)
	o.say("a long sentence", "run")
	<-started
	o.flush()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the synthesis kept going")
	}
}
