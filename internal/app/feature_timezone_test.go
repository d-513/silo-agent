package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func botIn(t *testing.T, h *apptest.H, name, tz string) string {
	t.Helper()
	res, err := h.Client.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: name, TimeZone: tz}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetId()
}

func sendFrom(t *testing.T, h *apptest.H, botID, chatID, text, tz string) string {
	t.Helper()
	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: botID, ChatId: chatID, Text: text, TimeZone: tz}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetRunId()
}

func zoneOf(t *testing.T, h *apptest.H, botID string) *v1.Bot {
	t.Helper()
	res, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

// userTexts is the user turns of a request, oldest first.
func userTexts(req llm.Request) []string {
	var out []string
	for _, m := range req.Messages {
		if m.Role == llm.RoleUser {
			out = append(out, m.Text)
		}
	}
	return out
}

// A Bot starts in the zone of the device that made it. A client that names
// none, or one the server does not know, gets the server's — by name.
func TestBotTakesTheCreatingDevicesZone(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	if got := zoneOf(t, h, botIn(t, h, "Tokyo", "Asia/Tokyo")); got.GetTimeZone() != "Asia/Tokyo" || got.GetTimeZonePinned() {
		t.Fatalf("zone %q pinned %v", got.GetTimeZone(), got.GetTimeZonePinned())
	}
	for _, tz := range []string{"", "Not/AZone", "../etc/passwd"} {
		got := zoneOf(t, h, botIn(t, h, "Server "+ids.New()[:6], tz)).GetTimeZone()
		if got == "" || got == "Local" || got == tz {
			t.Fatalf("a Bot made with zone %q is in %q", tz, got)
		}
	}
}

// The owner sets or pins the zone in Settings. Other saves of the Bot (the
// Memories page resends the whole request) leave it alone.
func TestUpdateBotZone(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := botIn(t, h, "Settler", "Europe/Warsaw")
	update := func(m *v1.UpdateBotRequest) (*v1.Bot, error) {
		m.Id, m.Name = id, "Settler"
		res, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(m))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	tz, pin := "America/Lima", true
	b, err := update(&v1.UpdateBotRequest{TimeZone: &tz, TimeZonePinned: &pin})
	if err != nil || b.GetTimeZone() != "America/Lima" || !b.GetTimeZonePinned() {
		t.Fatalf("bot %+v err %v", b, err)
	}
	if b, err = update(&v1.UpdateBotRequest{Description: "an unrelated save"}); err != nil || b.GetTimeZone() != "America/Lima" || !b.GetTimeZonePinned() {
		t.Fatalf("an unrelated save moved the zone: %+v err %v", b, err)
	}
	bad := "Mars/Olympus"
	if _, err = update(&v1.UpdateBotRequest{TimeZone: &bad}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("an unknown zone: %v", err)
	}
	if got := zoneOf(t, h, id).GetTimeZone(); got != "America/Lima" {
		t.Fatalf("zone %q after a refused update", got)
	}
}

// The zone follows the device the owner writes from, and every message tells
// the model the time where the human is, with the offset and the zone's name.
func TestZoneFollowsTheDevice(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := botIn(t, h, "Traveller", "Europe/Warsaw")
	chat := h.FirstChat(id)

	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	// Warsaw keeps summer time half the year: the stamp's offset is today's.
	warsawOffset := time.Now().In(warsaw).Format("-07:00")
	h.WaitRun(sendFrom(t, h, id, chat, "good morning", "Europe/Warsaw"))
	h.WaitRun(sendFrom(t, h, id, chat, "landed", "Asia/Tokyo"))
	if got := zoneOf(t, h, id).GetTimeZone(); got != "Asia/Tokyo" {
		t.Fatalf("the Bot is in %q after a message from Tokyo", got)
	}
	// A device that names no zone, or nonsense, changes nothing and is not refused.
	h.WaitRun(sendFrom(t, h, id, chat, "from an old client", ""))
	h.WaitRun(sendFrom(t, h, id, chat, "from a broken one", "Not/AZone"))
	if got := zoneOf(t, h, id).GetTimeZone(); got != "Asia/Tokyo" {
		t.Fatalf("the Bot is in %q after messages with no usable zone", got)
	}

	reqs := dummy.Streamed()
	got := userTexts(reqs[len(reqs)-1])
	if len(got) != 4 {
		t.Fatalf("%d user turns: %q", len(got), got)
	}
	for i, want := range []string{warsawOffset + " Europe/Warsaw] good morning", "+09:00 Asia/Tokyo] landed", "+09:00 Asia/Tokyo] from an old client", "+09:00 Asia/Tokyo] from a broken one"} {
		if !strings.HasSuffix(got[i], want) || !strings.HasPrefix(got[i], "[") {
			t.Errorf("turn %d is %q, want it to end %q", i, got[i], want)
		}
	}
	// Moving zone rewrote nothing the model had already been sent.
	assertAppendOnly(t, reqs)
}

// A pinned Bot stays where the owner put it, whatever device they write from.
func TestPinnedZoneDoesNotFollow(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := botIn(t, h, "Office", "Europe/Warsaw")
	pin := true
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id, Name: "Office", TimeZonePinned: &pin})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(sendFrom(t, h, id, h.FirstChat(id), "from a trip", "Asia/Tokyo"))
	if got := zoneOf(t, h, id).GetTimeZone(); got != "Europe/Warsaw" {
		t.Fatalf("a pinned Bot moved to %q", got)
	}
	reqs := dummy.Streamed()
	if got := userTexts(reqs[len(reqs)-1]); len(got) != 1 || !strings.HasSuffix(got[0], "Europe/Warsaw] from a trip") {
		t.Fatalf("user turns %q", got)
	}
}

