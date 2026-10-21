// Package automation is scheduled background prompts: a name, a prompt, a cron
// schedule and an on/off switch per Bot. The scheduler fires due rows once into
// the same run engine as chat, from a fresh context each time. Every Bot also
// has one of each pinned automation: the Heartbeat, and whatever another
// domain registers with Pin (the Dashboard's curator). This package owns the
// rows, their RPCs and Bot tools, and the scheduler.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/prompts"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
	"silo.agent/internal/zone"
)

// Tick is how often the scheduler looks for due automations.
const Tick = 20 * time.Second

const (
	automationHeartbeat = "heartbeat"
	automationCustom    = "custom"

	// automationMinGap is the shortest allowed interval between two firings,
	// so a typo such as "* * * * *" cannot burn a model call every minute.
	automationMinGap = 5 * time.Minute
	automationMax    = 50
	automationName   = 80
	automationPrompt = 8000
)

// Host is what the automations need from the App around them.
type Host interface {
	host.Authorizer
}

// Pinned is an automation every Bot has exactly one of: made on first need
// without a schedule (so it never fires until given one), never deleted, its
// name fixed. A domain that tends something in the background registers one
// with Pin and talks to it through Pinned and Ask.
type Pinned struct {
	Kind, Name, Prompt string
	// Replaces are the openings of earlier default prompts: a row still on
	// one is moved to Prompt at start (UpgradePinned). An edited prompt stays.
	Replaces []string
}

// Service owns the automation rows, their tools and the scheduler.
type Service struct {
	db     *gorm.DB
	engine run.Engine
	host   Host
	// pins are the pinned kinds in the order they list first. Registered
	// while the App is built, read-only after.
	pins []Pinned
}

func New(gdb *gorm.DB, e run.Engine, h Host) *Service {
	s := &Service{db: gdb, engine: e, host: h}
	s.Pin(Pinned{Kind: automationHeartbeat, Name: "Heartbeat", Prompt: prompts.Heartbeat, Replaces: []string{heartbeatV1}})
	return s
}

// heartbeatV1 is the Heartbeat's first default prompt, before ask_owner.
const heartbeatV1 = "Heartbeat. No human sent this; it is your periodic check-in.\n\nLook over what you are responsible for: open tasks or notes in `/workspace`, and anything you promised to follow up on. Do the small things that are due now. If something needs the human, post it to the Feed with `feed`"

// SetHeartbeatSchedule puts the Heartbeat on a schedule (empty: it never
// fires) and switches it on. It writes those columns alone, so a prompt edit
// or a firing that lands meanwhile stays.
func (s *Service) SetHeartbeatSchedule(botID, sched string) error {
	au := s.Pinned(botID, automationHeartbeat)
	if au == nil {
		return errors.New("the Heartbeat is missing")
	}
	on := true
	_, err := s.saveAutomation(botID, au, automationPatch{schedule: &sched, enabled: &on}, "")
	return err
}

// Heartbeat is the Bot's Heartbeat schedule and switch; ok is false when it
// has none yet.
func (s *Service) Heartbeat(botID string) (schedule string, enabled, ok bool) {
	var au db.Automation
	if s.db.Select("schedule, enabled").Where("bot_id = ? AND kind = ?", botID, automationHeartbeat).Limit(1).Find(&au).RowsAffected == 0 {
		return "", false, false
	}
	return au.Schedule, au.Enabled, true
}

// Kinds are the registered pinned kinds, in the order they list.
func (s *Service) Kinds() []string {
	out := make([]string, len(s.pins))
	for i, p := range s.pins {
		out[i] = p.Kind
	}
	return out
}

// Pin registers a pinned kind. Call it while the App is being built.
func (s *Service) Pin(p Pinned) { s.pins = append(s.pins, p) }

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// parseSchedule validates a cron schedule. Empty is valid and never fires.
func parseSchedule(s string) (cron.Schedule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	sched, err := cronParser.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("schedule %q: %w", s, err)
	}
	first := sched.Next(time.Now())
	if first.IsZero() {
		return nil, fmt.Errorf("schedule %q never fires", s)
	}
	if second := sched.Next(first); !second.IsZero() && second.Sub(first) < automationMinGap {
		return nil, fmt.Errorf("schedule %q fires more often than every %d minutes", s, int(automationMinGap.Minutes()))
	}
	return sched, nil
}

