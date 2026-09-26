package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/robfig/cron/v3"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/prompts"
	"silo.agent/internal/security"
)

const (
	automationHeartbeat = "heartbeat"
	automationCustom    = "custom"

	// automationTick is how often the scheduler looks for due automations.
	automationTick = 20 * time.Second
	// automationMinGap is the shortest allowed interval between two firings,
	// so a typo such as "* * * * *" cannot burn a model call every minute.
	automationMinGap = 5 * time.Minute
	automationMax    = 50
	automationName   = 80
	automationPrompt = 8000
)

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
// or has no schedule.
func nextRun(au *db.Automation, from time.Time) *time.Time {
	if !au.Enabled {
		return nil
	}
	sched, err := parseSchedule(au.Schedule)
	if err != nil || sched == nil {
		return nil
	}
	t := sched.Next(from)
	if t.IsZero() {
		return nil
	}
	return &t
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = strings.TrimSpace(string([]rune(s)[:n]))
	}
	return s
}

// --- store ---

// ensureHeartbeat creates the pinned Heartbeat for a Bot if it has none. It is
// created without a schedule, so it never fires until someone sets one.
func (a *App) ensureHeartbeat(botID string) {
	var n int64
	a.DB.Model(&db.Automation{}).Where("bot_id = ? AND kind = ?", botID, automationHeartbeat).Count(&n)
	if n > 0 {
		return
	}
	now := time.Now()
	au := db.Automation{
		ID: ids.New(), BotID: botID, Kind: automationHeartbeat, Name: "Heartbeat",
		Prompt: strings.TrimSpace(prompts.Heartbeat), Enabled: true, CreatedBy: "user",
		CreatedAt: now, UpdatedAt: now,
	}
	a.ensureAutomationChat(&au)
	if err := a.DB.Create(&au).Error; err != nil {
		log.Printf("heartbeat %s: %v", botID, err)
	}
}

// ensureAutomationChat gives an automation its hidden log Chat.
func (a *App) ensureAutomationChat(au *db.Automation) {
	if au.ChatID != "" {
		var n int64
		a.DB.Model(&db.Chat{}).Where("id = ?", au.ChatID).Count(&n)
		if n > 0 {
			return
		}
	}
	now := time.Now()
	c := db.Chat{ID: ids.New(), BotID: au.BotID, AutomationID: au.ID, Title: au.Name, CreatedAt: now, UpdatedAt: now}
	if err := a.DB.Create(&c).Error; err != nil {
		log.Printf("automation chat %s: %v", au.ID, err)
		return
	}
	au.ChatID = c.ID
	if au.CreatedAt.IsZero() {
		return // not persisted yet; the caller's Create writes chat_id
	}
	a.DB.Model(&db.Automation{}).Where("id = ?", au.ID).Update("chat_id", c.ID)
}

func (a *App) listAutomations(botID string) []db.Automation {
	a.ensureHeartbeat(botID)
	var rows []db.Automation
	// Heartbeat is pinned first; the rest newest first.
	a.DB.Where("bot_id = ?", botID).
		Order("CASE WHEN kind = 'heartbeat' THEN 0 ELSE 1 END").
		Order("created_at desc").Find(&rows)
	return rows
}

