package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// Every Bot has one main chat: listed first-class, the default for a send
// with no chat, and never deleted or renamed.
func TestMainChat(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Homebody").GetId()

	list := webChats(t, h, id)
	if len(list) != 1 || !list[0].GetMain() || list[0].GetTitle() != "Main" {
		t.Fatalf("a new Bot's chats: %+v", list)
	}
	main := list[0].GetId()

	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, Text: "hello"}))
	if err != nil || res.Msg.GetChatId() != main {
		t.Fatalf("a send with no chat went to %q (%v), want the main chat", res.Msg.GetChatId(), err)
	}
	h.WaitRun(res.Msg.GetRunId())

	if _, err := h.Client.DeleteChat(h.Ctx(), connect.NewRequest(&v1.DeleteChatRequest{BotId: id, Id: main})); err == nil {
		t.Fatal("the main chat was deleted")
	}
	if _, err := h.Client.RenameChat(h.Ctx(), connect.NewRequest(&v1.RenameChatRequest{BotId: id, Id: main, Title: "Other"})); err == nil {
		t.Fatal("the main chat was renamed")
	}
	var n int64
	h.DB.Model(&db.Chat{}).Where("bot_id = ? AND main", id).Count(&n)
	if n != 1 {
		t.Fatalf("%d main chats", n)
	}
}

// Replying to a Feed post lands in the main chat by default, after what was
// said there; the owner's next message is the reply.
func TestFeedQuoteGoesToTheMainChat(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_mq1",
		memCall("feed", `{"title":"Prices","text":"Coffee went up."}`),
		dummy.Turn{Text: "posted"},
		dummy.Turn{Text: "noted the quote"},
	)
	h := apptest.New(t, apptest.WithChatTools())
	id := h.CreateBot("Quoting").GetId()
	main := h.FirstChat(id)
	run, _ := h.Send(id, main, "Test_mq1_Input")
	h.WaitRun(run)
	posts := listFeed(t, h, id)
	if len(posts) != 1 {
		t.Fatalf("posts %d", len(posts))
	}

	q, err := h.Client.QuoteFeedPost(h.Ctx(), connect.NewRequest(&v1.QuoteFeedPostRequest{BotId: id, Id: posts[0].GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if q.Msg.GetChat().GetId() != main {
		t.Fatalf("quoted into %+v, want the main chat", q.Msg.GetChat())
	}
	if n := len(webChats(t, h, id)); n != 1 {
		t.Fatalf("%d chats; a reply to the Feed opened one", n)
	}
	evs := h.ChatEvents(main)
	if last := evs[len(evs)-1]; last.Kind != "feed_quote" || !strings.Contains(last.Body, "Coffee went up") {
		t.Fatalf("main chat ends with %s %q", last.Kind, last.Body)
	}

	run, _ = h.Send(id, main, "what should we do about it")
	h.WaitRun(run)
	sent := lastStreamedText(t)
	if !strings.Contains(sent, "Coffee went up") || !strings.Contains(sent, "what should we do about it") {
		t.Fatalf("the reply's turn lacks the quote:\n%s", sent)
	}
}

// lastStreamedText is the newest user turn the chat model was sent.
func lastStreamedText(t *testing.T) string {
	t.Helper()
	reqs := dummy.Streamed()
	if len(reqs) == 0 {
		t.Fatal("no chat request")
	}
	msgs := reqs[len(reqs)-1].Messages
	return msgs[len(msgs)-1].Text
}