// nextRun is the next firing after from, or nil when the automation is paused
// or has no schedule. The cron fields are wall-clock time in loc, the Bot's
// zone: a schedule with no zone of its own is read in the zone of the time it
// is asked about.
func nextRun(au *db.Automation, from time.Time, loc *time.Location) *time.Time {
	if !au.Enabled {
		return nil
	}
	sched, err := parseSchedule(au.Schedule)
	if err != nil || sched == nil {
		return nil
	}
	t := sched.Next(from.In(loc))
	if t.IsZero() {
		return nil
	}
	return &t
}

// zones answers a Bot's time zone, asking the database once per Bot.
func (s *Service) zones() func(botID string) *time.Location {
	seen := map[string]*time.Location{}
	return func(botID string) *time.Location {
		if loc, ok := seen[botID]; ok {
			return loc
		}
		seen[botID] = zone.OfBot(s.db, botID)
		return seen[botID]
	}
}

// Rezone re-times a Bot's automations after its time zone changed: the same
// cron now names other moments.
func (s *Service) Rezone(botID string) {
	var rows []db.Automation
	s.db.Where("bot_id = ? AND next_run_at IS NOT NULL", botID).Find(&rows)
	now, loc := time.Now(), s.zones()(botID)
	for i := range rows {
		s.db.Model(&rows[i]).Update("next_run_at", nextRun(&rows[i], now, loc))
	}
}

// --- store ---

// pin is the registered pinned kind, and where it lists among them.
func (s *Service) pin(kind string) (Pinned, int, bool) {
	for i, p := range s.pins {
		if p.Kind == kind {
			return p, i, true
		}
	}
	return Pinned{}, len(s.pins), false
}

func (s *Service) isPinned(kind string) bool {
	_, _, ok := s.pin(kind)
	return ok
}

// EnsurePinned gives a Bot every pinned automation it is missing.
func (s *Service) EnsurePinned(botID string) {
	for _, p := range s.pins {
		s.ensurePinned(botID, p.Kind, p.Name, p.Prompt)
	}
}

// ensurePinned is called from every page that lists automations, often by
// several requests at once, so a count-then-create would race and pin two. The
// partial unique index on (bot_id, kind) for pinned kinds makes the insert the
// check: a loser's insert does nothing, and only the winner makes the log chat.
func (s *Service) ensurePinned(botID, kind, name, prompt string) {
	var n int64
	s.db.Model(&db.Automation{}).Where("bot_id = ? AND kind = ?", botID, kind).Count(&n)
	if n > 0 {
		return
	}
	now := time.Now()
	au := db.Automation{
		ID: ids.New(), BotID: botID, Kind: kind, Name: name,
		Prompt: strings.TrimSpace(prompt), Enabled: true, CreatedBy: "user",
		CreatedAt: now, UpdatedAt: now,
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&au)
	if res.Error != nil {
		log.Printf("%s %s: %v", kind, botID, res.Error)
		return
	}
	if res.RowsAffected == 1 {
		s.ensureAutomationChat(&au)
	}
}

// ensureAutomationChat gives an automation its hidden log Chat.
func (s *Service) ensureAutomationChat(au *db.Automation) {
	if au.ChatID != "" {
		var n int64
		s.db.Model(&db.Chat{}).Where("id = ?", au.ChatID).Count(&n)
		if n > 0 {
			return
		}
	}
	now := time.Now()
	c := db.Chat{ID: ids.New(), BotID: au.BotID, AutomationID: au.ID, Title: au.Name, CreatedAt: now, UpdatedAt: now}
	if err := s.db.Create(&c).Error; err != nil {
		log.Printf("automation chat %s: %v", au.ID, err)
		return
	}
	au.ChatID = c.ID
	if au.CreatedAt.IsZero() {
		return // not persisted yet; the caller's Create writes chat_id
	}
	s.db.Model(&db.Automation{}).Where("id = ?", au.ID).Update("chat_id", c.ID)
}

