package channel

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// What the owner decides about a channel whichever adapter it is: the model
// its conversations use, and who in the conversation the Bot answers. They are
// ordinary config values under reserved keys, so every adapter's form draws
// them and no adapter reads them.
const (
	keyModel     = "model"
	keyAllowFrom = "allow_from"
)

// hostFields are the fields every adapter's form ends with.
func (s *Service) hostFields() []channels.Field {
	model := channels.Field{
		Key: keyModel, Label: "Model", Type: channels.FieldSelect,
		Description: "The model this channel's conversations use. Empty is the Bot's own.",
		Options:     []channels.Option{{Value: "", Label: "The Bot's model"}},
	}
	for _, id := range s.cfg().Models {
		model.Options = append(model.Options, channels.Option{Value: id, Label: id})
	}
	return []channels.Field{
		model,
		{
			Key: keyAllowFrom, Label: "Only answer", Type: channels.FieldTextarea,
			Description: "One person per line, by the id or the name the platform gives them. Empty answers everyone in the conversation. A message from anyone else is ignored and noted in the Bot's Audit, with the sender's id. Prefer ids: on most platforms anyone can change their display name.",
		},
	}
}

// fieldsOf is everything a channel's config may hold: the adapter's own
// fields, then the host's.
func (s *Service) fieldsOf(d channels.Descriptor) []channels.Field {
	return append(append([]channels.Field(nil), d.Fields...), s.hostFields()...)
}

// checkHostConfig refuses a host value that cannot be honoured.
func (s *Service) checkHostConfig(values map[string]string) error {
	if m := strings.TrimSpace(values[keyModel]); m != "" && !s.modelAllowed(m) {
		return fmt.Errorf("%s is not an allowed model", m)
	}
	return nil
}

func (s *Service) modelAllowed(id string) bool {
	for _, m := range s.cfg().Models {
		if m == id {
			return true
		}
	}
	return false
}

// channelModel is the model a channel's conversations use: its setting while
// the operator still allows it, else "" (the Bot's own).
func (s *Service) channelModel(ch *db.Channel) string {
	m := strings.TrimSpace(nonSecretConfig(ch)[keyModel])
	if m == "" || !s.modelAllowed(m) {
		return ""
	}
	return m
}

// allowList is a channel's "Only answer" list, one entry per line or comma.
func allowList(ch *db.Channel) []string {
	raw := nonSecretConfig(ch)[keyAllowFrom]
	var out []string
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
		if item = person(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// person is a name or id as the list compares it: trimmed, any case, with no
// leading @.
func person(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@"))
}

// answers reports whether the channel answers a message from this sender: yes
// with no list, else only someone the list names by id or by name.
func answers(list []string, in channels.Inbound) bool {
	if len(list) == 0 {
		return true
	}
	id, name := person(in.AuthorID), person(in.Author)
	for _, item := range list {
		if (id != "" && item == id) || (name != "" && item == name) {
			return true
		}
	}
	return false
}

// refusedEvery is how often one sender's ignored messages are noted.
var refusedEvery = time.Hour

// refusals remembers which senders were noted lately, so a busy group does not
// fill the Audit with one line per message.
type refusals struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (r *refusals) due(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if at, ok := r.last[key]; ok && now.Sub(at) < refusedEvery {
		return false
	}
	if r.last == nil {
		r.last = map[string]time.Time{}
	}
	if len(r.last) > 4096 {
		for k, at := range r.last {
			if now.Sub(at) >= refusedEvery {
				delete(r.last, k)
			}
		}
	}
	r.last[key] = now
	return true
}

// refuse notes a message that was not answered because its sender is not on
// the channel's list. The line names the sender's id, which is what the owner
// adds to the list to let them in.
func (s *Service) refuse(ch *db.Channel, in channels.Inbound) {
	who := strings.TrimSpace(in.Author)
	if who == "" {
		who = "someone"
	}
	if id := strings.TrimSpace(in.AuthorID); id != "" {
		who += " (id " + id + ")"
	}
	if !s.refused.due(ch.ID+"/"+person(in.AuthorID)+"/"+person(in.Author), time.Now()) {
		return
	}
	if s.Refused != nil {
		s.Refused(ch, who)
	}
}
