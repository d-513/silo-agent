package channel

import (
	"strings"
	"sync"
)

// replies remembers, per channel conversation, where the newest message asked
// to be answered (channels.Inbound.ReplyTo: a Slack thread). Everything a run in
// that conversation delivers goes there, a subagent's late report included,
// until a message names another place or none. It lives in memory: after a
// restart the next message sets it again, and until then answers go to the
// conversation itself.
type replies struct {
	mu sync.Mutex
	to map[string]string // channelID + "/" + external -> anchor
}

func replyKey(channelID, external string) string { return channelID + "/" + external }

func (r *replies) set(channelID, external, anchor string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if anchor = strings.TrimSpace(anchor); anchor == "" {
		delete(r.to, replyKey(channelID, external))
		return
	}
	r.to[replyKey(channelID, external)] = anchor
}

func (r *replies) get(channelID, external string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.to[replyKey(channelID, external)]
}

// drop forgets a deleted channel's conversations.
func (r *replies) drop(channelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.to {
		if strings.HasPrefix(k, channelID+"/") {
			delete(r.to, k)
		}
	}
}