func (s *Service) listAutomations(botID string) []db.Automation {
	s.EnsurePinned(botID)
	var rows []db.Automation
	// The pinned ones first, in the order they were registered; the rest
	// newest first.
	s.db.Where("bot_id = ?", botID).Order("created_at desc").Find(&rows)
	sort.SliceStable(rows, func(i, j int) bool {
		_, a, _ := s.pin(rows[i].Kind)
		_, b, _ := s.pin(rows[j].Kind)
		return a < b
	})
	return rows
}

// findAutomation resolves an id or a (case-insensitive) name within a Bot.
func (s *Service) findAutomation(botID, ref string) (*db.Automation, error) {
	s.EnsurePinned(botID)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("automation required")
	}
	var au db.Automation
	s.db.Where("bot_id = ? AND id = ?", botID, ref).Limit(1).Find(&au)
	if au.ID == "" {
		s.db.Where("bot_id = ? AND lower(name) = lower(?)", botID, ref).Limit(1).Find(&au)
	}
	if au.ID == "" {
		return nil, fmt.Errorf("unknown automation %q", ref)
	}
	return &au, nil
}

type automationPatch struct {
	name     *string
	prompt   *string
	schedule *string
	enabled  *bool
}

// saveAutomation validates and applies a patch. A nil au creates a custom
// automation owned by createdBy.
func (s *Service) saveAutomation(botID string, au *db.Automation, p automationPatch, createdBy string) (*db.Automation, error) {
	creating := au == nil
	if creating {
		var n int64
		s.db.Model(&db.Automation{}).Where("bot_id = ?", botID).Count(&n)
		if n >= automationMax {
			return nil, fmt.Errorf("a Bot can have at most %d automations", automationMax)
		}
		au = &db.Automation{ID: ids.New(), BotID: botID, Kind: automationCustom, Enabled: true, CreatedBy: createdBy}
	}
	if p.name != nil {
		au.Name = textx.ClipRunes(*p.name, automationName)
	}
	if p.prompt != nil {
		au.Prompt = strings.TrimSpace(*p.prompt)
	}
	if p.schedule != nil {
		au.Schedule = strings.Join(strings.Fields(*p.schedule), " ")
	}
	if p.enabled != nil {
		au.Enabled = *p.enabled
	}
	if p, _, ok := s.pin(au.Kind); ok {
		au.Name = p.Name
	}
	if au.Name == "" {
		return nil, errors.New("name required")
	}
	if au.Prompt == "" {
		return nil, errors.New("prompt required")
	}
	if utf8.RuneCountInString(au.Prompt) > automationPrompt {
		return nil, fmt.Errorf("prompt is over %d characters", automationPrompt)
	}
	if _, err := parseSchedule(au.Schedule); err != nil {
		return nil, err
	}
	var clash db.Automation
	s.db.Where("bot_id = ? AND lower(name) = lower(?) AND id <> ?", botID, au.Name, au.ID).Limit(1).Find(&clash)
	if clash.ID != "" {
		return nil, fmt.Errorf("an automation named %q already exists", au.Name)
	}
	now := time.Now()
	au.NextRunAt = nextRun(au, now, s.zones()(botID))
	au.UpdatedAt = now
	if creating {
		au.CreatedAt = now
		s.ensureAutomationChat(au)
		if err := s.db.Create(au).Error; err != nil {
			return nil, err
		}
		return au, nil
	}
	// Only the columns the patch names (and the next firing they move): a
	// whole-row save would undo a concurrent edit or a firing's last_* write.
	cols := []string{"updated_at"}
	if p.schedule != nil || p.enabled != nil {
		cols = append(cols, "next_run_at")
	}
	if p.name != nil {
		cols = append(cols, "name")
	}
	if p.prompt != nil {
		cols = append(cols, "prompt")
	}
	if p.schedule != nil {
		cols = append(cols, "schedule")
	}
	if p.enabled != nil {
		cols = append(cols, "enabled")
	}
	if err := s.db.Model(&db.Automation{}).Where("id = ?", au.ID).Select(cols).Updates(au).Error; err != nil {
		return nil, err
	}
	if p.name != nil {
		s.db.Model(&db.Chat{}).Where("id = ?", au.ChatID).Update("title", au.Name)
	}
	return au, nil
}

