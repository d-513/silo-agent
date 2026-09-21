//go:build integration

// Package integration drives a running Silo control plane as a client. It uses
// the developer's own silo.yaml (real provider keys, real Bot image) and costs
// tokens, so it is opt-in: set SILO_INTEGRATION=1 and start the stack first
// (`make dev`).
//
//	go test -tags integration ./integration/... -run Integration
package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func newClient(t *testing.T) (silov1connect.UIClient, string, string) {
	t.Helper()
	base := envOr("SILO_CP_URL", "http://localhost:8080")
	email := envOr("SILO_BOOTSTRAP_EMAIL", "admin@local")
	pass := envOr("SILO_BOOTSTRAP_PASSWORD", "silo-dev-pass")
	jar, _ := cookiejar.New(nil)
	client := silov1connect.NewUIClient(&http.Client{Jar: jar, Timeout: 0}, base)
	if _, err := client.SignIn(context.Background(), connect.NewRequest(&v1.SignInRequest{
		Email: email, Password: pass,
	})); err != nil {
		t.Fatalf("sign in at %s: %v", base, err)
	}
	return client, email, pass
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("SILO_INTEGRATION") != "1" {
		t.Skip("set SILO_INTEGRATION=1 to run live integration tests against a running stack")
	}
}

// TestIntegrationCreateBotSendMessage is the canonical client path: sign in,
// create a Bot, send one message, and see a real model reply.
func TestIntegrationCreateBotSendMessage(t *testing.T) {
	requireIntegration(t)
	client, _, _ := newClient(t)
	ctx := context.Background()

	name := fmt.Sprintf("E2E %d", time.Now().Unix())
	created, err := client.CreateBot(ctx, connect.NewRequest(&v1.CreateBotRequest{Name: name}))
	if err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	botID := created.Msg.GetId()
	t.Cleanup(func() {
		_, _ = client.DeleteBot(context.Background(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
	})

	chats, err := client.ListChats(ctx, connect.NewRequest(&v1.ListChatsRequest{BotId: botID}))
	if err != nil || len(chats.Msg.GetChats()) == 0 {
		t.Fatalf("ListChats: %v %+v", err, chats)
	}
	chatID := chats.Msg.GetChats()[0].GetId()

	if _, err := client.Send(ctx, connect.NewRequest(&v1.SendRequest{
		BotId: botID, ChatId: chatID,
		Text: "Reply with the single word PONG and nothing else.",
	})); err != nil {
		t.Fatalf("Send: %v", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	stream, err := client.StreamRun(streamCtx, connect.NewRequest(&v1.StreamRunRequest{
		BotId: botID, ChatId: chatID,
	}))
	if err != nil {
		t.Fatalf("StreamRun: %v", err)
	}
	defer stream.Close()

	var body strings.Builder
	for stream.Receive() {
		ev := stream.Msg()
		switch ev.GetKind() {
		case "chunk", "assistant", "section":
			body.WriteString(ev.GetBody())
		case "error":
			t.Fatalf("run error: %s", ev.GetBody())
		case "done":
			if got := strings.TrimSpace(body.String()); got == "" {
				t.Fatal("run finished with no assistant output")
			}
			t.Logf("assistant said: %q", strings.TrimSpace(body.String()))
			return
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	t.Fatal("stream ended before a done event")
}
