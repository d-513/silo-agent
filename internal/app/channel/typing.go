package channel

import (
	"context"
	"sync"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const (
	// typingEvery is under the shortest time a platform shows one signal
	// (Telegram, about 5 s).
	typingEvery = 4 * time.Second
	// typingMax ends the signals of a long job: past this the Bot's sections
	// say how it is going, and a linked WhatsApp account should not look like
	// it has been typing for two hours.
	typingMax = 10 * time.Minute
	// typingTimeout bounds one signal, so a slow platform delays only the next.
	typingTimeout = 5 * time.Second
)

// typists keeps one "typing…" going per conversation for as long as anyone
// holds it: a run, or a voice memo being heard before the run starts.
type typists struct {
	every, max time.Duration
	mu         sync.Mutex
	live       map[string]*typist
}

type typist struct {
	holds int
	stop  chan struct{}
}

// hold starts the signal if it is not going, and returns the call that lets go
// of it. The last to let go ends it.
func (t *typists) hold(key string, signal func(on bool)) (done func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.live[key]
	if e == nil {
		e = &typist{stop: make(chan struct{})}
		t.live[key] = e
		go e.run(t.every, t.max, signal)
	}
	e.holds++
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if e.holds--; e.holds == 0 {
				delete(t.live, key)
				close(e.stop)
			}
		})
	}
}

func (e *typist) run(every, max time.Duration, signal func(on bool)) {
	signal(true)
	tick := time.NewTicker(every)
	defer tick.Stop()
	for until := time.Now().Add(max); ; {
		select {
		case <-e.stop:
			signal(false)
			return
		case <-tick.C:
			if time.Now().Before(until) {
				signal(true)
			}
		}
	}
}

// showTyping shows the Bot typing in a conversation until the call it returns,
// on a platform that can.
func (s *Service) showTyping(ch *db.Channel, external string) (done func()) {
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return func() {}
	}
	typer, ok := ad.(channels.Typer)
	if !ok {
		return func() {}
	}
	return s.typing.hold(ch.ID+"/"+external, func(on bool) {
		ctx, cancel := context.WithTimeout(context.Background(), typingTimeout)
		defer cancel()
		_ = typer.Typing(ctx, ch, external, on)
	})
}
