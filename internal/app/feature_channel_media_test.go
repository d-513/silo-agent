package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

type mediaRig struct {
	h       *apptest.H
	adapter *apptest.FakeAdapter
	bot     *v1.Bot
	ch      db.Channel
	host    channels.Host
}

func newMediaRig(t *testing.T, name string) *mediaRig {
	t.Helper()
	slug := apptest.UniqueSlug("mediachan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot(name)
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	return &mediaRig{h: h, adapter: adapter, bot: bot, ch: ch, host: adapter.WaitHost(t)}
}

func (r *mediaRig) deliver(t *testing.T, in channels.Inbound) {
	t.Helper()
	in.ExternalID, in.Title = "chat-1", "One"
	if err := r.host.DeliverInbound(context.Background(), &r.ch, in); err != nil {
		t.Fatal(err)
	}
}

// userEvents waits for n user events in the channel's conversation.
func (r *mediaRig) userEvents(t *testing.T, n int) []db.RunEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var out []db.RunEvent
		for _, c := range channelChats(r.h, r.ch.ID) {
			for _, ev := range r.h.ChatEvents(c.ID) {
				if ev.Kind == "user" {
					out = append(out, ev)
				}
			}
		}
		if len(out) >= n {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never saw %d user events", n)
	return nil
}

func memo(words string) channels.Media {
	return channels.Media{Name: "memo.ogg", Mime: "audio/ogg", Voice: true, Size: int64(len(words)),
		Fetch: func(context.Context) ([]byte, error) { return []byte(words), nil }}
}

func picture(name string, data []byte) channels.Media {
	return channels.Media{Name: name, Mime: "image/png", Size: int64(len(data)),
		Fetch: func(context.Context) ([]byte, error) { return data, nil }}
}

func TestChannelVoiceMemoReachesTheBotAsWords(t *testing.T) {
	r := newMediaRig(t, "Listener")
	dummy.Script("Test_81", dummy.Turn{Text: "heard you<section_send />"})

	// No worker is running: hearing a memo needs no machine.
	r.deliver(t, channels.Inbound{Files: []channels.Media{memo("Test_81_Input call the plumber")}})

	waitSent(t, r.adapter, "heard you")
	ev := r.userEvents(t, 1)[0]
	if ev.Body != "[voice message]\nTest_81_Input call the plumber" {
		t.Fatalf("the Bot read %q", ev.Body)
	}
	if ev.Meta != "" {
		t.Fatalf("a transcribed memo kept a file: %s", ev.Meta)
	}
}

func TestChannelVoiceMemoIsNeverACommand(t *testing.T) {
	r := newMediaRig(t, "Careful")
	dummy.Script("Test_82", dummy.Turn{Text: "just words<section_send />"})
	r.deliver(t, channels.Inbound{Files: []channels.Media{memo("/clear Test_82_Input")}})
	waitSent(t, r.adapter, "just words")
	if got := r.userEvents(t, 1)[0].Body; !strings.Contains(got, "/clear") {
		t.Fatalf("the Bot read %q", got)
	}
}

func TestChannelSilentVoiceMemoSaysSo(t *testing.T) {
	r := newMediaRig(t, "Quiet")
	dummy.Script("Test_91", dummy.Turn{Text: "pardon?<section_send />"})
	r.deliver(t, channels.Inbound{Text: "Test_91_Input", Files: []channels.Media{{Name: "memo.ogg", Mime: "audio/ogg", Voice: true,
		Fetch: func(context.Context) ([]byte, error) { return []byte("   "), nil }}}})
	if got := r.userEvents(t, 1)[0].Body; got != "Test_91_Input\n[voice message: no speech found]" {
		t.Fatalf("the Bot read %q", got)
	}
}

func TestChannelVoiceMemoThatCannotBeHeardIsKeptAsAFile(t *testing.T) {
	r := newMediaRig(t, "Keeper")
	r.h.StartWorker(r.bot.GetId())
	dummy.Script("Test_92", dummy.Turn{Text: "I got a recording<section_send />"})
	// Past the transcription cap, inside what the workspace takes.
	big := make([]byte, llm.MaxAudioBytes+1)
	r.deliver(t, channels.Inbound{Text: "Test_92_Input", Files: []channels.Media{{Name: "long.ogg", Mime: "audio/ogg", Voice: true, Size: int64(len(big)),
		Fetch: func(context.Context) ([]byte, error) { return big, nil }}}})

	ev := r.userEvents(t, 1)[0]
	if !strings.Contains(ev.Body, "[voice message: could not be transcribed]") || !strings.Contains(ev.Meta, "long.ogg") {
		t.Fatalf("event %q meta %s", ev.Body, ev.Meta)
	}
}

func TestChannelPictureIsSavedForTheBot(t *testing.T) {
	r := newMediaRig(t, "Eyes")
	r.h.StartWorker(r.bot.GetId())
	dummy.Script("Test_83", dummy.Turn{Text: "nice photo<section_send />"})

	r.deliver(t, channels.Inbound{Text: "Test_83_Input look", Files: []channels.Media{picture("../../etc/pho to.png", []byte("PNGDATA"))}})
	waitSent(t, r.adapter, "nice photo")

	ev := r.userEvents(t, 1)[0]
	if ev.Body != "Test_83_Input look" {
		t.Fatalf("body %q", ev.Body)
	}
	if strings.Contains(ev.Meta, "..") || strings.Contains(ev.Meta, "pho to") || !strings.Contains(ev.Meta, `"path":"tmp/`) || !strings.Contains(ev.Meta, "pho_to.png") {
		t.Fatalf("meta %s", ev.Meta)
	}
	// The model's turn names it, and it is on the box.
	var meta struct {
		Files []struct{ Path string } `json:"files"`
	}
	if err := json.Unmarshal([]byte(ev.Meta), &meta); err != nil {
		t.Fatal(err)
	}
	f, err := r.h.Client.ReadFile(r.h.Ctx(), connect.NewRequest(&v1.ReadFileRequest{BotId: r.bot.GetId(), Path: meta.Files[0].Path}))
	if err != nil || f.Msg.GetContent() != "PNGDATA" {
		t.Fatalf("on the box: %v %+v", err, f)
	}
}

func TestChannelFileThatCannotBeFetchedIsToldNotDropped(t *testing.T) {
	r := newMediaRig(t, "Honest")
	dummy.Script("Test_84", dummy.Turn{Text: "that one failed<section_send />"})
	r.deliver(t, channels.Inbound{Text: "Test_84_Input", Files: []channels.Media{
		{Name: "gone.pdf", Fetch: func(context.Context) ([]byte, error) { return nil, errors.New("link expired") }},
		{Name: "huge.mov", Size: 1 << 40, Fetch: func(context.Context) ([]byte, error) {
			t.Error("a file over the cap was downloaded")
			return nil, nil
		}},
	}})
	waitSent(t, r.adapter, "that one failed")
	got := r.userEvents(t, 1)[0].Body
	if !strings.Contains(got, "[attachment: gone.pdf could not be downloaded]") || !strings.Contains(got, "[attachment: huge.mov is too large") {
		t.Fatalf("the Bot read %q", got)
	}
}

func TestChannelMessagesKeepTheirOrderBehindAMemo(t *testing.T) {
	r := newMediaRig(t, "Orderly")
	dummy.Script("Test_85", dummy.Turn{Text: "first<section_send />"}, dummy.Turn{Text: "second<section_send />"})
	release := make(chan struct{})
	r.deliver(t, channels.Inbound{Files: []channels.Media{{Name: "memo.ogg", Mime: "audio/ogg", Voice: true,
		Fetch: func(context.Context) ([]byte, error) { <-release; return []byte("Test_85_Input spoken first"), nil }}}})

	// The adapter's event loop is free while the memo is read, and the text
	// typed after it waits its turn.
	r.deliver(t, channels.Inbound{Text: "typed second"})
	time.Sleep(100 * time.Millisecond)
	if n := len(channelChats(r.h, r.ch.ID)); n != 1 {
		t.Fatalf("%d conversations", n)
	}
	if evs := r.h.ChatEvents(channelChats(r.h, r.ch.ID)[0].ID); len(evs) != 0 {
		t.Fatalf("the text overtook the memo: %+v", evs)
	}
	close(release)

	// Both may land in one turn or two; what must hold is what the model read.
	waitSent(t, r.adapter, "first")
	var read string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		read = ""
		if reqs := dummy.Streamed(); len(reqs) > 0 {
			for _, m := range reqs[len(reqs)-1].Messages {
				if m.Role == llm.RoleUser {
					read += m.Text + "\n"
				}
			}
		}
		if strings.Contains(read, "typed second") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	first, second := strings.Index(read, "spoken first"), strings.Index(read, "typed second")
	if first < 0 || second < first {
		t.Fatalf("the model read %q", read)
	}
	if n := strings.Count(read, "typed second"); n != 1 {
		t.Fatalf("the model read the text %d times: %q", n, read)
	}
}

func TestChannelShowsTypingForAsLongAsTheRun(t *testing.T) {
	r := newMediaRig(t, "Typist")
	dummy.Script("Test_86", dummy.Turn{Text: "done<section_send />"})
	r.deliver(t, channels.Inbound{Text: "Test_86_Input"})
	waitSent(t, r.adapter, "done")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if typed := r.adapter.Typed(); len(typed) >= 2 && typed[0] && !typed[len(typed)-1] {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("typing: %v", r.adapter.Typed())
}

// silentAdapter is an adapter with no way to show typing.
type silentAdapter struct{ channels.Adapter }

func TestChannelWithoutTypingStillAnswers(t *testing.T) {
	slug := apptest.UniqueSlug("silentchan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(silentAdapter{adapter})
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Mute")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	host := adapter.WaitHost(t)
	dummy.Script("Test_87", dummy.Turn{Text: "fine<section_send />"})
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-1", Text: "Test_87_Input"}); err != nil {
		t.Fatal(err)
	}
	waitSent(t, adapter, "fine")
}

func TestChannelPresentSendsOnlyWhatIsForTheHuman(t *testing.T) {
	r := newMediaRig(t, "Presenter")
	r.h.StartWorker(r.bot.GetId())
	dummy.Script("Test_88",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"report.txt","content":"for you"}`},
			{Name: "write", Arguments: `{"path":"bot/notes.txt","content":"scratch"}`},
			{Name: "write", Arguments: `{"path":"tmp/theirs.txt","content":"they sent this"}`},
		}},
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "present", Arguments: `{"path":"bot/notes.txt"}`},
			{Name: "present", Arguments: `{"path":"tmp/theirs.txt"}`},
			{Name: "present", Arguments: `{"path":"report.txt"}`},
		}},
		dummy.Turn{Text: "all done<section_send />"},
	)
	r.deliver(t, channels.Inbound{Text: "Test_88_Input"})
	waitSent(t, r.adapter, "all done")

	var sent []string
	for _, m := range r.adapter.Messages() {
		for _, f := range m.Files {
			sent = append(sent, f.Name+"="+string(f.Data))
		}
	}
	if !slices.Equal(sent, []string{"report.txt=for you"}) {
		t.Fatalf("sent to the channel: %v", sent)
	}
}

func TestChannelAttachmentThatFailsTellsTheHuman(t *testing.T) {
	r := newMediaRig(t, "Unlucky")
	r.h.StartWorker(r.bot.GetId())
	r.adapter.SendFileErr = errors.New("file too large for this platform")
	dummy.Script("Test_89",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "write", Arguments: `{"path":"big.txt","content":"x"}`}}},
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "present", Arguments: `{"path":"big.txt"}`}}},
		dummy.Turn{Text: "tried<section_send />"},
	)
	r.deliver(t, channels.Inbound{Text: "Test_89_Input"})
	waitSent(t, r.adapter, "File: big.txt (could not be attached here)")
}

func TestChannelArtifactFromPythonReachesTheHuman(t *testing.T) {
	r := newMediaRig(t, "Maker")
	r.h.StartWorker(r.bot.GetId())
	dummy.Script("Test_90",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "write", Arguments: `{"path":"chart.txt","content":"made in python"}`}}},
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "sleep", Arguments: `{"seconds":30}`}}},
		dummy.Turn{Text: "finished<section_send />"},
	)
	r.deliver(t, channels.Inbound{Text: "Test_90_Input"})

	var runID string
	deadline := time.Now().Add(10 * time.Second)
	for runID == "" && time.Now().Before(deadline) {
		var runs []db.Run
		r.h.DB.Where("channel_id = ?", r.ch.ID).Find(&runs)
		for _, ru := range runs {
			if _, wrote := toolResults(r.h, ru.ID)["write"]; wrote {
				runID = ru.ID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("the run never wrote the file")
	}
	if res := pyCall(t, r.h, r.bot.GetId(), runID, "artifact", "emit", `{"path":"chart.txt"}`); res.GetError() != "" {
		t.Fatalf("artifact: %+v", res)
	}
	r.deliver(t, channels.Inbound{Text: "wake up"})
	waitSent(t, r.adapter, "finished")

	for _, m := range r.adapter.Messages() {
		for _, f := range m.Files {
			if f.Name == "chart.txt" && string(f.Data) == "made in python" {
				return
			}
		}
	}
	t.Fatalf("the artifact never reached the channel: %+v", r.adapter.Messages())
}
