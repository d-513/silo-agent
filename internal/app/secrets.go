package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/db"
	"silo.agent/internal/egress"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

func (a *App) ListSecrets(ctx context.Context, req *connect.Request[v1.ListSecretsRequest]) (*connect.Response[v1.ListSecretsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var secs []db.Secret
	a.DB.Where("bot_id = ? AND name NOT LIKE ?", req.Msg.GetBotId(), "channel.%").Find(&secs)
	out := &v1.ListSecretsResponse{}
	for _, s := range secs {
		out.Secrets = append(out.Secrets, protoSecret(&s))
	}
	return connect.NewResponse(out), nil
}

func protoSecret(s *db.Secret) *v1.SecretMeta {
	m := &v1.SecretMeta{
		Id:        s.ID,
		Name:      s.Name,
		Mode:      secretMode(s.Mode),
		Hosts:     secretHostsOf(s.Hosts),
		Surrogate: s.Surrogate != "",
		SwapBody:  s.SwapBody,
	}
	m.CreatedAt = s.CreatedAt.Format(time.RFC3339)
	if s.LastUsedAt != nil {
		m.LastUsedAt = s.LastUsedAt.Format(time.RFC3339)
	}
	return m
}

func (a *App) AddSecret(ctx context.Context, req *connect.Request[v1.AddSecretRequest]) (*connect.Response[v1.SecretMeta], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	if req.Msg.GetName() == "" || req.Msg.GetValue() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name and value required"))
	}
	s := db.Secret{ID: ids.New(), BotID: req.Msg.GetBotId(), Name: req.Msg.GetName(), Value: req.Msg.GetValue(), SwapBody: req.Msg.GetSwapBody(), CreatedAt: time.Now()}
	if err := applySecretMode(&s, req.Msg.GetMode(), req.Msg.GetHosts(), true); err != nil {
		return nil, err
	}
	if err := a.DB.Create(&s).Error; err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a secret with that name already exists"))
	}
	a.forgetMask(s.BotID)
	a.Egress.Apply()
	return connect.NewResponse(protoSecret(&s)), nil
}

func (a *App) UpdateSecret(ctx context.Context, req *connect.Request[v1.UpdateSecretRequest]) (*connect.Response[v1.SecretMeta], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var s db.Secret
	if err := a.DB.First(&s, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	regenerate := false
	oldName := s.Name
	if req.Msg.Name != nil {
		if *req.Msg.Name == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name required"))
		}
		s.Name = *req.Msg.Name
	}
	if req.Msg.Value != nil {
		if *req.Msg.Value == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("value required"))
		}
		s.Value = *req.Msg.Value
		regenerate = true
	}
	if req.Msg.SwapBody != nil {
		s.SwapBody = *req.Msg.SwapBody
	}
	mode := s.Mode
	if req.Msg.Mode != nil {
		mode = req.Msg.GetMode()
		regenerate = true
	}
	hosts := req.Msg.GetHosts()
	if len(hosts) == 0 {
		hosts = secretHostsOf(s.Hosts)
	}
	// A stand-in only changes where the value or the mode did, so a Bot
	// holding one is not broken by an unrelated edit.
	if err := applySecretMode(&s, mode, hosts, regenerate); err != nil {
		return nil, err
	}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&s).Error; err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("a secret with that name already exists"))
		}
		// The secret's rule (secrets.<name>) follows it to its new name, so
		// a renamed secret keeps the owner's choice and leaves no ghost.
		if s.Name != oldName {
			// A row under the new name can only be a ghost: no secret had it.
			if err := tx.Where("bot_id = ? AND connector = ? AND action = ?", s.BotID, security.Secrets, s.Name).Delete(&db.Rule{}).Error; err != nil {
				return err
			}
			return tx.Model(&db.Rule{}).
				Where("bot_id = ? AND connector = ? AND action = ?", s.BotID, security.Secrets, oldName).
				Update("action", s.Name).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.forgetMask(s.BotID)
	a.Egress.Apply()
	return connect.NewResponse(protoSecret(&s)), nil
}

