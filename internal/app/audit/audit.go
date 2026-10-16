// Package audit is the installation's timeline of what its Bots did and what
// was done about it: every tool call and its decision, every tool result the
// output filter did not pass, and everything the network filter blocked,
// flagged or wants a human to look at. Calls and results are recorded as they
// happen; the gateway reports the network side over its session. Detail lines
// are swept after audit.detail_days, the per-minute traffic summaries after
// audit.traffic_days.
package audit

import (
	"log"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/host"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

// Tick is how often retention sweeps the tables.
const Tick = 10 * time.Minute

// Caps on what a gateway report can put in a row.
const (
	hostMax   = 253
	pathMax   = 1024
	detailMax = 500
	codeMax   = 64
)

// alertReasons are the reasons a human should see without asking: one of the
// Bot's own secrets leaving, or a send to an exfiltration sink.
var alertReasons = map[string]bool{
	"surrogate_leak": true,
	"plain_secret":   true,
	"secret_shape":   true,
	"exfil_sink":     true,
}

// Service records the timeline and serves it.
type Service struct {
	db   *gorm.DB
	cfg  func() config.Config
	host Host
}

// Host is what the timeline needs from the App around it.
type Host interface {
	host.Runs
	// LiveRuns names the runs at work on a Bot now. A gateway event can be put
	// down to a Bot's run only when exactly one was live; several mean it is
	// nobody's in particular.
	LiveRuns(botID string) []LiveRun
}

// LiveRun is one run at work: its id and the conversation it belongs to.
type LiveRun struct {
	RunID  string
	ChatID string
}

// New builds the audit service.
func New(gdb *gorm.DB, cfg func() config.Config, h Host) *Service {
	return &Service{db: gdb, cfg: cfg, host: h}
}

// Record adds a call or a result to the timeline. The engine calls it for every
// authorized action, so it stays one insert.
func (s *Service) Record(bot *db.Bot, runID, actor, action, decision string) {
	if s == nil || s.db == nil || bot == nil {
		return
	}
	e := db.AuditEvent{
		ID: ids.New(), BotID: bot.ID, BotName: bot.Name, Crest: bot.Crest,
		Actor:     textx.ClipRunes(actor, codeMax),
		Action:    textx.ClipRunes(action, 160),
		Decision:  textx.ClipRunes(decision, codeMax),
		CreatedAt: time.Now(),
	}
	s.attribute(&e, runID)
	if err := s.db.Create(&e).Error; err != nil {
		log.Printf("audit: record %s %s: %v", e.BotID, e.Action, err)
	}
}

// IngestEvents files what the gateway blocked, flagged or alerted on. It is the
// egress service's OnEvents sink.
func (s *Service) IngestEvents(events []*v1.EgressEvent) {
	if s == nil || s.db == nil || len(events) == 0 {
		return
	}
	now := time.Now()
	bots := map[string]*db.Bot{}
	rows := make([]db.AuditEvent, 0, len(events))
	for _, ev := range events {
		if ev == nil || ev.GetBotId() == "" {
			continue
		}
		bot := s.bot(bots, ev.GetBotId())
		if bot == nil {
			// The Bot is gone; its network is torn down with it.
			continue
		}
		e := db.AuditEvent{
			ID: ids.New(), BotID: bot.ID, BotName: bot.Name, Crest: bot.Crest,
			Actor:       "egress",
			Decision:    textx.ClipRunes(ev.GetVerdict(), codeMax),
			Reason:      textx.ClipRunes(ev.GetReason(), codeMax),
			Detail:      textx.CapRunes(textx.ValidUTF8(ev.GetDetail()), detailMax),
			Host:        textx.ClipRunes(ev.GetHost(), hostMax),
			Port:        port(ev.GetPort()),
			Proto:       textx.ClipRunes(ev.GetProto(), 16),
			Method:      textx.ClipRunes(ev.GetMethod(), 16),
			Path:        textx.CapRunes(textx.ValidUTF8(ev.GetPath()), pathMax),
			BytesUp:     max(ev.GetBytesUp(), 0),
			BytesDown:   max(ev.GetBytesDown(), 0),
			Attention:   attention(ev),
			ConnectorID: ev.GetConnectorId(),
			CreatedAt:   eventTime(ev.GetAt(), now),
		}
		s.attribute(&e, "")
		rows = append(rows, e)
	}
	if len(rows) == 0 {
		return
	}
	if err := s.db.CreateInBatches(&rows, 200).Error; err != nil {
		log.Printf("audit: ingest %d network events: %v", len(rows), err)
	}
	s.bumpUnseen(rows)
}

// bumpUnseen raises the owner's badge for the lines of a batch that need a
// human, once per Bot. The Audit tab clears it.
func (s *Service) bumpUnseen(rows []db.AuditEvent) {
	counts := map[string]int{}
	for _, r := range rows {
		if r.Attention {
			counts[r.BotID]++
		}
	}
	for botID, n := range counts {
		if err := s.db.Model(&db.Bot{}).Where("id = ?", botID).
			UpdateColumn("audit_unseen", gorm.Expr("audit_unseen + ?", n)).Error; err != nil {
			log.Printf("audit: bump unseen for %s: %v", botID, err)
		}
	}
}

// IngestTraffic sums what the gateway reported into the per-minute table. It is
// the egress service's OnTraffic sink.
func (s *Service) IngestTraffic(reported []*v1.EgressTrafficRow) {
	if s == nil || s.db == nil || len(reported) == 0 {
		return
	}
	now := time.Now()
	// One batch can hold the same minute twice; the columns are the key, so
	// they are summed here before a single upsert.
	agg := map[string]*db.EgressTraffic{}
	for _, r := range reported {
		if r == nil || r.GetBotId() == "" || strings.TrimSpace(r.GetHost()) == "" {
			continue
		}
		minute, ok := minuteOf(r.GetMinute(), now)
		if !ok {
			continue
		}
		host := textx.ClipRunes(r.GetHost(), hostMax)
		key := r.GetBotId() + "\x00" + r.GetConnectorId() + "\x00" + host + "\x00" + minute.Format(time.RFC3339)
		row := agg[key]
		if row == nil {
			row = &db.EgressTraffic{
				ID: ids.New(), BotID: r.GetBotId(), ConnectorID: r.GetConnectorId(),
				Host: host, Minute: minute,
			}
			agg[key] = row
		}
		row.Requests += max(r.GetRequests(), 0)
		row.Blocked += max(r.GetBlocked(), 0)
		row.Flagged += max(r.GetFlagged(), 0)
		row.BytesUp += max(r.GetBytesUp(), 0)
		row.BytesDown += max(r.GetBytesDown(), 0)
	}
	if len(agg) == 0 {
		return
	}
	rows := make([]db.EgressTraffic, 0, len(agg))
	for _, row := range agg {
		rows = append(rows, *row)
	}
	err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "bot_id"}, {Name: "connector_id"}, {Name: "host"}, {Name: "minute"}},
		DoUpdates: clause.Assignments(map[string]any{
			"requests":   gorm.Expr("egress_traffics.requests + EXCLUDED.requests"),
			"blocked":    gorm.Expr("egress_traffics.blocked + EXCLUDED.blocked"),
			"flagged":    gorm.Expr("egress_traffics.flagged + EXCLUDED.flagged"),
			"bytes_up":   gorm.Expr("egress_traffics.bytes_up + EXCLUDED.bytes_up"),
			"bytes_down": gorm.Expr("egress_traffics.bytes_down + EXCLUDED.bytes_down"),
			"updated_at": now,
		}),
	}).CreateInBatches(&rows, 200).Error
	if err != nil {
		log.Printf("audit: ingest %d traffic rows: %v", len(rows), err)
	}
}

