package audit

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
)

// Paging: what one page holds, and the most it may be asked for.
const (
	auditPageDefault = 50
	auditPageMax     = 200
	trafficDefault   = 100
	trafficMax       = 500
)

// searchCols is the concatenation the trigram index is over. The query has to
// spell it the same way (db.Migrate's audit_events_search) for Postgres to use
// the index rather than read the table.
const searchCols = "coalesce(bot_name, '') || ' ' || coalesce(action, '') || ' ' || coalesce(decision, '') || ' ' || coalesce(reason, '') || ' ' || coalesce(host, '') || ' ' || coalesce(method, '') || ' ' || coalesce(path, '') || ' ' || coalesce(detail, '')"

// ListAuditEvents is the timeline, newest first. An owner names one of their
// Bots; an admin with no bot_id reads the whole installation.
func (s *Service) ListAuditEvents(ctx context.Context, req *connect.Request[v1.ListAuditEventsRequest]) (*connect.Response[v1.ListAuditEventsResponse], error) {
	m := req.Msg
	if err := s.scope(ctx, m.GetBotId()); err != nil {
		return nil, err
	}
	limit := int(m.GetLimit())
	if limit <= 0 {
		limit = auditPageDefault
	}
	if limit > auditPageMax {
		limit = auditPageMax
	}
	var rows []db.AuditEvent
	if err := s.filter(m).Order("created_at desc, id desc").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := &v1.ListAuditEventsResponse{More: more}
	titles := s.titles(rows)
	for i := range rows {
		out.Events = append(out.Events, eventRow(&rows[i], titles[rows[i].ChatID]))
	}
	if more && len(rows) > 0 {
		out.NextCursor = cursorOf(rows[len(rows)-1])
	}
	return connect.NewResponse(out), nil
}