func (s *Service) deleteAutomation(au *db.Automation) error {
	if s.isPinned(au.Kind) {
		return fmt.Errorf("the %s is pinned; clear its schedule or pause it instead", au.Name)
	}
	if au.ChatID != "" {
		s.engine.StopChatLive(au.BotID, au.ChatID)
		chats.Drop(s.db, au.ChatID)
	}
	return s.db.Delete(au).Error
}

// --- scheduler ---

// Reschedule recomputes every pending firing from now. Firings missed
// while the Control Plane was down are skipped, not backfilled.
func (s *Service) Reschedule() {
	if s.db == nil {
		return
	}
	var rows []db.Automation
	s.db.Where("next_run_at IS NOT NULL AND next_run_at < ?", time.Now()).Find(&rows)
	zoneOf := s.zones()
	for i := range rows {
		s.db.Model(&rows[i]).Update("next_run_at", nextRun(&rows[i], time.Now(), zoneOf(rows[i].BotID)))
	}
}

// FireDue starts every enabled automation whose next firing is at or
// before now and moves it to its following slot. It returns how many started.
func (s *Service) FireDue(now time.Time) int {
	var due []db.Automation
	s.db.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).Find(&due)
	fired := 0
	zoneOf := s.zones()
	for i := range due {
		au := &due[i]
		next := nextRun(au, now, zoneOf(au.BotID))
		// Claim the slot: only the writer that moves next_run_at fires it.
		res := s.db.Model(&db.Automation{}).Where("id = ? AND next_run_at = ?", au.ID, au.NextRunAt).Update("next_run_at", next)
		if res.RowsAffected != 1 {
			continue
		}
		if _, err := s.fireAutomation(au); err != nil {
			log.Printf("automation %s (%s): %v", au.Name, au.ID, err)
			continue
		}
		fired++
	}
	return fired
}

var errAutomationBusy = errors.New("this automation is still running")

// fireAutomation starts one run of an automation in its log chat. A firing that
// lands while the previous run is live is skipped, never queued.
func (s *Service) fireAutomation(au *db.Automation) (string, error) {
	s.ensureAutomationChat(au)
	now := time.Now()
	runID, err := s.engine.StartIfIdle(run.Request{BotID: au.BotID, ChatID: au.ChatID, Text: au.Prompt, Origin: &run.Origin{Automation: au}})
	if errors.Is(err, run.ErrBusy) {
		s.db.Model(&db.Automation{}).Where("id = ?", au.ID).Updates(map[string]any{"last_status": "skipped", "last_run_at": now})
		return "", errAutomationBusy
	}
	if err != nil {
		return "", err
	}
	s.db.Model(&db.Automation{}).Where("id = ?", au.ID).Updates(map[string]any{"last_run_id": runID, "last_run_at": now, "last_status": ""})
	return runID, nil
}

// UpgradePinned moves pinned automations still on an earlier default prompt
// to the current one. A prompt the owner edited is theirs and stays.
func (s *Service) UpgradePinned() {
	for _, p := range s.pins {
		for _, old := range p.Replaces {
			s.db.Model(&db.Automation{}).Where("kind = ? AND prompt LIKE ?", p.Kind, old+"%").
				Update("prompt", strings.TrimSpace(p.Prompt))
		}
	}
}

// Pinned is a Bot's pinned automation of a kind (made if missing), or nil.
func (s *Service) Pinned(botID, kind string) *db.Automation {
	if p, _, ok := s.pin(kind); ok {
		s.ensurePinned(botID, p.Kind, p.Name, p.Prompt)
	}
	var au db.Automation
	if s.db.Where("bot_id = ? AND kind = ?", botID, kind).Limit(1).Find(&au).RowsAffected == 0 {
		return nil
	}
	return &au
}

