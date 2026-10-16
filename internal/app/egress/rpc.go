package egress

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/egress"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

// patternMax bounds one Bot's own lists: an owner's entries, not a threat list.
const patternMax = 200

// GetBotNetwork is a Bot's own view of the filter: the operator's entries (not
// editable here) over the Bot's own, and the switch for anything that is not
// HTTP.
func (s *Service) GetBotNetwork(ctx context.Context, req *connect.Request[v1.GetBotNetworkRequest]) (*connect.Response[v1.BotNetwork], error) {
	bot, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.network(bot)), nil
}

// AddHostPattern adds one entry to the Bot's own list.
func (s *Service) AddHostPattern(ctx context.Context, req *connect.Request[v1.AddHostPatternRequest]) (*connect.Response[v1.BotNetwork], error) {
	m := req.Msg
	bot, err := access.OwnBot(ctx, s.db, m.GetBotId())
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(strings.ToLower(m.GetKind()))
	switch kind {
	case "block", "trust", "pass":
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("kind must be block, trust or pass"))
	}
	pattern := strings.ToLower(strings.TrimSpace(m.GetPattern()))
	if !egress.ValidPattern(pattern) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("%q is not a host pattern: example.com, *.example.com, an IP or a CIDR", m.GetPattern()))
	}
	if len(pattern) > 253 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("pattern is too long"))
	}
	var n int64
	s.db.Model(&db.EgressPattern{}).Where("bot_id = ?", bot.ID).Count(&n)
	if n >= patternMax {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("this Bot keeps at most %d host patterns", patternMax))
	}
	row := db.EgressPattern{ID: ids.New(), BotID: bot.ID, Kind: kind, Pattern: pattern, CreatedAt: time.Now()}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	s.Apply()
	return connect.NewResponse(s.network(bot)), nil
}

// RemoveHostPattern drops one of the Bot's own entries.
func (s *Service) RemoveHostPattern(ctx context.Context, req *connect.Request[v1.RemoveHostPatternRequest]) (*connect.Response[v1.BotNetwork], error) {
	m := req.Msg
	bot, err := access.OwnBot(ctx, s.db, m.GetBotId())
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(strings.ToLower(m.GetKind()))
	pattern := strings.ToLower(strings.TrimSpace(m.GetPattern()))
	err = s.db.Where("bot_id = ? AND kind = ? AND pattern = ?", bot.ID, kind, pattern).
		Delete(&db.EgressPattern{}).Error
	if err != nil {
		return nil, err
	}
	s.Apply()
	return connect.NewResponse(s.network(bot)), nil
}

// SetOtherProtocols turns SOCKS5 on or off for this Bot's box: what lets it
// reach something that is not HTTP at all.
func (s *Service) SetOtherProtocols(ctx context.Context, req *connect.Request[v1.SetOtherProtocolsRequest]) (*connect.Response[v1.BotNetwork], error) {
	bot, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	if err := s.db.Model(&db.Bot{}).Where("id = ?", bot.ID).
		Update("other_protocols", req.Msg.GetOn()).Error; err != nil {
		return nil, err
	}
	bot.OtherProtocols = req.Msg.GetOn()
	s.Apply()
	return connect.NewResponse(s.network(bot)), nil
}

// GetEgressStatus is the filter's health for an admin: whether it is on, what
// the gateway last said, the key it decrypts with, and the threat lists.
func (s *Service) GetEgressStatus(ctx context.Context, _ *connect.Request[v1.GetEgressStatusRequest]) (*connect.Response[v1.EgressStatus], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	return connect.NewResponse(s.status()), nil
}

