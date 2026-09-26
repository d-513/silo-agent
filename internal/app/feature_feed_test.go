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

func listFeed(t *testing.T, h *apptest.H, botID string) []*v1.FeedPost {
	t.Helper()
	res, err := h.Client.ListFeed(h.Ctx(), connect.NewRequest(&v1.ListFeedRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetPosts()
}

func feedUnread(t *testing.T, h *apptest.H, botID string) int32 {
	t.Helper()
	res, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetFeedUnread()
}

func TestFeedPostReadDelete(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_90",
		memCall("feed", `{"title":"Digest","text":"**Three** things happened."}`),
		memCall("feed", `{"text":"   "}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Poster")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_90_Input")
	res := memResults(h.WaitRun(run))
	if len(res) != 2 || !strings.Contains(res[0], "posted") || !strings.Contains(res[1], "text required") {
		t.Fatalf("tool results %q\n%s", res, h.RunBody(run))
	}
	posts := listFeed(t, h, bot.GetId())
	if len(posts) != 1 {
		t.Fatalf("posts %d, want 1", len(posts))
	}
	p := posts[0]
	if p.GetTitle() != "Digest" || p.GetBody() != "**Three** things happened." || p.GetRead() {
		t.Fatalf("post %v", p)
	}
	if p.GetSourceKind() != "chat" || p.GetChatId() != chat || p.GetSourceName() == "" {
		t.Fatalf("source %q %q %q", p.GetSourceKind(), p.GetSourceName(), p.GetChatId())
	}
	if n := feedUnread(t, h, bot.GetId()); n != 1 {
		t.Fatalf("unread %d, want 1", n)
	}

	// Another Bot's Feed stays empty.
	other := h.CreateBot("Quiet")
	if len(listFeed(t, h, other.GetId())) != 0 || feedUnread(t, h, other.GetId()) != 0 {
		t.Fatal("feed leaked across bots")
	}

	if _, err := h.Client.MarkFeedRead(h.Ctx(), connect.NewRequest(&v1.MarkFeedReadRequest{BotId: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	if n := feedUnread(t, h, bot.GetId()); n != 0 {
		t.Fatalf("unread after mark %d", n)
	}
	if !listFeed(t, h, bot.GetId())[0].GetRead() {
		t.Fatal("post should be read")
	}

	if _, err := h.Client.DeleteFeedPost(h.Ctx(), connect.NewRequest(&v1.DeleteFeedPostRequest{BotId: other.GetId(), Id: p.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("deleting through another bot: %v", err)
	}
	if _, err := h.Client.DeleteFeedPost(h.Ctx(), connect.NewRequest(&v1.DeleteFeedPostRequest{BotId: bot.GetId(), Id: p.GetId()})); err != nil {
		t.Fatal(err)
	}
	if len(listFeed(t, h, bot.GetId())) != 0 {
		t.Fatal("post not deleted")
	}
}

func TestFeedPostFromAutomation(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_91",
		memCall("feed", `{"text":"Nightly check found nothing."}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Nightly")
	au := createAutomation(t, h, bot.GetId(), "Nightly check", "Test_91_Input", "")
	res, err := h.Client.RunAutomation(h.Ctx(), connect.NewRequest(&v1.RunAutomationRequest{BotId: bot.GetId(), Id: au.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(res.Msg.GetRunId())
	posts := listFeed(t, h, bot.GetId())
	if len(posts) != 1 || posts[0].GetSourceKind() != "automation" || posts[0].GetSourceName() != "Nightly check" {
		t.Fatalf("posts %v\n%s", posts, h.RunBody(res.Msg.GetRunId()))
	}
}

func TestFeedQuoteStartsChatWithPostInContext(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_92",
		memCall("feed", `{"title":"Prices","text":"Coffee went up. Test_93_Input"}`),
		dummy.Turn{Text: "done"},
	)
	// The quoted chat's first user turn carries the post, so its token drives the reply.
	dummy.Script("Test_93", dummy.Turn{Text: "I see the quoted post"})
	h := apptest.New(t)
	bot := h.CreateBot("Quoter")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_92_Input")
	h.WaitRun(run)
	posts := listFeed(t, h, bot.GetId())
	if len(posts) != 1 {
		t.Fatalf("posts %d\n%s", len(posts), h.RunBody(run))
	}

	q, err := h.Client.QuoteFeedPost(h.Ctx(), connect.NewRequest(&v1.QuoteFeedPostRequest{BotId: bot.GetId(), Id: posts[0].GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	chat := q.Msg.GetChat()
	if chat.GetTitle() != "Re: Prices" {
		t.Fatalf("title %q", chat.GetTitle())
	}
	evs := h.ChatEvents(chat.GetId())
	if len(evs) != 1 || evs[0].Kind != "feed_quote" || !strings.Contains(evs[0].Body, "Coffee went up") {
		t.Fatalf("quote events %v", evs)
	}
	if !listFeed(t, h, bot.GetId())[0].GetRead() {
		t.Fatal("quoting marks the post read")
	}
	chats, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range chats.Msg.GetChats() {
		found = found || c.GetId() == chat.GetId()
	}
	if !found {
		t.Fatal("quoted chat should be a normal web chat")
	}

	run2, _ := h.Send(bot.GetId(), chat.GetId(), "why?")
	h.WaitRun(run2)
	if body := h.RunBody(run2); !strings.Contains(body, "I see the quoted post") {
		t.Fatalf("model did not see the quote: %q", body)
	}
	var runs []db.Run
	h.DB.Where("chat_id = ?", chat.GetId()).Find(&runs)
	if len(runs) != 2 {
		t.Fatalf("runs %d", len(runs))
	}
}

func TestFeedPostFromPython(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Scripter")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "hello")
	h.WaitRun(run)

	wc := h.WorkerClient(bot.GetId())
	call := func(args string) *v1.ToolRes {
		t.Helper()
		res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "bot", Action: "feed", ArgsJson: args, RunId: run}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	if res := call(`{"title":"From Python","text":"scraped 3 rows"}`); res.GetError() != "" || !strings.Contains(res.GetResultJson(), "posted") {
		t.Fatalf("python feed: %+v", res)
	}
	if res := call(`{"text":""}`); res.GetError() != "text required" {
		t.Fatalf("empty post: %+v", res)
	}
	posts := listFeed(t, h, bot.GetId())
	if len(posts) != 1 || posts[0].GetTitle() != "From Python" || posts[0].GetSourceKind() != "chat" || posts[0].GetChatId() != chat {
		t.Fatalf("posts %v", posts)
	}

	// Python only reaches feed on the bot connector; soul stays a chat tool.
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "bot", Action: "soul", ArgsJson: `{"content":"x"}`, RunId: run}))
	if err != nil || res.Msg.GetError() != "unknown connector" {
		t.Fatalf("bot.soul from python: %v %+v", err, res.Msg)
	}

	// The shared gate: a Deny rule blocks the Python path too.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.GetId(), Connector: "bot", Action: "feed", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := call(`{"text":"blocked"}`); res.GetError() != "denied" {
		t.Fatalf("deny rule: %+v", res)
	}
}
