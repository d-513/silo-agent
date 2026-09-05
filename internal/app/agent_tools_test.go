package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
)

func TestHistoryPairsStreamedTool(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Chat{ID: "c1"})
	a.DB.Create(&db.Run{ID: "r1", ChatID: "c1"})
	t0 := time.Now()
	add := func(id, kind, tool, body string, d time.Duration) {
		a.DB.Create(&db.RunEvent{ID: id, RunID: "r1", Kind: kind, Tool: tool, Body: body, CreatedAt: t0.Add(d)})
	}
	add("u1", "user", "", "hi", 0)
	add("t1", "tool", "read", "", time.Millisecond)
	add("a1", "tool_args_chunk", "read", `{"path":"x"}`, 2*time.Millisecond)
	add("t2", "tool", "read", `{"path":"x"}`, 3*time.Millisecond)
	add("ok", "tool_result", "read", "ok", 4*time.Millisecond)
	msgs := a.historyFromDB("c1")
	var calls, outs int
	for _, m := range msgs {
		if m.OfAssistant != nil {
			calls += len(m.OfAssistant.ToolCalls)
		}
		if m.OfTool != nil {
			outs++
		}
	}
	if calls != 1 || outs != 1 {
		t.Fatalf("calls %d outs %d msgs %d", calls, outs, len(msgs))
	}
}

func TestFormatRead(t *testing.T) {
	raw := "a\nb\nc\n"
	got := formatRead(raw, 2, 1)
	if !strings.Contains(got, "2|b") || !strings.Contains(got, "of 3") {
		t.Fatal(got)
	}
	if formatRead("", 1, 10) != "" {
		t.Fatal("empty")
	}
}

func TestCapHits(t *testing.T) {
	in := "1\n2\n3\n4\n"
	got := capHits(in, 2)
	if !strings.Contains(got, "1\n2\n…2 more") {
		t.Fatal(got)
	}
	if capHits("a\nb\n", 10) != "a\nb\n" {
		t.Fatal("short")
	}
}

func TestProtoBotDescription(t *testing.T) {
	a := testApp(t, nil)
	p := a.protoBot(&db.Bot{ID: "x", Name: "Scout", Description: "Mail clerk", Status: "stopped"}, false)
	if p.Description != "Mail clerk" {
		t.Fatal(p.Description)
	}
}

func TestGetContainer(t *testing.T) {
	h := &fakeHost{inspect: map[string]dockerx.State{
		"cid": {ID: "cid", Running: true},
	}}
	a := testApp(t, h)
	a.DB.Create(&db.User{ID: "u", Email: "a@b.c"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u", ContainerID: "cid"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	res, err := a.GetContainer(ctx, connect.NewRequest(&v1.GetBotRequest{Id: "b1"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Msg.Running || res.Msg.MemUsed == 0 {
		t.Fatalf("%+v", res.Msg)
	}
}

func TestUpdateBot(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.User{ID: "u", Email: "a@b.c"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u", Name: "Old", Description: "x"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	res, err := a.UpdateBot(ctx, connect.NewRequest(&v1.UpdateBotRequest{
		Id: "b1", Name: "Scout", Description: "Reads the mail",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.Name != "Scout" || res.Msg.Description != "Reads the mail" {
		t.Fatalf("%+v", res.Msg)
	}
}
