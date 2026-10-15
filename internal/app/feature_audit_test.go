package app_test

import (
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// What the gateway reports lands in two tables: one line per thing it did
// something about, and one summed row per minute of traffic.
func TestAuditIngestsGatewayReport(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Filtered")

	now := time.Now()
	h.App.Audit.IngestEvents([]*v1.EgressEvent{
		{
			BotId: bot.GetId(), At: now.UnixMilli(), Verdict: "block", Reason: "threat_list",
			Detail: "urlhaus", Host: "evil.example", Port: 443, Proto: "https", Method: "GET",
			Path: "/a?b=1", BytesUp: 12, BytesDown: 34,
		},
		{BotId: bot.GetId(), Verdict: "flag", Reason: "exfil_sink", Host: "paste.example"},
		{BotId: "gone", Verdict: "block", Reason: "private"},
	})

	byReason := func(reason string) db.AuditEvent {
		t.Helper()
		var e db.AuditEvent
		if err := h.DB.Where("bot_id = ? AND actor = ? AND reason = ?", bot.GetId(), "egress", reason).First(&e).Error; err != nil {
			t.Fatalf("no %s event: %v", reason, err)
		}
		return e
	}
	blocked := byReason("threat_list")
	if blocked.Decision != "block" || !blocked.Attention {
		t.Fatalf("a block needs attention: %+v", blocked)
	}
	if blocked.Host != "evil.example" || blocked.Port != 443 || blocked.Proto != "https" ||
		blocked.Method != "GET" || blocked.Path != "/a?b=1" || blocked.BytesUp != 12 || blocked.BytesDown != 34 {
		t.Fatalf("blocked event lost its detail: %+v", blocked)
	}
	if blocked.Detail != "urlhaus" {
		t.Fatalf("detail = %q", blocked.Detail)
	}
	flagged := byReason("exfil_sink")
	if flagged.Decision != "flag" || !flagged.Attention {
		t.Fatalf("an exfil sink flags and needs attention: %+v", flagged)
	}
	var gone int64
	h.DB.Model(&db.AuditEvent{}).Where("bot_id = ?", "gone").Count(&gone)
	if gone != 0 {
		t.Fatalf("an event for a Bot that is gone is dropped, got %d", gone)
	}

	minute := now.Add(-2 * time.Minute).Truncate(time.Minute).Unix()
	h.App.Audit.IngestTraffic([]*v1.EgressTrafficRow{
		{BotId: bot.GetId(), Host: "a.example", Minute: minute, Requests: 3, BytesUp: 100, BytesDown: 200, Blocked: 1},
		{BotId: bot.GetId(), Host: "a.example", Minute: minute, Requests: 2, BytesUp: 10, BytesDown: 20},
		{BotId: bot.GetId(), Host: "b.example", Minute: minute, Requests: 1},
	})
	// A later report for the same minute sums into the row, it does not replace it.
	h.App.Audit.IngestTraffic([]*v1.EgressTrafficRow{
		{BotId: bot.GetId(), Host: "a.example", Minute: minute, Requests: 5, Flagged: 2},
	})
	var traffic []db.EgressTraffic
	h.DB.Where("bot_id = ?", bot.GetId()).Order("host").Find(&traffic)
	if len(traffic) != 2 {
		t.Fatalf("want one row per host, got %d: %+v", len(traffic), traffic)
	}
	a := traffic[0]
	if a.Host != "a.example" || a.Requests != 10 || a.Blocked != 1 || a.Flagged != 2 || a.BytesUp != 110 || a.BytesDown != 220 {
		t.Fatalf("summed row = %+v", a)
	}
	if traffic[1].Requests != 1 {
		t.Fatalf("b.example = %+v", traffic[1])
	}
}

// Retention keeps the configured windows and drops what is older.
func TestAuditTrimKeepsTheWindow(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"audit:\n  detail_days: 7\n  traffic_days: 30\n"))
	bot := h.CreateBot("Filtered")
	now := time.Now()

	for _, e := range []db.AuditEvent{
		{ID: "old", BotID: bot.GetId(), Actor: "egress", CreatedAt: now.AddDate(0, 0, -10)},
		{ID: "new", BotID: bot.GetId(), Actor: "egress", CreatedAt: now.Add(-time.Hour)},
	} {
		if err := h.DB.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []db.EgressTraffic{
		{ID: "t-old", BotID: bot.GetId(), Host: "a.example", Minute: now.AddDate(0, 0, -40)},
		{ID: "t-new", BotID: bot.GetId(), Host: "a.example", Minute: now.AddDate(0, 0, -2)},
	} {
		if err := h.DB.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}

	h.App.Audit.Trim(now)

	var ids []string
	h.DB.Model(&db.AuditEvent{}).Where("bot_id = ?", bot.GetId()).Pluck("id", &ids)
	if len(ids) != 1 || ids[0] != "new" {
		t.Fatalf("audit events after trim = %v", ids)
	}
	ids = nil
	h.DB.Model(&db.EgressTraffic{}).Where("bot_id = ?", bot.GetId()).Pluck("id", &ids)
	if len(ids) != 1 || ids[0] != "t-new" {
		t.Fatalf("traffic after trim = %v", ids)
	}
}

// The retention defaults are the design's: a week of detail, a month of sums.
func TestAuditRetentionDefaults(t *testing.T) {
	h := apptest.New(t)
	if got := h.Store.Config().AuditDetailDays(); got != 7 {
		t.Fatalf("detail days = %d", got)
	}
	if got := h.Store.Config().AuditTrafficDays(); got != 30 {
		t.Fatalf("traffic days = %d", got)
	}
}
