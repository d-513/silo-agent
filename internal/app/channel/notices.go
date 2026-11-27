package channel

import (
	"strings"
	"sync"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// A channel conversation only ever hears what the Bot writes. When a run stops
// with nothing written (it failed, or it waits on an approval in Silo) the
// person on the other side would hear nothing at all, so the channel says it:
// fixed text in the Bot's language, never a model's, and never the reason (an
// error can name a file, a key or an address, and a group reads the channel).

type notice int

const (
	noticeFailed notice = iota
	noticeApproval
)

var notices = map[string][]string{
	"en": {
		noticeFailed:   "Something went wrong and I could not finish. The details are in Silo.",
		noticeApproval: "I need an approval in Silo before I can go on with this.",
	},
	"pl": {
		noticeFailed:   "Coś poszło nie tak i nie udało mi się dokończyć. Szczegóły są w Silo.",
		noticeApproval: "Zanim pójdę dalej, potrzebuję zgody w Silo.",
	},
	"de": {
		noticeFailed:   "Etwas ist schiefgegangen, ich konnte das nicht abschließen. Die Details stehen in Silo.",
		noticeApproval: "Bevor ich weitermache, brauche ich eine Freigabe in Silo.",
	},
}

func (n notice) in(lang string) string {
	if l, ok := notices[strings.ToLower(strings.TrimSpace(lang))]; ok {
		return l[n]
	}
	return notices["en"][n]
}

// noticeEvery is how often one conversation is told the same thing: a run can
// stop on several approvals in a row, and each need not say so again.
var noticeEvery = time.Minute

// told remembers when a conversation last heard each notice.
type told struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (t *told) due(chatID string, n notice, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := chatID + "/" + string(rune('0'+int(n)))
	if at, ok := t.last[key]; ok && now.Sub(at) < noticeEvery {
		return false
	}
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	// Bounded: an entry is only worth keeping for noticeEvery.
	if len(t.last) > 4096 {
		for k, at := range t.last {
			if now.Sub(at) >= noticeEvery {
				delete(t.last, k)
			}
		}
	}
	t.last[key] = now
	return true
}

// RunEnded is told when a run ends in any chat; one in a channel conversation
// that failed says so there, since nothing else would.
func (s *Service) RunEnded(botID, chatID, st string) {
	if st != "error" {
		return
	}
	s.tell(botID, chatID, noticeFailed)
}

// NeedsApproval is told when a run stops on an approval a human answers in
// Silo; in a channel conversation the people there hear that it waits.
func (s *Service) NeedsApproval(botID, chatID string) {
	s.tell(botID, chatID, noticeApproval)
}

// tell delivers a notice to the channel conversation chatID is, if it is one.
// It runs off the caller's path: a send is a network call, and neither a run
// ending nor an approval being raised waits for it.
func (s *Service) tell(botID, chatID string, n notice) {
	if s == nil || s.db == nil || chatID == "" {
		return
	}
	var c db.Chat
	s.db.Select("id, bot_id, channel_id, external_id").Where("id = ? AND bot_id = ?", chatID, botID).Limit(1).Find(&c)
	if c.ID == "" || c.ChannelID == "" || c.ExternalID == "" {
		return
	}
	var ch db.Channel
	s.db.Where("id = ? AND bot_id = ?", c.ChannelID, botID).Limit(1).Find(&ch)
	if ch.ID == "" || !ch.Enabled {
		return
	}
	if !s.told.due(chatID, n, time.Now()) {
		return
	}
	var bot db.Bot
	s.db.Select("id, language").Where("id = ?", botID).Limit(1).Find(&bot)
	text := n.in(bot.Language)
	go func() {
		if err := s.Origin(&ch, c.ExternalID).Deliver(channels.Outbound{Text: text}); err != nil {
			s.logf("channel %s: notice: %v", ch.ID, err)
		}
	}()
}