// Ask hands the owner's words to a pinned automation: they join its live run,
// or start one in its log from a fresh context like any firing.
func (s *Service) Ask(botID, kind, text string) (runID, chatID string, err error) {
	au := s.Pinned(botID, kind)
	if au == nil {
		return "", "", errors.New("this Bot has no " + kind + " automation")
	}
	s.ensureAutomationChat(au)
	runID, err = s.engine.StartOrInject(botID, au.ChatID, text, run.Extra{}, &run.Origin{Automation: au})
	if err != nil {
		return "", "", err
	}
	s.db.Model(&db.Automation{}).Where("id = ?", au.ID).Updates(map[string]any{"last_run_id": runID, "last_run_at": time.Now(), "last_status": ""})
	return runID, au.ChatID, nil
}

// --- proto / RPC ---

func (s *Service) automationStatus(au *db.Automation) (string, bool) {
	running := au.ChatID != "" && s.engine.LiveRunID(au.BotID, au.ChatID) != ""
	if au.LastStatus != "" || au.LastRunID == "" {
		return au.LastStatus, running
	}
	var run db.Run
	s.db.Where("id = ?", au.LastRunID).Limit(1).Find(&run)
	return run.Status, running
}

func (s *Service) protoAutomation(au *db.Automation) *v1.Automation {
	status, running := s.automationStatus(au)
	return &v1.Automation{
		Id: au.ID, BotId: au.BotID, Name: au.Name, Prompt: au.Prompt, Schedule: au.Schedule,
		Enabled: au.Enabled, Kind: au.Kind, ChatId: au.ChatID,
		LastRunAt: textx.RFC3339(au.LastRunAt), NextRunAt: textx.RFC3339(au.NextRunAt), LastStatus: status,
		Running: running, CreatedBy: au.CreatedBy, CreatedAt: au.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Service) ownAutomation(ctx context.Context, botID, id string) (*db.Automation, error) {
	return access.OwnBotRow[db.Automation](ctx, s.db, botID, id, "automation")
}

func (s *Service) ListAutomations(ctx context.Context, req *connect.Request[v1.ListAutomationsRequest]) (*connect.Response[v1.ListAutomationsResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	out := &v1.ListAutomationsResponse{}
	rows := s.listAutomations(req.Msg.GetBotId())
	for i := range rows {
		out.Automations = append(out.Automations, s.protoAutomation(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateAutomation(ctx context.Context, req *connect.Request[v1.CreateAutomationRequest]) (*connect.Response[v1.Automation], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	m := req.Msg
	name, prompt, schedule, enabled := m.GetName(), m.GetPrompt(), m.GetSchedule(), m.GetEnabled()
	au, err := s.saveAutomation(m.GetBotId(), nil, automationPatch{name: &name, prompt: &prompt, schedule: &schedule, enabled: &enabled}, "user")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(s.protoAutomation(au)), nil
}

func (s *Service) UpdateAutomation(ctx context.Context, req *connect.Request[v1.UpdateAutomationRequest]) (*connect.Response[v1.Automation], error) {
	m := req.Msg
	au, err := s.ownAutomation(ctx, m.GetBotId(), m.GetId())
	if err != nil {
		return nil, err
	}
	name, prompt, schedule, enabled := m.GetName(), m.GetPrompt(), m.GetSchedule(), m.GetEnabled()
	au, err = s.saveAutomation(m.GetBotId(), au, automationPatch{name: &name, prompt: &prompt, schedule: &schedule, enabled: &enabled}, "")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(s.protoAutomation(au)), nil
}

func (s *Service) DeleteAutomation(ctx context.Context, req *connect.Request[v1.DeleteAutomationRequest]) (*connect.Response[v1.DeleteAutomationResponse], error) {
	au, err := s.ownAutomation(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.deleteAutomation(au); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.DeleteAutomationResponse{}), nil
}

// RunAutomation fires an automation now, outside its schedule.
func (s *Service) RunAutomation(ctx context.Context, req *connect.Request[v1.RunAutomationRequest]) (*connect.Response[v1.RunAutomationResponse], error) {
	au, err := s.ownAutomation(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	runID, err := s.fireAutomation(au)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.RunAutomationResponse{RunId: runID, ChatId: au.ChatID}), nil
}

// --- Bot tools ---

func (s *Service) automationLine(au *db.Automation, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s  %s", au.ID, au.Name)
	if s.isPinned(au.Kind) {
		fmt.Fprintf(&b, " [%s, pinned]", au.Kind)
	}
	sched := au.Schedule
	if sched == "" {
		sched = "none (never fires)"
	}
	fmt.Fprintf(&b, "\n  schedule: %s", sched)
	if !au.Enabled {
		b.WriteString(" — paused")
	}
	if au.NextRunAt != nil {
		fmt.Fprintf(&b, "; next: %s", zone.Stamp(*au.NextRunAt, loc))
	}
	if au.LastRunAt != nil {
		status, _ := s.automationStatus(au)
		fmt.Fprintf(&b, "; last: %s (%s)", zone.Stamp(*au.LastRunAt, loc), status)
	}
	p := au.Prompt
	if utf8.RuneCountInString(p) > 300 {
		p = string([]rune(p)[:300]) + "…"
	}
	fmt.Fprintf(&b, "\n  prompt: %s\n", strings.ReplaceAll(p, "\n", " "))
	return b.String()
}

func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any) (string, error) {
	action := map[string]string{
		"list_automations":  "list",
		"create_automation": "create",
		"update_automation": "update",
		"delete_automation": "delete",
	}[name]
	str := func(k string) *string {
		v, ok := args[k]
		if !ok || v == nil {
			return nil
		}
		s, _ := v.(string)
		return &s
	}
	var p automationPatch
	p.name, p.prompt, p.schedule = str("name"), str("prompt"), str("schedule")
	if v, ok := args["enabled"].(bool); ok {
		p.enabled = &v
	}
	// The approval slip names the automation, not a raw id.
	slip := map[string]any{}
	for k, v := range args {
		slip[k] = v
	}
	var target *db.Automation
	if action == "update" || action == "delete" {
		ref := str("automation")
		if ref == nil {
			return "", errors.New("automation required (id or name)")
		}
		au, err := s.findAutomation(bot.ID, *ref)
		if err != nil {
			return "", err
		}
		target = au
		slip["automation"] = au.Name
	}
	slipJSON, _ := json.Marshal(slip)
	if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Automations, action, string(slipJSON), ""); err != nil {
		return "", err
	}
	// Asked now, not read off bot: the zone may have moved during the run.
	loc := s.zones()(bot.ID)
	switch action {
	case "list":
		rows := s.listAutomations(bot.ID)
		var b strings.Builder
		fmt.Fprintf(&b, "Automations (cron in this Bot's time zone, %s):\n", loc)
		for i := range rows {
			b.WriteString(s.automationLine(&rows[i], loc))
		}
		return b.String(), nil
	case "create":
		au, err := s.saveAutomation(bot.ID, nil, p, "bot")
		if err != nil {
			return "", err
		}
		return "created\n" + s.automationLine(au, loc), nil
	case "update":
		if s.isPinned(target.Kind) {
			p.name = nil
		}
		au, err := s.saveAutomation(bot.ID, target, p, "")
		if err != nil {
			return "", err
		}
		return "updated\n" + s.automationLine(au, loc), nil
	case "delete":
		if err := s.deleteAutomation(target); err != nil {
			return "", err
		}
		return "deleted " + target.Name, nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// Prompt is the per-run system-prompt note when an automation started the
// run: its name and schedule, then the standing instructions for unattended
// runs.
func Prompt(au *db.Automation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This run is the automation “%s”", au.Name)
	if sched := au.Schedule; sched != "" {
		fmt.Fprintf(&b, " (schedule `%s`)", sched)
	}
	b.WriteString(".\n\n")
	b.WriteString(strings.TrimSpace(prompts.Automation))
	return b.String()
}
