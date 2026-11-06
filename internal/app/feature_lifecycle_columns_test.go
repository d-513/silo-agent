package app_test

import (
	"errors"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// others is what other requests write on a Bot's row while its box is being
// started, stopped or removed: none of it is the lifecycle's to write.
var others = map[string]any{
	"proactivity":  "quiet",
	"time_zone":    "Asia/Tokyo",
	"audit_unseen": 7,
	"last_task":    "Sorting the mail",
	"soul":         "Written meanwhile",
	"speech_model": "dummy/speech",
}

func writeOthers(h *apptest.H, botID string) {
	h.T.Helper()
	if err := h.DB.Model(&db.Bot{}).Where("id = ?", botID).Updates(others).Error; err != nil {
		h.T.Fatal(err)
	}
}

func botRow(h *apptest.H, botID string) db.Bot {
	h.T.Helper()
	var row db.Bot
	if err := h.DB.First(&row, "id = ?", botID).Error; err != nil {
		h.T.Fatal(err)
	}
	return row
}

func checkOthers(t *testing.T, row db.Bot) {
	t.Helper()
	if row.Proactivity != "quiet" || row.TimeZone != "Asia/Tokyo" || row.AuditUnseen != 7 ||
		row.LastTask != "Sorting the mail" || row.Soul != "Written meanwhile" || row.SpeechModel != "dummy/speech" {
		t.Fatalf("a lifecycle write undid what was written meanwhile: proactivity=%q zone=%q audit=%d task=%q soul=%q speech=%q",
			row.Proactivity, row.TimeZone, row.AuditUnseen, row.LastTask, row.Soul, row.SpeechModel)
	}
}

// once runs f the first time the fake engine does op.
func once(h *apptest.H, op string, f func()) {
	done := false
	h.Fake.During = func(got string) {
		if got == op && !done {
			done = true
			f()
		}
	}
	h.T.Cleanup(func() { h.Fake.During = nil })
}

func TestStopBotKeepsColumnsWrittenMeanwhile(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Stopper")
	// A box made (and its token stored) while the stop was on its way: the
	// stop must not put the old token back, or live() drops the new box.
	once(h, "stop", func() {
		writeOthers(h, bot.GetId())
		h.DB.Model(&db.Bot{}).Where("id = ?", bot.GetId()).Update("token_hash", "made-meanwhile")
	})
	if _, err := h.Client.StopBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	row := botRow(h, bot.GetId())
	checkOthers(t, row)
	if row.TokenHash != "made-meanwhile" {
		t.Fatalf("stop wrote back a stale token hash: %q", row.TokenHash)
	}
}

func TestResetAndStartKeepColumnsWrittenMeanwhile(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Resetter")
	old := h.Fake.Token(bot.GetId())
	once(h, "drop", func() { writeOthers(h, bot.GetId()) })
	if _, err := h.Client.ResetContainer(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	row := botRow(h, bot.GetId())
	checkOthers(t, row)
	if row.ContainerID != "" {
		t.Fatalf("reset kept the container id %q", row.ContainerID)
	}
	// The removed box's token opens nothing any more.
	if row.TokenHash == ids.Hash(old) || row.TokenHash == "" {
		t.Fatalf("reset left the token hash %q", row.TokenHash)
	}

	h.DB.Model(&db.Bot{}).Where("id = ?", bot.GetId()).Updates(map[string]any{"proactivity": "off", "audit_unseen": 0})
	once(h, "create", func() { writeOthers(h, bot.GetId()) })
	if _, err := h.Client.StartBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	row = botRow(h, bot.GetId())
	checkOthers(t, row)
	if row.ContainerID != "cid-"+bot.GetId() || row.TokenHash != ids.Hash(h.Fake.Token(bot.GetId())) {
		t.Fatalf("start stored container %q token %q", row.ContainerID, row.TokenHash)
	}
}

func TestRemoveContainersKeepsColumnsWrittenMeanwhile(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Remover")
	once(h, "drop", func() { writeOthers(h, bot.GetId()) })
	if _, err := h.Client.RemoveBotContainers(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	checkOthers(t, botRow(h, bot.GetId()))

	h.DB.Model(&db.Bot{}).Where("id = ?", bot.GetId()).Updates(map[string]any{"proactivity": "off", "audit_unseen": 0})
	if _, err := h.Client.StartBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	once(h, "drop", func() { writeOthers(h, bot.GetId()) })
	if _, err := h.Client.RemoveAdminBotContainers(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	checkOthers(t, botRow(h, bot.GetId()))
}

// A Bot deleted while its box was being made stays deleted: storing the new
// box must not insert the row again.
func TestStartDoesNotBringADeletedBotBack(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Ghost")
	if _, err := h.Client.ResetContainer(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	once(h, "create", func() { h.DB.Delete(&db.Bot{}, "id = ?", bot.GetId()) })
	_, _ = h.Client.StartBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()}))
	var n int64
	h.DB.Model(&db.Bot{}).Where("id = ?", bot.GetId()).Count(&n)
	if n != 0 {
		t.Fatal("starting the box brought the deleted Bot's row back")
	}
}

// A Bot whose first box could not be made has no token yet; that must not
// stop the next Bot from being made (token_hash is unique).
func TestFailedFirstBoxDoesNotBlockTheNextBot(t *testing.T) {
	h := apptest.New(t)
	h.Fake.CreateErr = errors.New("engine down")
	h.CreateBot("First")
	h.CreateBot("Second")
	h.Fake.CreateErr = nil
	h.CreateBot("Third")
}
