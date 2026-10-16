package app_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/config"
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

func auditBot(t *testing.T, h *apptest.H, botID string) *db.Bot {
	t.Helper()
	var b db.Bot
	if err := h.DB.First(&b, "id = ?", botID).Error; err != nil {
		t.Fatalf("bot row: %v", err)
	}
	return &b
}

// The timeline reads back through RPCs: filtering, the attention view, and a
// cursor that neither repeats nor skips.
func TestAuditListFiltersAndPages(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Timeline")
	row := auditBot(t, h, bot.GetId())
	other := h.CreateBot("Other")
	otherRow := auditBot(t, h, other.GetId())

	for _, call := range []struct{ actor, action, decision string }{
		{"gate", "web.search", "auto_allow"},
		{"gate", "terminal.run", "auto_deny"},
		{"you@test.local", "secrets.GITHUB_TOKEN", "allow_once"},
	} {
		h.App.Audit.Record(row, "", call.actor, call.action, call.decision)
	}
	h.App.Audit.Record(otherRow, "", "gate", "web.search", "auto_allow")
	h.App.Audit.IngestEvents([]*v1.EgressEvent{
		{BotId: bot.GetId(), At: time.Now().UnixMilli(), Verdict: "block", Reason: "threat_list", Detail: "urlhaus", Host: "evil.example"},
	})

	all, err := h.Client.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(all.Msg.GetEvents()) != 4 || all.Msg.GetMore() {
		t.Fatalf("timeline = %d rows, more = %v", len(all.Msg.GetEvents()), all.Msg.GetMore())
	}
	// The line the network filter blocked is there, and marked for a human.
	var blocked *v1.AuditEvent
	for _, e := range all.Msg.GetEvents() {
		if e.GetHost() == "evil.example" {
			blocked = e
		}
	}
	if blocked == nil || !blocked.GetAttention() || blocked.GetActor() != "egress" {
		t.Fatalf("blocked line = %+v", blocked)
	}

	attention, err := h.Client.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{BotId: bot.GetId(), View: "attention"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(attention.Msg.GetEvents()) != 1 || attention.Msg.GetEvents()[0].GetReason() != "threat_list" {
		t.Fatalf("attention view = %+v", attention.Msg.GetEvents())
	}

	// A search reads the columns a person would type.
	search, err := h.Client.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{Query: "urlhaus"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Msg.GetEvents()) != 1 || search.Msg.GetEvents()[0].GetBotId() != bot.GetId() {
		t.Fatalf("search = %+v", search.Msg.GetEvents())
	}
	decided, err := h.Client.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{
		BotId: bot.GetId(), Actor: "gate", Decision: "auto_deny",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(decided.Msg.GetEvents()) != 1 || decided.Msg.GetEvents()[0].GetAction() != "terminal.run" {
		t.Fatalf("filtered = %+v", decided.Msg.GetEvents())
	}

	// Two at a time, and the cursor walks the whole timeline once.
	var seen []string
	cursor := ""
	for page := 0; page < 4; page++ {
		res, err := h.Client.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{BotId: bot.GetId(), Limit: 2, Cursor: cursor}))
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, e := range res.Msg.GetEvents() {
			seen = append(seen, e.GetId())
		}
		if !res.Msg.GetMore() {
			break
		}
		cursor = res.Msg.GetNextCursor()
	}
	if len(seen) != 4 {
		t.Fatalf("paging saw %d rows: %v", len(seen), seen)
	}
	uniq := map[string]bool{}
	for _, id := range seen {
		if uniq[id] {
			t.Fatalf("paging repeated %s", id)
		}
		uniq[id] = true
	}
	// An admin with no bot_id reads every Bot's lines.
	wide, err := h.Client.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	wideBots := map[string]bool{}
	for _, e := range wide.Msg.GetEvents() {
		wideBots[e.GetBotId()] = true
	}
	if len(wide.Msg.GetEvents()) < 5 || !wideBots[other.GetId()] {
		t.Fatalf("installation timeline = %d rows over %v", len(wide.Msg.GetEvents()), wideBots)
	}
}

