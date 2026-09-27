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

func (w *modelWindows) lookup(ctx context.Context, model string) (int, error) {
	windowCache.Lock()
	defer windowCache.Unlock()
	l := windowCache.lists[w.base]
	fresh := l != nil && ((l.err == nil && time.Since(l.at) < windowTTL) || (l.err != nil && time.Since(l.at) < windowRetry))
	if !fresh {
		sizes, err := w.fetch(ctx)
		l = &windowList{at: time.Now(), err: err, sizes: sizes}
		windowCache.lists[w.base] = l
	}
	if l.err != nil {
		return 0, l.err
	}
	if n := l.sizes[model]; n > 0 {
		return n, nil
	}
	return 0, fmt.Errorf("no context length listed for %q", model)
}

func (w *modelWindows) fetch(ctx context.Context) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(w.base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if w.key != "" {
		req.Header.Set("Authorization", "Bearer "+w.key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: http %d", res.StatusCode)
	}
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	out := make(map[string]int, len(body.Data))
	for _, m := range body.Data {
		if m.ContextLength > 0 {
			out[m.ID] = m.ContextLength
		}
	}
	return out, nil
}