// findAutomation resolves an id or a (case-insensitive) name within a Bot.
func (a *App) findAutomation(botID, ref string) (*db.Automation, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("automation required")
	}
	var au db.Automation
	a.DB.Where("bot_id = ? AND id = ?", botID, ref).Limit(1).Find(&au)
	if au.ID == "" {
		a.DB.Where("bot_id = ? AND lower(name) = lower(?)", botID, ref).Limit(1).Find(&au)
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
func (a *App) saveAutomation(botID string, au *db.Automation, p automationPatch, createdBy string) (*db.Automation, error) {
	creating := au == nil
	if creating {
		var n int64
		a.DB.Model(&db.Automation{}).Where("bot_id = ?", botID).Count(&n)
		if n >= automationMax {
			return nil, fmt.Errorf("a Bot can have at most %d automations", automationMax)
		}
		au = &db.Automation{ID: ids.New(), BotID: botID, Kind: automationCustom, Enabled: true, CreatedBy: createdBy}
	}
	if p.name != nil {
		au.Name = clipRunes(*p.name, automationName)
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
	if au.Kind == automationHeartbeat {
		au.Name = "Heartbeat"
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
	a.DB.Where("bot_id = ? AND lower(name) = lower(?) AND id <> ?", botID, au.Name, au.ID).Limit(1).Find(&clash)
	if clash.ID != "" {
		return nil, fmt.Errorf("an automation named %q already exists", au.Name)
	}
	now := time.Now()
	au.NextRunAt = nextRun(au, now)
	au.UpdatedAt = now
	if creating {
		au.CreatedAt = now
		a.ensureAutomationChat(au)
		if err := a.DB.Create(au).Error; err != nil {
			return nil, err
		}
		return au, nil
	}
	if err := a.DB.Save(au).Error; err != nil {
		return nil, err
	}
	a.DB.Model(&db.Chat{}).Where("id = ?", au.ChatID).Update("title", au.Name)
	return au, nil
}

func (a *App) deleteAutomation(au *db.Automation) error {
	if au.Kind == automationHeartbeat {
		return errors.New("the Heartbeat is pinned; clear its schedule or pause it instead")
	}
	if au.ChatID != "" {
		a.stopChatLive(au.BotID, au.ChatID)
		a.dropChat(au.ChatID)
	}
	return a.DB.Delete(au).Error
}

// dropChat deletes a chat with its runs and events.
func (a *App) dropChat(chatID string) {
	a.DB.Where("run_id IN (?)", a.DB.Model(&db.Run{}).Select("id").Where("chat_id = ?", chatID)).Delete(&db.RunEvent{})
	a.DB.Where("chat_id = ?", chatID).Delete(&db.Run{})
	a.DB.Where("id = ?", chatID).Delete(&db.Chat{})
}

// --- scheduler ---

// rescheduleAutomations recomputes every pending firing from now. Firings missed
// while the Control Plane was down are skipped, not backfilled.
func (a *App) rescheduleAutomations() {
	if a.DB == nil {
		return
	}
	var rows []db.Automation
	a.DB.Where("next_run_at IS NOT NULL AND next_run_at < ?", time.Now()).Find(&rows)
	for i := range rows {
		a.DB.Model(&rows[i]).Update("next_run_at", nextRun(&rows[i], time.Now()))
	}
}

func (a *App) automationLoop(stop <-chan struct{}) {
	t := time.NewTicker(automationTick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			a.FireDueAutomations(now)
		}
	}
}

// FireDueAutomations starts every enabled automation whose next firing is at or
// before now and moves it to its following slot. It returns how many started.
func (a *App) FireDueAutomations(now time.Time) int {
	var due []db.Automation
	a.DB.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).Find(&due)
	fired := 0
	for i := range due {
		au := &due[i]
		next := nextRun(au, now)
		// Claim the slot: only the writer that moves next_run_at fires it.
		res := a.DB.Model(&db.Automation{}).Where("id = ? AND next_run_at = ?", au.ID, au.NextRunAt).Update("next_run_at", next)
		if res.RowsAffected != 1 {
			continue
		}
		if _, err := a.fireAutomation(au); err != nil {
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
func (a *App) fireAutomation(au *db.Automation) (string, error) {
	a.ensureAutomationChat(au)
	a.convMu.Lock()
	defer a.convMu.Unlock()
	now := time.Now()
	if a.liveRunID(au.BotID, au.ChatID) != "" {
		a.DB.Model(&db.Automation{}).Where("id = ?", au.ID).Updates(map[string]any{"last_status": "skipped", "last_run_at": now})
		return "", errAutomationBusy
	}
	runID, err := a.startRun(runRequest{botID: au.BotID, chatID: au.ChatID, text: au.Prompt, origin: &runOrigin{automation: au}})
	if err != nil {
		return "", err
	}
	a.DB.Model(&db.Automation{}).Where("id = ?", au.ID).Updates(map[string]any{"last_run_id": runID, "last_run_at": now, "last_status": ""})
	return runID, nil
}

// --- proto / RPC ---

func rfc3339(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func (a *App) automationStatus(au *db.Automation) (string, bool) {
	running := au.ChatID != "" && a.liveRunID(au.BotID, au.ChatID) != ""
	if au.LastStatus != "" || au.LastRunID == "" {
		return au.LastStatus, running
	}
	var run db.Run
	a.DB.Where("id = ?", au.LastRunID).Limit(1).Find(&run)
	return run.Status, running
}

func (a *App) protoAutomation(au *db.Automation) *v1.Automation {
	status, running := a.automationStatus(au)
	return &v1.Automation{
		Id: au.ID, BotId: au.BotID, Name: au.Name, Prompt: au.Prompt, Schedule: au.Schedule,
		Enabled: au.Enabled, Kind: au.Kind, ChatId: au.ChatID,
		LastRunAt: rfc3339(au.LastRunAt), NextRunAt: rfc3339(au.NextRunAt), LastStatus: status,
		Running: running, CreatedBy: au.CreatedBy, CreatedAt: au.CreatedAt.Format(time.RFC3339),
	}
}

func (a *App) ownAutomation(ctx context.Context, botID, id string) (*db.Automation, error) {
	if _, err := a.ownBot(ctx, botID); err != nil {
		return nil, err
	}
	var au db.Automation
	a.DB.Where("bot_id = ? AND id = ?", botID, id).Limit(1).Find(&au)
	if au.ID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("automation not found"))
	}
	return &au, nil
}

func (a *App) ListAutomations(ctx context.Context, req *connect.Request[v1.ListAutomationsRequest]) (*connect.Response[v1.ListAutomationsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	out := &v1.ListAutomationsResponse{}
	rows := a.listAutomations(req.Msg.GetBotId())
	for i := range rows {
		out.Automations = append(out.Automations, a.protoAutomation(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateAutomation(ctx context.Context, req *connect.Request[v1.CreateAutomationRequest]) (*connect.Response[v1.Automation], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	m := req.Msg
	name, prompt, schedule, enabled := m.GetName(), m.GetPrompt(), m.GetSchedule(), m.GetEnabled()
	au, err := a.saveAutomation(m.GetBotId(), nil, automationPatch{name: &name, prompt: &prompt, schedule: &schedule, enabled: &enabled}, "user")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(a.protoAutomation(au)), nil
}

func (a *App) UpdateAutomation(ctx context.Context, req *connect.Request[v1.UpdateAutomationRequest]) (*connect.Response[v1.Automation], error) {
	m := req.Msg
	au, err := a.ownAutomation(ctx, m.GetBotId(), m.GetId())
	if err != nil {
		return nil, err
	}
	name, prompt, schedule, enabled := m.GetName(), m.GetPrompt(), m.GetSchedule(), m.GetEnabled()
	au, err = a.saveAutomation(m.GetBotId(), au, automationPatch{name: &name, prompt: &prompt, schedule: &schedule, enabled: &enabled}, "")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(a.protoAutomation(au)), nil
}

func (a *App) DeleteAutomation(ctx context.Context, req *connect.Request[v1.DeleteAutomationRequest]) (*connect.Response[v1.DeleteAutomationResponse], error) {
	au, err := a.ownAutomation(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := a.deleteAutomation(au); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.DeleteAutomationResponse{}), nil
}

// RunAutomation fires an automation now, outside its schedule.
func (a *App) RunAutomation(ctx context.Context, req *connect.Request[v1.RunAutomationRequest]) (*connect.Response[v1.RunAutomationResponse], error) {
	au, err := a.ownAutomation(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	runID, err := a.fireAutomation(au)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.RunAutomationResponse{RunId: runID, ChatId: au.ChatID}), nil
}

// --- Bot tools ---

func automationLine(a *App, au *db.Automation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s  %s", au.ID, au.Name)
	if au.Kind == automationHeartbeat {
		b.WriteString(" [heartbeat, pinned]")
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
		fmt.Fprintf(&b, "; next: %s", au.NextRunAt.Local().Format("Mon 2006-01-02 15:04 MST"))
	}
	if au.LastRunAt != nil {
		status, _ := a.automationStatus(au)
		fmt.Fprintf(&b, "; last: %s (%s)", au.LastRunAt.Local().Format("Mon 2006-01-02 15:04 MST"), status)
	}
	p := au.Prompt
	if utf8.RuneCountInString(p) > 300 {
		p = string([]rune(p)[:300]) + "…"
	}
	fmt.Fprintf(&b, "\n  prompt: %s\n", strings.ReplaceAll(p, "\n", " "))
	return b.String()
}

func (a *App) automationTool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any) (string, error) {
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
		au, err := a.findAutomation(bot.ID, *ref)
		if err != nil {
			return "", err
		}
		target = au
		slip["automation"] = au.Name
	}
	slipJSON, _ := json.Marshal(slip)
	if _, err := a.authorizeAction(ctx, bot, runID, security.Automations, action, string(slipJSON), ""); err != nil {
		return "", err
	}
	switch action {
	case "list":
		rows := a.listAutomations(bot.ID)
		var b strings.Builder
		b.WriteString("Automations (cron in the machine's local time):\n")
		for i := range rows {
			b.WriteString(automationLine(a, &rows[i]))
		}
		return b.String(), nil
	case "create":
		au, err := a.saveAutomation(bot.ID, nil, p, "bot")
		if err != nil {
			return "", err
		}
		return "created\n" + automationLine(a, au), nil
	case "update":
		if target.Kind == automationHeartbeat {
			p.name = nil
		}
		au, err := a.saveAutomation(bot.ID, target, p, "")
		if err != nil {
			return "", err
		}
		return "updated\n" + automationLine(a, au), nil
	case "delete":
		if err := a.deleteAutomation(target); err != nil {
			return "", err
		}
		return "deleted " + target.Name, nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func isAutomationTool(name string) bool {
	switch name {
	case "list_automations", "create_automation", "update_automation", "delete_automation":
		return true
	}
	return false
}

// automationSections adds the per-run note when an automation started the run.
func (a *App) automationSections(pc promptContext) []promptSection {
	if pc.automation == nil {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This run is the automation “%s”", pc.automation.Name)
	if s := pc.automation.Schedule; s != "" {
		fmt.Fprintf(&b, " (schedule `%s`)", s)
	}
	b.WriteString(".\n\n")
	b.WriteString(strings.TrimSpace(prompts.Automation))
	return []promptSection{{title: "This run", body: b.String(), trailing: true}}
}
