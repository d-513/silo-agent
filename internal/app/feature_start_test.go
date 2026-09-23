package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// waitWorkerConnected blocks until the bot's worker dials the CP.
func waitWorkerConnected(h *apptest.H, id string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for !h.App.Hub.Connected(id) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

// TestMessageStartSurvivesStatusPolling pins the race where a message creates
// a box, a concurrent GetBot poll carries a row loaded before the create, and
// live() drops the fresh box as a stale token and clobbers the new token hash.
// A message on a bot with no container must leave a box standing.
func TestMessageStartSurvivesStatusPolling(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t)
	id := h.SeedBot("PollRace")
	h.StartSeededBot(id)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h.Host.Drop(ctx, id, "")
	time.Sleep(2 * time.Second)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = h.Client.GetBot(h.Ctx(), botReq(id))
			time.Sleep(20 * time.Millisecond)
		}
	}()

	chat := h.FirstChat(id)
	h.Send(id, chat, "Test_50_Input")
	ok := waitWorkerConnected(h, id, 90*time.Second)
	// Keep polling a moment past connect, then assert the box survives.
	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
	if !ok {
		t.Fatal("worker never connected after a message on a bot with no container")
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", id)
	if row.ContainerID == "" {
		t.Fatal("fresh container id was clobbered by status polling")
	}
}
