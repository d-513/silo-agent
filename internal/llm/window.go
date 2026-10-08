package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var errNoWindow = errors.New("provider does not report context windows")

// windowTTL is how long a fetched model list is trusted; windowRetry is how
// long a failed fetch is remembered so a down gateway is not hit every turn.
const (
	windowTTL   = 6 * time.Hour
	windowRetry = time.Minute
)

type windowList struct {
	at    time.Time
	err   error
	sizes map[string]int
	// reasoning is the set of models whose supported_parameters lists
	// "reasoning" (OpenRouter's unified reasoning control).
	reasoning map[string]bool
	known     map[string]bool
}

// windowCache holds one model list per base URL, shared by every client the
// CP builds (a client is built per run).
var windowCache = struct {
	sync.Mutex
	lists map[string]*windowList
}{lists: map[string]*windowList{}}

// modelWindows reads each entry's context_length from {base}/models.
type modelWindows struct {
	base string
	key  string
}

// list returns the cached model list, fetching it when stale.
func (w *modelWindows) list(ctx context.Context) (*windowList, error) {
	windowCache.Lock()
	defer windowCache.Unlock()
	l := windowCache.lists[w.base]
	fresh := l != nil && ((l.err == nil && time.Since(l.at) < windowTTL) || (l.err != nil && time.Since(l.at) < windowRetry))
	if !fresh {
		l = w.fetch(ctx)
		l.at = time.Now()
		windowCache.lists[w.base] = l
	}
	return l, l.err
}

func (w *modelWindows) lookup(ctx context.Context, model string) (int, error) {
	l, err := w.list(ctx)
	if err != nil {
		return 0, err
	}
	if n := l.sizes[model]; n > 0 {
		return n, nil
	}
	return 0, fmt.Errorf("no context length listed for %q", model)
}

// reasons reports whether the gateway lists model as taking the reasoning
// parameter, and whether the model is listed at all.
func (w *modelWindows) reasons(ctx context.Context, model string) (reasoning, listed bool, err error) {
	l, err := w.list(ctx)
	if err != nil {
		return false, false, err
	}
	return l.reasoning[model], l.known[model], nil
}

func (w *modelWindows) fetch(ctx context.Context) *windowList {
	sizes, reasoning, known, err := w.get(ctx)
	return &windowList{err: err, sizes: sizes, reasoning: reasoning, known: known}
}

func (w *modelWindows) get(ctx context.Context) (sizes map[string]int, reasoning, known map[string]bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list, err := fetchModels(ctx, w.base, w.key)
	if err != nil {
		return nil, nil, nil, err
	}
	sizes = make(map[string]int, len(list))
	reasoning, known = map[string]bool{}, make(map[string]bool, len(list))
	for _, m := range list {
		known[m.ID] = true
		if m.ContextLength > 0 {
			sizes[m.ID] = m.ContextLength
		}
		for _, p := range m.SupportedParameters {
			if p == "reasoning" {
				reasoning[m.ID] = true
			}
		}
	}
	return sizes, reasoning, known, nil
}