// The Bot can be told where the human is from a conversation with no device
// (a channel): set_timezone moves it, and re-times the automations whose cron
// is read in the zone.
func TestSetTimezoneTool(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := botIn(t, h, "Asked", "Europe/Warsaw")
	if _, err := h.Client.CreateAutomation(h.Ctx(), connect.NewRequest(&v1.CreateAutomationRequest{
		BotId: id, Name: "Morning", Prompt: "say hello", Schedule: "0 9 * * *", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	next := func() time.Time {
		var au db.Automation
		if err := h.DB.First(&au, "bot_id = ? AND name = ?", id, "Morning").Error; err != nil || au.NextRunAt == nil {
			t.Fatalf("automation %+v err %v", au, err)
		}
		return *au.NextRunAt
	}
	warsaw, _ := time.LoadLocation("Europe/Warsaw")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	if at := next().In(warsaw); at.Hour() != 9 || at.Minute() != 0 {
		t.Fatalf("09:00 fires at %s in Warsaw", at)
	}

	res := pyCall(t, h, id, "", "bot", "set_timezone", `{"time_zone":"Asia/Tokyo"}`)
	if res.GetError() != "" || !strings.Contains(res.GetResultJson(), "Asia/Tokyo") || !strings.Contains(res.GetResultJson(), "Morning") {
		t.Fatalf("set_timezone: %+v", res)
	}
	if got := zoneOf(t, h, id).GetTimeZone(); got != "Asia/Tokyo" {
		t.Fatalf("zone %q", got)
	}
	if at := next().In(tokyo); at.Hour() != 9 || at.Minute() != 0 {
		t.Fatalf("09:00 fires at %s in Tokyo after the move", at)
	}
	if res := pyCall(t, h, id, "", "bot", "set_timezone", `{"time_zone":"Tokio"}`); !strings.Contains(res.GetError(), "unknown time zone") {
		t.Fatalf("an unknown zone: %+v", res)
	}
	if got := zoneOf(t, h, id).GetTimeZone(); got != "Asia/Tokyo" {
		t.Fatalf("zone %q after a refused call", got)
	}
}
