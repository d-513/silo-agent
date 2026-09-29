package dummy

import (
	"errors"
	"strings"

	"silo.agent/internal/llm"
)

// CollectMarker is the heading the CP's memory collector prompt starts with; a
// Complete whose system prompt carries it answers with a scripted collection.
const CollectMarker = "MEMORY COLLECTOR"

// CollectError is a Collect reply that fails the call like a provider error.
const CollectError = "!error"

var (
	collects     = map[string]string{}
	collectCalls []string
)

// Collect registers the collector's raw reply for requests whose excerpt holds
// token. Unmatched requests answer {} (nothing to save).
func Collect(token, reply string) {
	mu.Lock()
	defer mu.Unlock()
	collects[token] = reply
}

// CollectCalls returns the user text of every collector request so far.
func CollectCalls() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), collectCalls...)
}

func collectFrom(req llm.Request) (llm.Response, bool, error) {
	is := false
	for _, blk := range req.System {
		if strings.Contains(blk.Text, CollectMarker) {
			is = true
		}
	}
	if !is {
		return llm.Response{}, false, nil
	}
	hay := ""
	for _, m := range req.Messages {
		hay += m.Text + "\n"
	}
	// Match only what the human said: the dummy chat model echoes an older
	// token in later assistant turns.
	var said strings.Builder
	for _, entry := range strings.Split(hay, "\n\n") {
		if strings.HasPrefix(entry, "USER:") {
			said.WriteString(entry)
		}
	}
	mu.Lock()
	collectCalls = append(collectCalls, hay)
	reply := "{}"
	for token, r := range collects {
		if strings.Contains(said.String(), token) {
			reply = r
		}
	}
	mu.Unlock()
	if reply == CollectError {
		return llm.Response{}, true, errors.New("dummy: upstream unavailable")
	}
	return llm.Response{Text: reply, Usage: llm.Usage{InputTokens: requestRunes(req), OutputTokens: 8}}, true, nil
}