// Trim drops detail lines and traffic older than the configured periods, and is
// the background sweep the App ticks.
func (s *Service) Trim(now time.Time) {
	if s == nil || s.db == nil || s.cfg == nil {
		return
	}
	cfg := s.cfg()
	detail := now.AddDate(0, 0, -cfg.AuditDetailDays())
	if err := s.db.Where("created_at < ?", detail).Delete(&db.AuditEvent{}).Error; err != nil {
		log.Printf("audit: trim events: %v", err)
	}
	traffic := now.AddDate(0, 0, -cfg.AuditTrafficDays())
	if err := s.db.Where("minute < ?", traffic).Delete(&db.EgressTraffic{}).Error; err != nil {
		log.Printf("audit: trim traffic: %v", err)
	}
}

// attribute puts a run on an event: the one the caller named, or the Bot's sole
// live run.
func (s *Service) attribute(e *db.AuditEvent, runID string) {
	if runID == "" && s.host != nil {
		if live := s.host.LiveRuns(e.BotID); len(live) == 1 {
			runID = live[0].RunID
		}
	}
	e.RunID = runID
	if runID != "" && s.host != nil {
		e.ChatID = s.host.ChatOfRun(runID)
	}
}

// bot reads a Bot row once per batch; the App has it in memory everywhere else.
func (s *Service) bot(cache map[string]*db.Bot, botID string) *db.Bot {
	if b, ok := cache[botID]; ok {
		return b
	}
	var b db.Bot
	if err := s.db.First(&b, "id = ?", botID).Error; err != nil {
		cache[botID] = nil
		return nil
	}
	cache[botID] = &b
	return &b
}

// attention is whether the line is one the owner should see without asking.
func attention(ev *v1.EgressEvent) bool {
	return ev.GetVerdict() == "block" || alertReasons[ev.GetReason()]
}

// port keeps a reported port in range; 0 means it was not known.
func port(p int32) int32 {
	if p < 0 || p > 65535 {
		return 0
	}
	return p
}

// minuteOf reads the minute a traffic row belongs to, refusing nonsense: a
// sidecar's clock is not trusted far.
func minuteOf(unix int64, now time.Time) (time.Time, bool) {
	if unix <= 0 {
		return time.Time{}, false
	}
	m := time.Unix(unix, 0).UTC().Truncate(time.Minute)
	if m.After(now.Add(5*time.Minute)) || m.Before(now.AddDate(0, 0, -31)) {
		return time.Time{}, false
	}
	return m, true
}

// eventTime reads the instant an event happened, refusing nonsense the way a
// drive change does.
func eventTime(ms int64, now time.Time) time.Time {
	if ms <= 0 {
		return now
	}
	at := time.UnixMilli(ms)
	if at.After(now.Add(time.Minute)) || at.Before(now.Add(-24*time.Hour)) {
		return now
	}
	return at
}
