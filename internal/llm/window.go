package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

// modelWindows fetches {base}/models and reads each entry's context_length.
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(w.base, "/")+"/models", nil)
	if err != nil {
		return nil, nil, nil, err
	}
	if w.key != "" {
		req.Header.Set("Authorization", "Bearer "+w.key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("list models: http %d", res.StatusCode)
	}
	var body struct {
		Data []struct {
			ID                  string   `json:"id"`
			ContextLength       int      `json:"context_length"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, nil, nil, fmt.Errorf("list models: %w", err)
	}
	sizes = make(map[string]int, len(body.Data))
	reasoning, known = map[string]bool{}, make(map[string]bool, len(body.Data))
	for _, m := range body.Data {
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
