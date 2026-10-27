package access

import "sync"

// Watch ends what a person holds open on a Bot (a run stream, the desktop
// and console sockets) when their access to it ends. Each long-lived handler
// asks Lost for a channel and stops when it closes; End closes them.
type Watch struct {
	mu   sync.Mutex
	next int
	subs map[[2]string]map[int]chan struct{}
}

// Lost is closed when the user's access to the Bot ends. Call stop when done.
func (w *Watch) Lost(botID, userID string) (lost <-chan struct{}, stop func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.subs == nil {
		w.subs = map[[2]string]map[int]chan struct{}{}
	}
	key := [2]string{botID, userID}
	if w.subs[key] == nil {
		w.subs[key] = map[int]chan struct{}{}
	}
	w.next++
	id, ch := w.next, make(chan struct{})
	w.subs[key][id] = ch
	return ch, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if m := w.subs[key]; m != nil {
			delete(m, id)
			if len(m) == 0 {
				delete(w.subs, key)
			}
		}
	}
}

// End closes everything the user holds open on the Bot.
func (w *Watch) End(botID, userID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := [2]string{botID, userID}
	for _, ch := range w.subs[key] {
		close(ch)
	}
	delete(w.subs, key)
}