// applySecretMode writes a secret's mode, hosts and (surrogate-mode) stand-in
// from the caller's request. A surrogate needs allowed hosts and a value long
// enough to stand in for; anything else must be plain. The stand-in is only
// regenerated when regen is set (the value or the mode changed).
func applySecretMode(s *db.Secret, mode string, hosts []string, regen bool) error {
	if mode == "" {
		// Unspecified: surrogate when there are hosts to send the real value
		// to, plain otherwise (there is nothing to swap the stand-in back for).
		if len(hosts) == 0 {
			mode = "plain"
		} else {
			mode = "surrogate"
		}
	}
	s.Mode = secretMode(mode)
	s.Hosts = marshalSecretHosts(hosts)
	if s.Mode == "surrogate" {
		if len(hosts) == 0 {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("a surrogate secret needs at least one allowed host"))
		}
		for _, h := range normalizeHosts(hosts) {
			if err := checkSecretHost(h); err != nil {
				return connect.NewError(connect.CodeInvalidArgument, err)
			}
		}
		if egress.NewSurrogate(s.Value) == "" {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("the value is too short for a surrogate; use plain mode"))
		}
	}
	if regen || s.Surrogate == "" {
		s.Surrogate = makeSurrogate(s)
	}
	return nil
}

// checkSecretHost refuses an allowed host the real value must not be sent to:
// anything that is not a host pattern, and a public suffix ("com", "co.uk",
// "github.io"), which would let every site below it receive the real value.
func checkSecretHost(h string) error {
	if !egress.ValidPattern(h) {
		return fmt.Errorf("%q is not a host pattern", h)
	}
	bare := strings.TrimPrefix(egress.NormHost(h), "*.")
	if _, err := netip.ParseAddr(bare); err == nil {
		return nil
	}
	if suffix, _ := publicsuffix.PublicSuffix(bare); suffix == bare {
		return fmt.Errorf("%q is a public suffix: name the site the value is for", h)
	}
	return nil
}

// makeSurrogate is the stand-in for a secret, or "" when it has none.
func makeSurrogate(s *db.Secret) string {
	if s.Mode != "surrogate" {
		return ""
	}
	return egress.NewSurrogate(s.Value)
}

// secretMode normalizes a stored or requested mode; empty and anything
// unknown is the default, surrogate.
func secretMode(m string) string {
	if m == "plain" {
		return "plain"
	}
	return "surrogate"
}

