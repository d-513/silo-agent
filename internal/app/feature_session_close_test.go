package app_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/rpcx"
)

// hangUps is how many sidecars dial and leave at once. A handler that returned
// while another goroutine was still sending would panic the whole control
// plane, and the race is too narrow to count on losing it once.
const hangUps = 300

// Every session opens by sending the full state, so a client that hangs up
// straight away ends the handler while that first send is on its way.
func TestDriveSessionSurvivesASidecarHangingUp(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Drives")
	token := ids.New()
	if err := h.DB.Create(&db.DriveHost{BotID: bot.GetId(), TokenHash: ids.Hash(token)}).Error; err != nil {
		t.Fatal(err)
	}
	client := silov1connect.NewDriveHostClient(rpcx.H2CClient(5*time.Second), h.URL, connect.WithInterceptors(rpcx.Bearer(token)))
	for range hangUps {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stream := client.Session(ctx)
		_ = stream.Send(&v1.DriveUp{})
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
		cancel()
	}
}

func TestEgressSessionSurvivesAGatewayHangingUp(t *testing.T) {
	h := apptest.New(t)
	token := ids.New()
	if err := h.DB.Create(&db.EgressHost{ID: "gateway", TokenHash: ids.Hash(token)}).Error; err != nil {
		t.Fatal(err)
	}
	client := silov1connect.NewEgressHostClient(rpcx.H2CClient(5*time.Second), h.URL, connect.WithInterceptors(rpcx.Bearer(token)))
	for range hangUps {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stream := client.Session(ctx)
		_ = stream.Send(&v1.EgressUp{})
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
		cancel()
	}
}
