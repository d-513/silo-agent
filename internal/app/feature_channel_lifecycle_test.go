package app_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/channel"
	"silo.agent/internal/apptest"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func updateChannel(t *testing.T, h *apptest.H, ch db.Channel, enabled bool) {
	t.Helper()
	if _, err := h.Client.UpdateChannel(h.Ctx(), connect.NewRequest(&v1.UpdateChannelRequest{
		BotId: ch.BotID, Id: ch.ID, Name: ch.Name, Enabled: enabled, Inbound: true,
		ExternalId: ch.ExternalID, TargetTitle: ch.TargetTitle,
	})); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
}

func channelRow(h *apptest.H, id string) db.Channel {
	var ch db.Channel
	h.DB.First(&ch, "id = ?", id)
	return ch
}

// A restarted channel's new worker starts only once the old one has returned,
// so the old one's cleanup cannot take away what the new one set up (an
// adapter keeps its client in a map by channel id), and concurrent restarts
// leave exactly one worker.
func TestChannelRestartWaitsForTheOldWorker(t *testing.T) {
	slug := apptest.UniqueSlug("lifecycle")
	adapter := apptest.NewFakeAdapter(slug)
	var running, most, starts atomic.Int32
	adapter.StartFunc = func(ctx context.Context, _ *db.Channel, _ channels.Host) error {
		starts.Add(1)
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		<-ctx.Done()
		time.Sleep(100 * time.Millisecond) // letting go takes a moment
		running.Add(-1)
		return nil
	}
	channels.Register(adapter)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Restarter")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	waitFor(t, func() bool { return starts.Load() == 1 }, "the first start")

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); updateChannel(t, h, ch, true) }()
	}
	wg.Wait()
	waitFor(t, func() bool {
		time.Sleep(300 * time.Millisecond)
		return running.Load() == 1
	}, "one worker left running")
	if m := most.Load(); m != 1 {
		t.Fatalf("%d workers of one channel ran at once", m)
	}
}

func TestChannelStartErrorIsRetried(t *testing.T) {
	prev := channel.RetryDelay
	channel.RetryDelay = func(int) time.Duration { return 20 * time.Millisecond }
	t.Cleanup(func() { channel.RetryDelay = prev })

	slug := apptest.UniqueSlug("retry")
	adapter := apptest.NewFakeAdapter(slug)
	var starts atomic.Int32
	adapter.StartFunc = func(ctx context.Context, ch *db.Channel, host channels.Host) error {
		if starts.Add(1) == 1 {
			return errors.New("the platform is down")
		}
		host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "Connected"})
		<-ctx.Done()
		return nil
	}
	channels.Register(adapter)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Persistent")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	waitFor(t, func() bool {
		return starts.Load() == 2 && channelRow(h, ch.ID).Status == "connected"
	}, "a second start that connects")
}

func TestChannelPermanentStartErrorIsNotRetried(t *testing.T) {
	prev := channel.RetryDelay
	channel.RetryDelay = func(int) time.Duration { return 10 * time.Millisecond }
	t.Cleanup(func() { channel.RetryDelay = prev })

	slug := apptest.UniqueSlug("permanent")
	adapter := apptest.NewFakeAdapter(slug)
	var starts atomic.Int32
	adapter.StartFunc = func(context.Context, *db.Channel, channels.Host) error {
		starts.Add(1)
		return channels.Permanent(errors.New("bad token"))
	}
	channels.Register(adapter)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Refused")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	waitFor(t, func() bool { return channelRow(h, ch.ID).Status == "error" }, "the error")
	time.Sleep(200 * time.Millisecond)
	if n := starts.Load(); n != 1 {
		t.Fatalf("a permanent error was started %d times", n)
	}
	if got := channelRow(h, ch.ID).StatusDetail; got != "bad token" {
		t.Fatalf("status detail %q", got)
	}
}

// A stopped channel keeps its durable values but no longer says how it last
// stood; a pairing state clears an old error's detail.
func TestChannelStateFollowsItsWorker(t *testing.T) {
	slug := apptest.UniqueSlug("state")
	adapter := apptest.NewFakeAdapter(slug)
	adapter.StartFunc = func(ctx context.Context, ch *db.Channel, host channels.Host) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: "boom"})
		host.PublishState(ch.ID, channels.State{Kind: channels.StateQR, QR: "data:", Values: map[string]string{"jid": "me@s"}})
		host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "Connected as me"})
		<-ctx.Done()
		// What a stopped worker says last never sets the status.
		host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "late", Values: map[string]string{"late": "1"}})
		return nil
	}
	channels.Register(adapter)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Stateful")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	waitFor(t, func() bool { return channelRow(h, ch.ID).Status == "connected" }, "connected")

	updateChannel(t, h, ch, false)
	var got *v1.Channel
	waitFor(t, func() bool {
		list, err := h.Client.ListBotChannels(h.Ctx(), connect.NewRequest(&v1.ListBotChannelsRequest{BotId: bot.GetId()}))
		if err != nil {
			t.Fatal(err)
		}
		got = list.Msg.GetChannels()[0]
		return got.GetState().GetValues()["late"] == "1"
	}, "the late values")
	if got.GetStatus() != "stopped" || got.GetState().GetKind() != "" || got.GetState().GetMessage() != "" {
		t.Fatalf("a stopped channel shows %q %q %q", got.GetStatus(), got.GetState().GetKind(), got.GetState().GetMessage())
	}
	if got.GetState().GetValues()["jid"] != "me@s" {
		t.Fatalf("the paired id was lost: %v", got.GetState().GetValues())
	}
}

func TestChannelPairingClearsAnOldError(t *testing.T) {
	slug := apptest.UniqueSlug("pairing")
	adapter := apptest.NewFakeAdapter(slug)
	adapter.StartFunc = func(ctx context.Context, ch *db.Channel, host channels.Host) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: "boom"})
		host.PublishState(ch.ID, channels.State{Kind: channels.StateQR, QR: "data:"})
		<-ctx.Done()
		return nil
	}
	channels.Register(adapter)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Pairer")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	waitFor(t, func() bool { return channelRow(h, ch.ID).Status == "pending_auth" }, "pending_auth")
	if d := channelRow(h, ch.ID).StatusDetail; d != "" {
		t.Fatalf("pairing kept the old detail %q", d)
	}
}