// A line is only readable by its owner, and the installation-wide view only by
// an admin.
func TestAuditIsScopedToTheOwner(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Mine")
	h.App.Audit.Record(auditBot(t, h, bot.GetId()), "", "gate", "web.search", "auto_allow")

	stranger, _ := h.SignedInUser("eve@test.local")
	if _, err := stranger.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{BotId: bot.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("another user read the timeline: %v", err)
	}
	if _, err := stranger.ListAuditEvents(h.Ctx(), connect.NewRequest(&v1.ListAuditEventsRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a non-admin read the installation timeline: %v", err)
	}
	if _, err := stranger.AuditSummary(h.Ctx(), connect.NewRequest(&v1.AuditSummaryRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a non-admin read the installation summary: %v", err)
	}
	if _, err := stranger.ListTraffic(h.Ctx(), connect.NewRequest(&v1.ListTrafficRequest{BotId: bot.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("another user read the traffic: %v", err)
	}
}

// The badge counts what needs a human, rides on the Bot row, and is cleared by
// opening the Audit tab.
func TestAuditBadgeAndSummary(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Watched")
	now := time.Now()
	h.App.Audit.IngestEvents([]*v1.EgressEvent{
		{BotId: bot.GetId(), At: now.UnixMilli(), Verdict: "block", Reason: "private", Host: "10.0.0.1"},
		{BotId: bot.GetId(), At: now.UnixMilli(), Verdict: "flag", Reason: "exfil_sink", Host: "paste.example"},
		{BotId: bot.GetId(), At: now.UnixMilli(), Verdict: "flag", Reason: "beaconing", Host: "c2.example"},
	})
	h.App.Audit.IngestTraffic([]*v1.EgressTrafficRow{
		{BotId: bot.GetId(), Host: "docs.example", Minute: now.Add(-time.Minute).Truncate(time.Minute).Unix(), Requests: 4, Blocked: 1},
	})

	res, err := h.Client.AuditSummary(h.Ctx(), connect.NewRequest(&v1.AuditSummaryRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatalf("AuditSummary: %v", err)
	}
	s := res.Msg
	if s.GetTotal() != 3 || s.GetAttention() != 2 || s.GetUnread() != 2 {
		t.Fatalf("summary = %+v", s)
	}
	if s.GetRequests() != 4 || s.GetBlocked() != 1 || s.GetDetailDays() != 7 || s.GetTrafficDays() != 30 {
		t.Fatalf("summary traffic = %+v", s)
	}
	// The count is on the Bot row the sidebar polls.
	got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetAuditUnseen() != 2 {
		t.Fatalf("Bot audit_unseen = %d", got.Msg.GetAuditUnseen())
	}
	// A line that is not one to look at does not count.
	h.App.Audit.Record(auditBot(t, h, bot.GetId()), "", "gate", "web.search", "auto_allow")
	if res, err := h.Client.AuditSummary(h.Ctx(), connect.NewRequest(&v1.AuditSummaryRequest{BotId: bot.GetId()})); err != nil || res.Msg.GetUnread() != 2 {
		t.Fatalf("unread after a plain allow = %d (%v)", res.Msg.GetUnread(), err)
	}

	read, err := h.Client.MarkAuditRead(h.Ctx(), connect.NewRequest(&v1.MarkAuditReadRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatalf("MarkAuditRead: %v", err)
	}
	if read.Msg.GetUnread() != 0 || read.Msg.GetAttention() != 2 {
		t.Fatalf("after reading: %+v", read.Msg)
	}
	got, err = h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetAuditUnseen() != 0 {
		t.Fatalf("Badge stayed at %d", got.Msg.GetAuditUnseen())
	}
}

// The traffic view sums per host, busiest first, over both windows.
func TestTrafficSumsPerHost(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Busy")
	now := time.Now()
	minute := now.Add(-3 * time.Minute).Truncate(time.Minute).Unix()
	h.App.Audit.IngestTraffic([]*v1.EgressTrafficRow{
		{BotId: bot.GetId(), Host: "docs.example", Minute: minute, Requests: 4, BytesUp: 10, BytesDown: 20},
		{BotId: bot.GetId(), Host: "docs.example", Minute: minute + 60, Requests: 6, BytesUp: 1, BytesDown: 2, Flagged: 1},
		{BotId: bot.GetId(), Host: "api.example", Minute: minute, Requests: 2},
	})

	res, err := h.Client.ListTraffic(h.Ctx(), connect.NewRequest(&v1.ListTrafficRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatalf("ListTraffic: %v", err)
	}
	rows := res.Msg.GetRows()
	if len(rows) != 2 {
		t.Fatalf("traffic rows = %+v", rows)
	}
	if rows[0].GetHost() != "docs.example" || rows[0].GetRequests() != 10 || rows[0].GetFlagged() != 1 ||
		rows[0].GetBytesUp() != 11 || rows[0].GetBytesDown() != 22 || rows[0].GetLastAt() == "" {
		t.Fatalf("busiest row = %+v", rows[0])
	}
	if rows[0].GetBotName() != "Busy" {
		t.Fatalf("traffic row lost the Bot's name: %+v", rows[0])
	}
	narrowed, err := h.Client.ListTraffic(h.Ctx(), connect.NewRequest(&v1.ListTrafficRequest{BotId: bot.GetId(), Query: "api"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed.Msg.GetRows()) != 1 || narrowed.Msg.GetRows()[0].GetHost() != "api.example" {
		t.Fatalf("filtered traffic = %+v", narrowed.Msg.GetRows())
	}
}

// The owner's lists lay over the operator's, and the switch for anything that
// is not HTTP is the Bot's own.
func TestBotNetworkListsAndSwitch(t *testing.T) {
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(dir)+
		"egress:\n  enabled: false\n  block: [ads.example]\n  trust: [corp.example]\n"))
	bot := h.CreateBot("Networked")

	net, err := h.Client.GetBotNetwork(h.Ctx(), connect.NewRequest(&v1.GetBotNetworkRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatalf("GetBotNetwork: %v", err)
	}
	if net.Msg.GetOtherProtocols() || len(net.Msg.GetPatterns()) != 2 {
		t.Fatalf("network = %+v", net.Msg)
	}
	for _, p := range net.Msg.GetPatterns() {
		if !p.GetFromOperator() {
			t.Fatalf("an operator entry must be marked as such: %+v", p)
		}
	}

	add, err := h.Client.AddHostPattern(h.Ctx(), connect.NewRequest(&v1.AddHostPatternRequest{
		BotId: bot.GetId(), Kind: "block", Pattern: "Evil.Example",
	}))
	if err != nil {
		t.Fatalf("AddHostPattern: %v", err)
	}
	// Stored lower-cased, and adding it twice is one entry.
	if _, err := h.Client.AddHostPattern(h.Ctx(), connect.NewRequest(&v1.AddHostPatternRequest{
		BotId: bot.GetId(), Kind: "block", Pattern: "evil.example",
	})); err != nil {
		t.Fatal(err)
	}
	var owned int
	for _, p := range add.Msg.GetPatterns() {
		if !p.GetFromOperator() {
			owned++
			if p.GetPattern() != "evil.example" || p.GetKind() != "block" {
				t.Fatalf("own entry = %+v", p)
			}
		}
	}
	if owned != 1 {
		t.Fatalf("own entries = %d", owned)
	}
	var rows int64
	h.DB.Model(&db.EgressPattern{}).Where("bot_id = ?", bot.GetId()).Count(&rows)
	if rows != 1 {
		t.Fatalf("stored patterns = %d", rows)
	}

	if _, err := h.Client.AddHostPattern(h.Ctx(), connect.NewRequest(&v1.AddHostPatternRequest{
		BotId: bot.GetId(), Kind: "allow", Pattern: "x.example",
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a kind that is not a list was accepted: %v", err)
	}
	if _, err := h.Client.AddHostPattern(h.Ctx(), connect.NewRequest(&v1.AddHostPatternRequest{
		BotId: bot.GetId(), Kind: "block", Pattern: "not a host",
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a pattern that is not a host was accepted: %v", err)
	}

	switched, err := h.Client.SetOtherProtocols(h.Ctx(), connect.NewRequest(&v1.SetOtherProtocolsRequest{
		BotId: bot.GetId(), On: true,
	}))
	if err != nil {
		t.Fatalf("SetOtherProtocols: %v", err)
	}
	if !switched.Msg.GetOtherProtocols() || !auditBot(t, h, bot.GetId()).OtherProtocols {
		t.Fatal("the switch did not stick")
	}

	gone, err := h.Client.RemoveHostPattern(h.Ctx(), connect.NewRequest(&v1.RemoveHostPatternRequest{
		BotId: bot.GetId(), Kind: "block", Pattern: "evil.example",
	}))
	if err != nil {
		t.Fatalf("RemoveHostPattern: %v", err)
	}
	for _, p := range gone.Msg.GetPatterns() {
		if !p.GetFromOperator() {
			t.Fatalf("the entry is still there: %+v", p)
		}
	}
	// An operator entry cannot be removed from under the owner's feet.
	if _, err := h.Client.RemoveHostPattern(h.Ctx(), connect.NewRequest(&v1.RemoveHostPatternRequest{
		BotId: bot.GetId(), Kind: "block", Pattern: "ads.example",
	})); err != nil {
		t.Fatal(err)
	}
	still, err := h.Client.GetBotNetwork(h.Ctx(), connect.NewRequest(&v1.GetBotNetworkRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(still.Msg.GetPatterns()) != 2 {
		t.Fatalf("the operator's entries must stay: %+v", still.Msg.GetPatterns())
	}

	stranger, _ := h.SignedInUser("eve@test.local")
	if _, err := stranger.GetBotNetwork(h.Ctx(), connect.NewRequest(&v1.GetBotNetworkRequest{BotId: bot.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("another user read the network lists: %v", err)
	}
	if _, err := stranger.AddHostPattern(h.Ctx(), connect.NewRequest(&v1.AddHostPatternRequest{
		BotId: bot.GetId(), Kind: "block", Pattern: "x.example",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("another user changed the network lists: %v", err)
	}
}

// The filter's own status is for an admin, and the CA rotates.
func TestEgressStatusAndRotate(t *testing.T) {
	h := apptest.New(t)
	// Nothing has asked the gateway to start yet, so make the CA the way a
	// first connect does, then read the status.
	if err := h.App.Egress.Rotate(h.Ctx()); err != nil {
		t.Fatalf("make the CA: %v", err)
	}
	st, err := h.Client.GetEgressStatus(h.Ctx(), connect.NewRequest(&v1.GetEgressStatusRequest{}))
	if err != nil {
		t.Fatalf("GetEgressStatus: %v", err)
	}
	if len(st.Msg.GetFeeds()) != len(config.DefaultEgressFeeds) {
		t.Fatalf("feeds = %+v", st.Msg.GetFeeds())
	}
	before := st.Msg.GetCaFingerprint()
	if before == "" {
		t.Fatal("no CA fingerprint")
	}
	rotated, err := h.Client.RotateEgressCA(h.Ctx(), connect.NewRequest(&v1.RotateEgressCARequest{}))
	if err != nil {
		t.Fatalf("RotateEgressCA: %v", err)
	}
	if rotated.Msg.GetCaFingerprint() == "" || rotated.Msg.GetCaFingerprint() == before {
		t.Fatalf("fingerprint %q -> %q", before, rotated.Msg.GetCaFingerprint())
	}

	stranger, _ := h.SignedInUser("eve@test.local")
	if _, err := stranger.GetEgressStatus(h.Ctx(), connect.NewRequest(&v1.GetEgressStatusRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a non-admin read the filter status: %v", err)
	}
	if _, err := stranger.RotateEgressCA(h.Ctx(), connect.NewRequest(&v1.RotateEgressCARequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a non-admin rotated the CA: %v", err)
	}
}
