package apptest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// TestBotPrefix marks every container and row a container test owns. Cleanup
// only ever touches these, never a developer's real bots.
const TestBotPrefix = "zztest-"

// BotImage is the image container tests boot, overridable for CI.
func BotImage() string {
	if v := strings.TrimSpace(os.Getenv("SILO_BOT_IMAGE")); v != "" {
		return v
	}
	return "localhost/silo-bot:v1"
}

// ContainersEnabled reports whether the container tier can run. It honors
// SILO_SKIP_CONTAINERS=1 (always skip) and SILO_REQUIRE_CONTAINERS=1 (fail
// instead of skip when the daemon or image is missing).
func ContainersEnabled(t *testing.T) bool {
	t.Helper()
	if os.Getenv("SILO_SKIP_CONTAINERS") == "1" {
		t.Skip("SILO_SKIP_CONTAINERS=1")
		return false
	}
	missing := func(reason string) bool {
		if os.Getenv("SILO_REQUIRE_CONTAINERS") == "1" {
			t.Fatalf("container tests required but %s", reason)
		}
		t.Skipf("container tier skipped: %s (run `make images`, or set SILO_REQUIRE_CONTAINERS=1)", reason)
		return false
	}
	if _, err := exec.LookPath("podman"); err != nil {
		return missing("podman not found")
	}
	if out, err := exec.Command("podman", "image", "exists", BotImage()).CombinedOutput(); err != nil {
		return missing(fmt.Sprintf("image %s missing: %s", BotImage(), strings.TrimSpace(string(out))))
	}
	return true
}

// SweepTestContainers removes any container left behind by an interrupted
// container test. Safe: it only matches the zztest prefix.
func SweepTestContainers() {
	if _, err := exec.LookPath("podman"); err != nil {
		return
	}
	for _, filter := range []string{"name=^silo-" + TestBotPrefix, "name=^silo-mcp-" + TestBotPrefix} {
		out, err := exec.Command("podman", "ps", "-aq", "--filter", filter).Output()
		if err != nil {
			return
		}
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command("podman", "rm", "-f", id).Run()
		}
	}
}

// RemoveTestDataDir deletes a container-owned data directory. Root-owned
// files (the Chromium profile start.sh chowns) are removed inside the user
// namespace with podman unshare.
func RemoveTestDataDir(dir string) {
	if dir == "" {
		return
	}
	if _, err := exec.LookPath("podman"); err == nil {
		if err := exec.Command("podman", "unshare", "rm", "-rf", dir).Run(); err == nil {
			return
		}
	}
	_ = os.RemoveAll(dir)
}

// AdminUser returns the bootstrapped admin row.
func (h *H) AdminUser() *db.User {
	h.T.Helper()
	var u db.User
	if err := h.DB.Where("admin = ?", true).Order("created_at").First(&u).Error; err != nil {
		h.T.Fatalf("admin user: %v", err)
	}
	return &u
}

// SeedBot inserts a bot row with a zztest-prefixed id (so cleanup can find its
// container) and a default chat. It does not start the machine.
func (h *H) SeedBot(name string) string {
	h.T.Helper()
	u := h.AdminUser()
	id := TestBotPrefix + ids.New()[:12]
	b := db.Bot{ID: id, UserID: u.ID, Name: name, Status: "stopped", CreatedAt: time.Now()}
	if err := h.DB.Create(&b).Error; err != nil {
		h.T.Fatalf("seed bot: %v", err)
	}
	if err := h.DB.Create(&db.Chat{ID: ids.New(), BotID: id, Title: "New chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		h.T.Fatalf("seed chat: %v", err)
	}
	h.T.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := h.Client.DeleteBot(ctx, getBotRequest(id)); err != nil {
			h.Host.Drop(ctx, id, "")
		}
		RemoveTestDataDir(h.DataDir)
		SweepTestContainers()
	})
	return id
}

// StartSeededBot starts a seeded bot through the public API and waits until its
// worker dials the CP.
func (h *H) StartSeededBot(botID string) {
	h.T.Helper()
	if _, err := h.Client.StartBot(h.Ctx(), getBotRequest(botID)); err != nil {
		h.T.Fatalf("StartBot: %v", err)
	}
	h.WaitWorker(botID, 60*time.Second)
}

// WaitWorker blocks until the bot's worker connects.
func (h *H) WaitWorker(botID string, timeout time.Duration) {
	h.T.Helper()
	deadline := time.Now().Add(timeout)
	for !h.App.Hub.Connected(botID) {
		if time.Now().After(deadline) {
			h.T.Fatalf("worker for %s never connected", botID)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func getBotRequest(id string) *connect.Request[v1.GetBotRequest] {
	return connect.NewRequest(&v1.GetBotRequest{Id: id})
}
