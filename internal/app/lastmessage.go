package app

import (
	"strings"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// lastSaid is the newest message of a Bot as one person sees it: the line a
// list of Bots shows under its name, and what that list is sorted by.
type lastSaid struct {
	Text string
	At   time.Time
	// Mine: the viewer wrote it.
	Mine bool
}

// snippetMax caps the line: a list row shows far less, the rest is for a
// wider one.
const snippetMax = 160

// lastMessage is the newest thing said in the conversations userID reads on
// the Bot in Silo: their own chats and, for a manager, the Bot's own (a
// channel, a mail wake). An automation's or a subagent's log is work, not a
// conversation, someone else's chat is theirs, and a seat in a room is the
// room's (its row has the room's own last line). A message is what a
// person typed or the Bot's reply; a brief, a quoted mail and the interview's
// opening turn (user events with a sender tag) are not.
//
// It looks only at the few chats that moved last and their newest runs, so
// it stays three small indexed reads however long the history is.
func (a *App) lastMessage(botID, userID string, manager bool) lastSaid {
	q := a.DB.Model(&db.Chat{}).Where("bot_id = ? AND COALESCE(automation_id, '') = '' AND COALESCE(subagent_id, '') = '' AND COALESCE(room_id, '') = ''", botID)
	if manager {
		q = q.Where("user_id IN ?", []string{userID, ""})
	} else {
		q = q.Where("user_id = ?", userID)
	}
	var chatIDs []string
	q.Order("updated_at desc").Limit(3).Pluck("id", &chatIDs)
	if len(chatIDs) == 0 {
		return lastSaid{}
	}
	var runIDs []string
	a.DB.Model(&db.Run{}).Where("chat_id IN ?", chatIDs).Order("created_at desc").Limit(6).Pluck("id", &runIDs)
	if len(runIDs) == 0 {
		return lastSaid{}
	}
	var ev db.RunEvent
	// The body is cut in SQL: a message can be megabytes, and the line is not.
	a.DB.Model(&db.RunEvent{}).Select("kind, left(body, 800) AS body, user_id, created_at").
		Where("run_id IN ? AND ((kind = 'user' AND tool = '') OR kind IN ('assistant', 'section'))", runIDs).
		Where("btrim(body) <> ''").
		Order("seq desc").Limit(1).Find(&ev)
	text := snippet(ev.Body)
	if text == "" {
		return lastSaid{}
	}
	return lastSaid{Text: text, At: ev.CreatedAt, Mine: ev.Kind == "user" && userID != "" && ev.UserID == userID}
}

// unmark drops the markdown that would only be noise on one plain line.
var unmark = strings.NewReplacer("<section_send />", " ", "**", "", "__", "", "`", "")

// snippet is a message as one plain line: its whitespace folded, the
// markdown marks that open a line or wrap a word dropped, cut on a rune.
func snippet(s string) string {
	words := strings.Fields(unmark.Replace(s))
	for len(words) > 0 && strings.Trim(words[0], "#>-*") == "" {
		words = words[1:]
	}
	return textx.CapRunes(strings.Join(words, " "), snippetMax)
}