// RotateEgressCA makes a new certificate authority. Every box that the gateway
// decrypted for is unaffected (the gateway does the decrypting); connections
// through it are re-keyed, and nothing else has to change.
func (s *Service) RotateEgressCA(ctx context.Context, _ *connect.Request[v1.RotateEgressCARequest]) (*connect.Response[v1.EgressStatus], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := s.Rotate(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(s.status()), nil
}

// network is one Bot's lists and switch as the owner's page shows them.
func (s *Service) network(bot *db.Bot) *v1.BotNetwork {
	out := &v1.BotNetwork{Enabled: s.Enabled(), OtherProtocols: bot.OtherProtocols}
	e := s.cfg().Egress
	for _, group := range []struct {
		kind     string
		patterns []string
	}{{"block", e.Block}, {"trust", e.Trust}, {"pass", e.Pass}} {
		for _, p := range group.patterns {
			out.Patterns = append(out.Patterns, &v1.HostPattern{Kind: group.kind, Pattern: p, FromOperator: true})
		}
	}
	var rows []db.EgressPattern
	s.db.Where("bot_id = ?", bot.ID).Order("created_at").Find(&rows)
	for _, r := range rows {
		out.Patterns = append(out.Patterns, &v1.HostPattern{Kind: r.Kind, Pattern: r.Pattern})
	}
	return out
}

// status reads the gateway's last word and the CA.
func (s *Service) status() *v1.EgressStatus {
	out := &v1.EgressStatus{Enabled: s.Enabled(), Problem: s.Problem()}
	var host db.EgressHost
	if s.db.Where("id = ?", gatewayID).Limit(1).Find(&host).Error == nil && host.ID != "" {
		out.HealthyAt = textx.RFC3339(&host.HealthyAt)
		out.Networks = host.Networks
	}
	out.CaFingerprint = s.caFingerprint()

	var rows []db.EgressFeedState
	s.db.Find(&rows)
	state := map[string]db.EgressFeedState{}
	for _, r := range rows {
		state[r.ID] = r
	}
	feeds := s.cfg().EgressFeeds()
	for _, f := range feeds {
		out.Feeds = append(out.Feeds, feedStatus(f.ID, f.URL, state[f.ID]))
		delete(state, f.ID)
	}
	// A feed the operator removed still has its last state: show it too, so a
	// stale list is visible rather than silently gone.
	for id, r := range state {
		out.Feeds = append(out.Feeds, feedStatus(id, "", r))
	}
	return out
}

func feedStatus(id, url string, r db.EgressFeedState) *v1.FeedStatus {
	return &v1.FeedStatus{
		Id: id, Url: url, Entries: r.Entries,
		UpdatedAt: textx.RFC3339(&r.DownloadedAt), Error: r.Err,
	}
}

// caFingerprint is the CA certificate's SHA-256, as fingerprints are written.
func (s *Service) caFingerprint() string {
	var row db.EgressCA
	s.db.Select("cert_pem").Where("id = ?", caID).Limit(1).Find(&row)
	block, _ := pem.Decode(row.CertPEM)
	if block == nil {
		return ""
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	digits := strings.ToUpper(hex.EncodeToString(sum[:]))
	out := make([]string, 0, len(digits)/2)
	for i := 0; i+1 < len(digits); i += 2 {
		out = append(out, digits[i:i+2])
	}
	return strings.Join(out, ":")
}

// recordFeeds files what the gateway last reported about its threat lists.
func (s *Service) recordFeeds(feeds []*v1.EgressFeedState) {
	now := time.Now()
	for _, f := range feeds {
		if f.GetId() == "" {
			continue
		}
		row := db.EgressFeedState{
			ID: f.GetId(), Entries: f.GetEntries(),
			Err: textx.ClipRunes(f.GetError(), 300), ReportedAt: now,
		}
		if f.GetUpdatedAt() > 0 {
			row.DownloadedAt = time.Unix(f.GetUpdatedAt(), 0).UTC()
		}
		upd := map[string]any{"entries": row.Entries, "err": row.Err, "reported_at": now}
		if !row.DownloadedAt.IsZero() {
			upd["downloaded_at"] = row.DownloadedAt
		}
		// Health is a report, not a run, so a write that fails is dropped: the
		// next report carries the same news.
		s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(upd),
		}).Create(&row)
	}
}
