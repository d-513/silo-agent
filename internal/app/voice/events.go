package voice

import (
	"encoding/json"
	"strings"

	v1 "silo.agent/gen/silo/v1"
)

// The events a call writes into its chat: where it began and ended. Replay
// has no case for them, so the model never reads them; spoken turns carry
// their own mark (run.Spoken).
const (
	CallStartKind = "call_start"
	CallEndKind   = "call_end"
)

// IsCallKind reports whether an event kind is one of a call's.
func IsCallKind(kind string) bool { return kind == CallStartKind || kind == CallEndKind }

// callMeta is the Meta of a call event.
type callMeta struct {
	CallID  string `json:"call_id"`
	Seconds int    `json:"seconds,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (m callMeta) encode() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// SpokenProto is a call event's mark for the thread.
func SpokenProto(meta string) *v1.Spoken {
	var m callMeta
	if json.Unmarshal([]byte(strings.TrimSpace(meta)), &m) != nil {
		return nil
	}
	return &v1.Spoken{CallId: m.CallID, Seconds: int32(m.Seconds)}
}
