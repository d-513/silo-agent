package app_test

import (
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
)

// A drive the CP leaves out keeps the reason it was left out: the sidecar
// stopping its mount and saying "stopped" does not wipe it. And a sidecar
// cannot write a state that is not one of its own.
func TestDriveLeftOutKeepsItsReason(t *testing.T) {
	h := driveHarness(t)
	bot := h.CreateBot("Keeper")
	draft, err := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "s3", Draft: true, Options: map[string]string{
		"access_key_id": "AKIA", "secret_access_key": "s3cret", "region": "eu-west-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "files", Options: map[string]string{
		"access_key_id": "AKIA", "region": "eu-west-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitDrive(t, h, bot.Id, d.Id, "mounted")
	starts := len(h.Drives.Runner.Starts())

	// The lender shared it, then left: it is signed out and waits for
	// someone to sign it in again.
	admin := h.AdminUser()
	h.App.Drives.SetShared(d.Id, true)
	h.App.Drives.Unlend(h.Ctx(), access.Lend{Kind: "drive", ID: d.Id, Name: "files", LentBy: admin.ID, Shared: true}, "Its lender left the workspace.")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := findDrive(listDrives(t, h, bot.Id, ""), d.Id)
		if got == nil || !strings.HasPrefix(got.State, "needs_") || got.Shared {
			t.Fatalf("the drive left out reads %+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := len(h.Drives.Runner.Starts()); n != starts {
		t.Fatalf("a drive with no credentials was mounted again (%d -> %d starts)", starts, n)
	}
}