// marshalSecretHosts normalizes host patterns (trim, lower, drop empty) into
// the JSON list the row stores.
func marshalSecretHosts(hosts []string) string {
	out := normalizeHosts(hosts)
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func normalizeHosts(hosts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// secretHostsOf reads the host list a row stores.
func secretHostsOf(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return normalizeHosts(out)
}

func (a *App) forgetMask(botID string) {
	a.mu.Lock()
	delete(a.mask, botID)
	a.mu.Unlock()
}

func (a *App) DeleteSecret(ctx context.Context, req *connect.Request[v1.DeleteSecretRequest]) (*connect.Response[v1.DeleteSecretResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var sec db.Secret
	if err := a.DB.First(&sec, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err == nil {
		a.DB.Where("bot_id = ? AND connector = ? AND action = ?", sec.BotID, security.Secrets, sec.Name).Delete(&db.Rule{})
	}
	a.DB.Where("id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Delete(&db.Secret{})
	a.forgetMask(req.Msg.GetBotId())
	a.Egress.Apply()
	return connect.NewResponse(&v1.DeleteSecretResponse{}), nil
}

func (a *App) ListApprovals(ctx context.Context, req *connect.Request[v1.ListApprovalsRequest]) (*connect.Response[v1.ListApprovalsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.Approval
	// Oldest first: the slip shows the head, so it must not reshuffle between
	// polls when two runs ask at once.
	a.DB.Where("bot_id = ? AND status = ?", req.Msg.GetBotId(), "pending").Order("created_at, id").Find(&rows)
	out := &v1.ListApprovalsResponse{}
	for _, r := range rows {
		p := protoApproval(&r)
		p.Source = a.approvalSource(r.RunID)
		out.Approvals = append(out.Approvals, p)
	}
	return connect.NewResponse(out), nil
}

// approvalSource names who asked for an approval so the slip can say which
// conversation is paused. A run-less approval came from an orphan call.
func (a *App) approvalSource(runID string) string {
	if runID == "" {
		return "Background process"
	}
	kind, name := chats.Source(a.DB, a.ChatOfRun(runID))
	switch kind {
	case "automation":
		return "Automation · " + name
	case "channel":
		return "Channel · " + name
	case "subagent":
		return "Subagent · " + name
	case "chat":
		return "Chat · " + name
	}
	return ""
}

func protoApproval(r *db.Approval) *v1.Approval {
	p := security.Describe(r.Connector, r.Action, r.ArgsJSON)
	out := &v1.Approval{
		Id: r.ID, BotId: r.BotID, RunId: r.RunID,
		Connector: r.Connector, Action: r.Action, ArgsJson: r.ArgsJSON, Status: r.Status,
		Title: p.Title, Summary: p.Summary, Auto: r.Auto,
	}
	for _, f := range p.Fields {
		out.Fields = append(out.Fields, &v1.ApprovalField{Label: f.Label, Value: f.Value})
	}
	return out
}

func (a *App) DecideApproval(ctx context.Context, req *connect.Request[v1.DecideApprovalRequest]) (*connect.Response[v1.Approval], error) {
	var row db.Approval
	if err := a.DB.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if _, err := a.ownBot(ctx, row.BotID); err != nil {
		return nil, err
	}
	dec := security.Vote(req.Msg.GetDecision())
	if dec == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("decision must be allow_once, always, or deny"))
	}
	// The slip of an Auto rule's approval settles one call. The rule stays
	// Auto: a client that still offers "always" must not turn it into Allow.
	if row.Auto && dec == security.Always {
		dec = security.Once
	}
	row.Status = dec
	a.DB.Save(&row)
	u := access.User(ctx)
	var bot db.Bot
	a.DB.First(&bot, "id = ?", row.BotID)
	a.audit(&bot, row.RunID, u.Email, row.Connector+"."+row.Action, dec)
	if dec == security.Always {
		var rule db.Rule
		a.DB.Where("bot_id = ? AND connector = ? AND action = ?", row.BotID, row.Connector, row.Action).Limit(1).Find(&rule)
		if rule.ID == "" {
			a.DB.Create(&db.Rule{ID: ids.New(), BotID: row.BotID, Connector: row.Connector, Action: row.Action, Decision: "allow"})
		} else {
			rule.Decision = "allow"
			a.DB.Save(&rule)
		}
	}
	a.emitDecision(&row, dec)
	a.mu.Lock()
	w := a.approvals[row.ID]
	delete(a.approvals, row.ID)
	a.mu.Unlock()
	if w != nil {
		select {
		case w.ch <- dec:
		default:
		}
	}
	a.recomputeStatus(row.BotID)
	return connect.NewResponse(protoApproval(&row)), nil
}

// emitDecision writes the human's vote into the run log so the thread keeps a
// receipt across reloads. It carries the catalog title and the first visible
// field (never args_json), and is emitted before the waiting tool resumes.
func (a *App) emitDecision(row *db.Approval, dec string) {
	if row.RunID == "" {
		return
	}
	p := security.Describe(row.Connector, row.Action, row.ArgsJSON)
	target := ""
	if len(p.Fields) > 0 {
		target = strings.Join(strings.Fields(p.Fields[0].Value), " ")
		if r := []rune(target); len(r) > 80 {
			target = string(r[:79]) + "…"
		}
	}
	body, _ := json.Marshal(map[string]string{
		"approval_id": row.ID,
		"decision":    dec,
		"title":       p.Title,
		"target":      target,
	})
	a.Emit(row.BotID, a.ChatOfRun(row.RunID), row.RunID, "decision", string(body), row.Connector+"."+row.Action)
}

func (a *App) setBotStatus(id, st string) {
	a.DB.Model(&db.Bot{}).Where("id = ?", id).Update("status", st)
}

func argsJSON(name string) string {
	b, _ := json.Marshal(map[string]string{"name": name})
	return string(b)
}
