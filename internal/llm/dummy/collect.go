package dummy

import (
	"strings"

	"silo.agent/internal/llm"
)

// CollectMarker is the heading the CP's memory keeper prompt starts with: a
// request whose system prompt carries it is a keeper run's turn, and is
// answered from the turns Collect registered instead of the chat's script.
const CollectMarker = "MEMORY KEEPER"

// CollectError fails a keeper turn like a provider error.
var CollectError = Turn{Fail: "dummy: upstream unavailable"}

// keeperScript is the model-like suffix a keeper run's scripts are filed
// under, so one token can script the chat and the keeper that reads it.
const keeperScript = "@keeper"

var collectCalls []string

// Collect registers the keeper's turns for a run whose opening holds token
// (in what the human said, when it is reading a conversation). A run nothing
// is registered for answers with one line and no tool call: nothing to keep.
func Collect(token string, turns ...Turn) {
	Script(token+keeperScript, turns...)
}

// Remember is a keeper (or chat) turn that calls remember with args, a JSON
// object.
func Remember(args string) Turn { return Call("remember", args) }

// Call is a turn that makes one tool call.
func Call(name, args string) Turn {
	return Turn{ToolCalls: []llm.ToolCall{{Name: name, Arguments: args}}}
}

// CollectCalls returns what every keeper run so far opened with.
func CollectCalls() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), collectCalls...)
}

func isKeeper(req llm.Request) bool {
	for _, blk := range req.System {
		if strings.Contains(blk.Text, CollectMarker) {
			return true
		}
	}
	return false
}

// keeperTurn picks a keeper run's next turn: the script of the first token
// the human said in the conversation it reads, else of the first token in
// its opening at all (a tidy run has no conversation).
func keeperTurn(req llm.Request) Turn {
	opening, index := "", 0
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleUser:
			if opening == "" {
				opening = m.Text
			}
		case llm.RoleAssistant:
			index++
		}
	}
	// Match only what the human said: the dummy chat model echoes an older
	// token in later assistant turns.
	var said strings.Builder
	for _, entry := range strings.Split(opening, "\n\n") {
		if strings.HasPrefix(entry, "USER:") {
			said.WriteString(entry)
		}
	}
	token := firstToken(said.String())
	if token == "" {
		token = firstToken(opening)
	}
	mu.Lock()
	if index == 0 {
		collectCalls = append(collectCalls, opening)
	}
	turns := scenarios[token+keeperScript]
	mu.Unlock()
	if index < len(turns) {
		return turns[index]
	}
	return Turn{Text: "Nothing more to keep."}
}