// AuditSummary is what the page's header shows and what the badge counts: only
// the counts, never the lines themselves.
func (s *Service) AuditSummary(ctx context.Context, req *connect.Request[v1.AuditSummaryRequest]) (*connect.Response[v1.AuditSummaryResponse], error) {
	if err := s.scope(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(s.summary(req.Msg.GetBotId())), nil
}

// MarkAuditRead clears the Bot's badge. It answers the summary so the caller
// has the fresh counts.
func (s *Service) MarkAuditRead(ctx context.Context, req *connect.Request[v1.MarkAuditReadRequest]) (*connect.Response[v1.AuditSummaryResponse], error) {
	bot, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	if err := s.db.Model(&db.Bot{}).Where("id = ?", bot.ID).Update("audit_unseen", 0).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(s.summary(bot.ID)), nil
}

// ListTraffic is the network filter's sums by host, busiest first. It needs no
// machine: the sums come from the gateway's reports.
func (s *Service) ListTraffic(ctx context.Context, req *connect.Request[v1.ListTrafficRequest]) (*connect.Response[v1.ListTrafficResponse], error) {
	m := req.Msg
	if err := s.scope(ctx, m.GetBotId()); err != nil {
		return nil, err
	}
	limit := int(m.GetLimit())
	if limit <= 0 {
		limit = trafficDefault
	}
	if limit > trafficMax {
		limit = trafficMax
	}
	type row struct {
		BotID     string
		Host      string
		Requests  int64
		Blocked   int64
		Flagged   int64
		BytesUp   int64
		BytesDown int64
		LastAt    time.Time
	}
	q := s.db.Model(&db.EgressTraffic{}).
		Select("bot_id, host, sum(requests) as requests, sum(blocked) as blocked, sum(flagged) as flagged, "+
			"sum(bytes_up) as bytes_up, sum(bytes_down) as bytes_down, max(minute) as last_at").
		Where("minute >= ?", time.Now().AddDate(0, 0, -s.cfg().AuditTrafficDays())).
		Group("bot_id, host").Order("sum(requests) desc, host").Limit(limit)
	if id := m.GetBotId(); id != "" {
		q = q.Where("bot_id = ?", id)
	}
	if v := strings.TrimSpace(m.GetQuery()); v != "" {
		q = q.Where("host ILIKE ?", "%"+likeEscape(v)+"%")
	}
	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := &v1.ListTrafficResponse{}
	seen := map[string]bool{}
	var ids []string
	for _, r := range rows {
		if !seen[r.BotID] {
			seen[r.BotID] = true
			ids = append(ids, r.BotID)
		}
	}
	var bots []db.Bot
	if len(ids) > 0 {
		s.db.Select("id, name").Where("id IN ?", ids).Find(&bots)
	}
	names := map[string]string{}
	for _, b := range bots {
		names[b.ID] = b.Name
	}
	for _, r := range rows {
		out.Rows = append(out.Rows, &v1.TrafficRow{
			BotId: r.BotID, BotName: names[r.BotID], Host: r.Host,
			Requests: int32(r.Requests), Blocked: int32(r.Blocked), Flagged: int32(r.Flagged),
			BytesUp: r.BytesUp, BytesDown: r.BytesDown, LastAt: r.LastAt.UTC().Format(time.RFC3339),
		})
	}
	return connect.NewResponse(out), nil
}

// scope lets an admin read the whole installation and makes anyone else name a
// Bot they own.
func (s *Service) scope(ctx context.Context, botID string) error {
	if botID == "" {
		return access.RequireAdmin(ctx)
	}
	_, err := access.Bot(ctx, s.db, botID, access.Manage)
	return err
}

// filter builds the timeline's where clause from the request.
func (s *Service) filter(m *v1.ListAuditEventsRequest) *gorm.DB {
	q := s.db.Model(&db.AuditEvent{})
	if id := m.GetBotId(); id != "" {
		q = q.Where("bot_id = ?", id)
	}
	if m.GetView() == "attention" {
		q = q.Where("attention = ?", true)
	}
	if v := strings.TrimSpace(m.GetQuery()); v != "" {
		q = q.Where(searchCols+" ILIKE ?", "%"+likeEscape(v)+"%")
	}
	for col, v := range map[string]string{
		"actor":        m.GetActor(),
		"action":       m.GetAction(),
		"decision":     m.GetDecision(),
		"connector_id": m.GetConnectorId(),
	} {
		if v = strings.TrimSpace(v); v != "" {
			q = q.Where(col+" = ?", v)
		}
	}
	if at, id, ok := parseCursor(m.GetCursor()); ok {
		q = q.Where("(created_at, id) < (?, ?)", at, id)
	}
	return q
}

// summary counts what the page and the badge show.
func (s *Service) summary(botID string) *v1.AuditSummaryResponse {
	cfg := s.cfg()
	out := &v1.AuditSummaryResponse{
		DetailDays:  int32(cfg.AuditDetailDays()),
		TrafficDays: int32(cfg.AuditTrafficDays()),
	}
	events := func() *gorm.DB {
		q := s.db.Model(&db.AuditEvent{})
		if botID != "" {
			q = q.Where("bot_id = ?", botID)
		}
		return q
	}
	var total, attention int64
	events().Count(&total)
	events().Where("attention = ?", true).Count(&attention)
	out.Total, out.Attention = int32(total), int32(attention)

	var unread int64
	bots := s.db.Model(&db.Bot{}).Select("coalesce(sum(audit_unseen), 0)")
	if botID != "" {
		bots = bots.Where("id = ?", botID)
	}
	bots.Scan(&unread)
	out.Unread = int32(unread)

	var sums struct {
		Requests, Blocked, Flagged int64
	}
	traffic := s.db.Model(&db.EgressTraffic{}).Select("coalesce(sum(requests),0) as requests, coalesce(sum(blocked),0) as blocked, coalesce(sum(flagged),0) as flagged").
		Where("minute >= ?", time.Now().AddDate(0, 0, -cfg.AuditTrafficDays()))
	if botID != "" {
		traffic = traffic.Where("bot_id = ?", botID)
	}
	traffic.Scan(&sums)
	out.Requests, out.Blocked, out.Flagged = int32(sums.Requests), int32(sums.Blocked), int32(sums.Flagged)
	return out
}

// titles names the conversations the page's lines belong to.
func (s *Service) titles(rows []db.AuditEvent) map[string]string {
	ids := map[string]bool{}
	for _, r := range rows {
		if r.ChatID != "" {
			ids[r.ChatID] = true
		}
	}
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var chats []db.Chat
	s.db.Select("id, title").Where("id IN ?", list).Find(&chats)
	for _, c := range chats {
		out[c.ID] = c.Title
	}
	return out
}

// eventRow is one line as the page sees it.
func eventRow(e *db.AuditEvent, chatTitle string) *v1.AuditEvent {
	return &v1.AuditEvent{
		Id: e.ID, At: e.CreatedAt.UTC().Format(time.RFC3339),
		BotId: e.BotID, BotName: e.BotName, Crest: int32(e.Crest),
		Actor: e.Actor, Action: e.Action, Decision: e.Decision,
		Reason: e.Reason, Detail: e.Detail, Host: e.Host, Port: e.Port, Proto: e.Proto,
		Method: e.Method, Path: e.Path, BytesUp: e.BytesUp, BytesDown: e.BytesDown,
		Attention: e.Attention, ConnectorId: e.ConnectorID,
		RunId: e.RunID, ChatId: e.ChatID, ChatTitle: chatTitle,
	}
}

// cursorOf points past the last row of a page: the newest instant and id that
// has been read.
func cursorOf(e db.AuditEvent) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(e.CreatedAt.UnixNano(), 10) + ":" + e.ID))
}

// parseCursor reads a cursor back; a garbled one starts from the newest rather
// than failing the page.
func parseCursor(c string) (time.Time, string, bool) {
	if c == "" {
		return time.Time{}, "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", false
	}
	nanos, id, ok := strings.Cut(string(raw), ":")
	if !ok || id == "" {
		return time.Time{}, "", false
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.Unix(0, n).UTC(), id, true
}

// likeEscape keeps a search for a literal % or _ a search for that character.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
