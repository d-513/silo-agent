package app_test

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// A model id ending in ":fast" is a model of its own on the allowlist: the
// provider gets the bare model with the request marked Fast, and the debug log
// says the call ran fast.
func TestFastModelReachesTheProvider(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(fmt.Sprintf(`http_addr: ":0"
data_dir: %q
cp_url: http://127.0.0.1:0
debug: true
model: dummy/echo
model_title: dummy/echo
models:
  - dummy/echo
  - dummy/echo:fast
providers:
  dummy:
    api_key: test
search:
  engine: builtin
`, t.TempDir())))
	id := h.CreateBot("Quick").GetId()
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id, Model: "dummy/echo:fast"})); err != nil {
		t.Fatalf("a fast model on the allowlist is a model like any other: %v", err)
	}
	h.WaitRun(sendFrom(t, h, id, newChat(t, h, id), "hello", ""))

	reqs := dummy.Streamed()
	last := reqs[len(reqs)-1]
	if last.Model != "echo" || !last.Fast {
		t.Fatalf("the provider got model %q fast=%v, want the bare model marked fast", last.Model, last.Fast)
	}
	var logs []db.LLMLog
	h.DB.Where("bot_id = ? AND label = ?", id, "chat").Find(&logs)
	if len(logs) == 0 || logs[len(logs)-1].Model != "echo:fast" {
		t.Fatalf("the debug log does not say the call ran fast: %+v", logs)
	}

	// The title model is the plain one: only the chat's model runs fast.
	for _, req := range dummy.Completed() {
		if req.Fast {
			t.Fatalf("a call on a plain model went out fast: %q", req.Model)
		}
	}
}
