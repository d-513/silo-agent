package app

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/command"
	"silo.agent/internal/app/dashboard"
	"silo.agent/internal/app/proactivity"
	"silo.agent/internal/app/quote"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/setup"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
	"silo.agent/internal/lang"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
	"silo.agent/internal/zone"
	"slices"
)

func (a *App) protoBot(b *db.Bot, running bool) *v1.Bot {
	connected := a.Hub.Connected(b.ID)
	return &v1.Bot{
		Id:              b.ID,
		SpaceId:         b.SpaceID,
		Name:            b.Name,
		Status:          DeriveStatus(connected, running, b.Status),
		LastTask:        b.LastTask,
		Crest:           int32(b.Crest),
		WorkerConnected: connected,
		Description:     b.Description,
		Soul:            b.Soul,
		AutoApprove:     b.AutoApprove,
		OutputPolicy:    b.OutputPolicy,
		Model:           b.Model,
		DashboardAsks:   a.Dashboard.Open(b.ID),
		DesktopPaused:   a.Pause.Paused(b.ID),
		Proactivity:     b.Proactivity,
		HeartbeatCustom: a.Proactivity.Custom(b.ID, b.Proactivity),
		Activity:        a.activity(b.ID),
		AuditUnseen:     int32(b.AuditUnseen),
		MailAddress:     a.Mail.Address(b.ID),
		TimeZone:        zone.Resolve(b.TimeZone).String(),
		TimeZonePinned:  b.TimeZonePinned,
		Language:        lang.Bot(b.Language),
	}
}

// viewBot is a Bot as one person sees it: their role, their unread Feed,
// and (on a Bot several people use) who else is on it.
func (a *App) viewBot(ctx context.Context, b *db.Bot) *v1.Bot {
	p := a.protoBot(b, a.live(ctx, b))
	if u := access.User(ctx); u != nil {
		p.Role = access.BotRole(a.DB, b.ID, u.ID)
		p.FeedUnread = a.Feed.Unread(b.ID, u.ID)
		if people := access.People(a.DB, b.ID); len(people) > 1 {
			for _, id := range people {
				p.People = append(p.People, &v1.BotPerson{UserId: id, Email: access.Email(a.DB, id), Role: access.BotRole(a.DB, b.ID, id), You: id == u.ID})
			}
		}
	}
	return p
}

// bot loads a Bot the caller may do what need says with (access.Bot).
func (a *App) bot(ctx context.Context, id string, need access.Need) (*db.Bot, error) {
	return access.Bot(ctx, a.DB, id, need)
}

// ListBots lists the Bots the caller is on: in one Workspace, or in all.
func (a *App) ListBots(ctx context.Context, req *connect.Request[v1.ListBotsRequest]) (*connect.Response[v1.ListBotsResponse], error) {
	u := access.User(ctx)
	q := a.DB.Joins("JOIN bot_access a ON a.bot_id = bots.id AND a.user_id = ?", u.ID)
	if id := req.Msg.GetSpaceId(); id != "" {
		if _, _, err := access.Space(ctx, a.DB, id, access.View); err != nil {
			return nil, err
		}
		q = q.Where("bots.space_id = ?", id)
	}
	var bots []db.Bot
	if err := q.Order("bots.created_at desc").Find(&bots).Error; err != nil {
		return nil, err
	}
	out := &v1.ListBotsResponse{}
	for i := range bots {
		p := a.viewBot(ctx, &bots[i])
		p.Soul = ""
		p.OutputPolicy = ""
		out.Bots = append(out.Bots, p)
	}
	// A Workspace's admins see that its other Bots exist, who is on them and
	// what they are granted: enough to delete one, nothing that opens it.
	if id := req.Msg.GetSpaceId(); id != "" && access.Rank(access.SpaceRole(a.DB, id, u.ID)) >= access.Admin {
		var others []db.Bot
		a.DB.Where("space_id = ? AND id NOT IN (?)", id, a.DB.Model(&db.BotAccess{}).Select("bot_id").Where("user_id = ?", u.ID)).
			Order("created_at desc").Find(&others)
		for _, b := range others {
			o := &v1.OtherBot{Id: b.ID, Name: b.Name, Crest: int32(b.Crest)}
			for _, p := range access.People(a.DB, b.ID) {
				o.People = append(o.People, access.Email(a.DB, p))
			}
			var conns, drives int64
			a.DB.Model(&db.BotConnector{}).Where("bot_id = ?", b.ID).Count(&conns)
			a.DB.Model(&db.BotDrive{}).Where("bot_id = ?", b.ID).Count(&drives)
			o.Grants = int32(conns + drives)
			out.Others = append(out.Others, o)
		}
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateBot(ctx context.Context, req *connect.Request[v1.CreateBotRequest]) (*connect.Response[v1.Bot], error) {
	u := access.User(ctx)
	// Where first: a request for someone else's Workspace learns nothing more.
	sp, err := a.Spaces.ForNewBot(ctx, req.Msg.GetSpaceId())
	if err != nil {
		return nil, err
	}
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name required"))
	}
	// Who else uses it, and what it may use, are checked before it exists:
	// nobody outside the Workspace, a guest only uses, and nobody's personal
	// sign-in reaches someone else.
	users := slices.DeleteFunc(slices.Clone(req.Msg.GetUserIds()), func(id string) bool { return id == u.ID })
	for _, uid := range users {
		if err := a.Spaces.CheckJoin("", sp.ID, uid, access.RoleUse); err != nil {
			return nil, err
		}
	}
	people := append([]string{u.ID}, users...)
	if err := a.Connectors.CheckNewBot(sp.ID, req.Msg.GetConnectorIds(), people); err != nil {
		return nil, err
	}
	if err := a.Drives.CheckNewBot(sp.ID, req.Msg.GetDriveIds(), people); err != nil {
		return nil, err
	}
	id := ids.New()
	crest := int(req.Msg.GetCrest())
	if crest < 0 || crest >= ids.CrestCount {
		crest = ids.Crest(name, id)
	}
	b := db.Bot{
		ID:          id,
		SpaceID:     sp.ID,
		CreatedBy:   u.ID,
		Name:        name,
		Description: clipDesc(req.Msg.GetDescription()),
		Soul:        defaultSoul,
		// The zone of the device that makes the Bot; a client that names
		// none, or one unknown here, gets the server's.
		TimeZone: zone.Resolve(req.Msg.GetTimeZone()).String(),
		// A Bot starts out writing in the language its maker reads Silo in.
		Language:  lang.UI(u.Language),
		Status:    "stopped",
		Crest:     crest,
		CreatedAt: time.Now(),
	}
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&b).Error; err != nil {
			return err
		}
		// Whoever makes a Bot manages it; the people picked use it.
		if err := tx.Create(&db.BotAccess{BotID: id, UserID: u.ID, Role: access.RoleManage, CreatedAt: b.CreatedAt}).Error; err != nil {
			return err
		}
		for _, uid := range users {
			if err := tx.Create(&db.BotAccess{BotID: id, UserID: uid, Role: access.RoleUse, CreatedAt: b.CreatedAt}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	main, err := a.mainChat(id, u.ID)
	if err != nil {
		return nil, err
	}
	access.Activity(a.DB, sp.ID, u.ID, access.BotCreated, b.Name, id, "")
	a.Skills.EnsureDefaults(id)
	a.Skills.EnableSpace(id, req.Msg.GetSkillsOff()...)
	a.Automations.EnsurePinned(id)
	a.Connectors.AttachDefaults(ctx, id)
	a.Connectors.GrantNew(id, req.Msg.GetConnectorIds())
	a.Drives.GrantNew(id, req.Msg.GetDriveIds())
	if err := a.ensureRunning(ctx, &b); err != nil {
		log.Printf("create start %s: %v", id, err)
	}
	if req.Msg.GetInterview() {
		if _, err := a.startInterview(id, main.ID); err != nil {
			log.Printf("create interview %s: %v", id, err)
		}
	}
	return connect.NewResponse(a.viewBot(ctx, &b)), nil
}

func (a *App) UpdateBot(ctx context.Context, req *connect.Request[v1.UpdateBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.bot(ctx, req.Msg.GetId(), access.Manage)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(req.Msg.GetName()); name != "" {
		b.Name = name
	}
	b.Description = clipDesc(req.Msg.GetDescription())
	b.Soul = req.Msg.GetSoul()
	b.AutoApprove = req.Msg.GetAutoApprove()
	if req.Msg.OutputPolicy != nil {
		b.OutputPolicy = textx.CapRunes(strings.TrimSpace(req.Msg.GetOutputPolicy()), outputPolicyMax)
	}
	model := strings.TrimSpace(req.Msg.GetModel())
	if model != "" {
		if _, _, err := llm.Parse(model); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if !llm.Allowed(model, a.cfg().Models) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("model is not in the allowed list"))
		}
	}
	b.Model = model
	rezone := false
	if req.Msg.TimeZone != nil {
		tz := strings.TrimSpace(req.Msg.GetTimeZone())
		if err := zone.Check(tz); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		rezone = tz != b.TimeZone
		b.TimeZone = tz
	}
	if req.Msg.TimeZonePinned != nil {
		b.TimeZonePinned = req.Msg.GetTimeZonePinned()
	}
	if req.Msg.Language != nil {
		if err := lang.Check(req.Msg.GetLanguage()); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		b.Language = req.Msg.GetLanguage()
	}
	level := ""
	if req.Msg.Proactivity != nil {
		l, err := proactivity.Normalize(req.Msg.GetProactivity())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		level = l
	}
	// proactivity is Proactivity.Set's to write, with the Heartbeat: a
	// whole-row save would undo a set_proactivity that landed meanwhile.
	if err := a.DB.Omit("proactivity").Save(b).Error; err != nil {
		return nil, err
	}
	if rezone {
		a.Automations.Rezone(b.ID)
	}
	// The same level again is a no-op, unless the Heartbeat was set by hand:
	// then choosing the level puts it back on the level's schedule.
	if level != "" && (level != b.Proactivity || a.Proactivity.Custom(b.ID, level)) {
		if _, _, err := a.Proactivity.Set(b.ID, level); err != nil {
			return nil, err
		}
		b.Proactivity = level
	}
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) GetBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.bot(ctx, req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) GetContainer(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Container], error) {
	b, err := a.bot(ctx, req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	running := a.live(ctx, b)
	out := &v1.Container{Running: running}
	if !running {
		return connect.NewResponse(out), nil
	}
	id := b.ContainerID
	if id == "" {
		id = dockerx.Name(b.ID)
	}
	st, err := a.Docker.Stats(ctx, id)
	if err != nil {
		if dockerx.IsNotFound(err) {
			return connect.NewResponse(out), nil
		}
		return nil, err
	}
	out.CpuPercent = st.CPUPercent
	out.MemUsed = st.MemUsed
	out.MemLimit = st.MemLimit
	return connect.NewResponse(out), nil
}

func (a *App) StartBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.bot(ctx, req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	if err := a.ensureRunning(ctx, b); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) StopBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	return a.takeDown(ctx, req.Msg.GetId(), a.haltBot)
}

func (a *App) ResetContainer(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	return a.takeDown(ctx, req.Msg.GetId(), a.destroyBot)
}

// takeDown runs down (halt the box, or remove it) on the user's Bot and returns
// the Bot as stopped.
func (a *App) takeDown(ctx context.Context, id string, down func(context.Context, *db.Bot)) (*connect.Response[v1.Bot], error) {
	b, err := a.bot(ctx, id, access.Manage)
	if err != nil {
		return nil, err
	}
	down(ctx, b)
	b.Status = "stopped"
	a.DB.Save(b)
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) DeleteBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.DeleteBotResponse], error) {
	b, err := a.bot(ctx, req.Msg.GetId(), access.Administer)
	if err != nil {
		return nil, err
	}
	a.dropBot(ctx, b)
	// After the drop, and on no Bot: the drop takes the Bot's own audit rows.
	access.Activity(a.DB, b.SpaceID, access.User(ctx).ID, access.BotDeleted, b.Name, "", "")
	return connect.NewResponse(&v1.DeleteBotResponse{}), nil
}

// dropBot deletes a Bot for good: its box, everything it has in the database,
// and its files.
func (a *App) dropBot(ctx context.Context, b *db.Bot) {
	a.destroyBot(ctx, b)
	var runs []db.Run
	a.DB.Where("bot_id = ?", b.ID).Find(&runs)
	for _, r := range runs {
		a.DB.Where("run_id = ?", r.ID).Delete(&db.RunEvent{})
	}
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Run{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.LLMLog{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Memory{})
	a.Memory.DropDigest(b.ID)
	a.Knowledge.Drop(b.ID)
	a.Changes.Drop(b.ID)
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Chat{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Automation{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.FeedPost{})
	a.Dashboard.Drop(b.ID)
	a.Setup.Drop(b.ID)
	a.Tunnels.Drop(b.ID)
	a.Artifacts.Drop(b.ID)
	a.Mail.Drop(b.ID)
	_ = a.Pause.Drop(b.ID)
	a.DB.Where("chat_id IN (?)", a.DB.Model(&db.Chat{}).Select("id").Where("bot_id = ?", b.ID)).Delete(&db.TaskItem{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Subagent{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Secret{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Rule{})
	var bcs []db.BotConnector
	a.DB.Where("bot_id = ?", b.ID).Find(&bcs)
	for i := range bcs {
		a.Connectors.DropStdio(&bcs[i])
	}
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.BotConnector{})
	a.Connectors.ReconcileStdio()
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.BotSkill{})
	for _, ch := range a.Channels.All(b.ID) {
		c := ch
		a.Channels.Delete(&c)
	}
	a.Drives.Drop(ctx, b.ID)
	a.Egress.Drop(ctx, b.ID)
	a.Spaces.DropBot(b.ID)
	a.DB.Delete(b)
	if dir := a.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "bots", b.ID))
	}
}

func (a *App) Send(ctx context.Context, req *connect.Request[v1.SendRequest]) (*connect.Response[v1.SendResponse], error) {
	b, err := a.bot(ctx, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	// Whose chat first: only its author writes in it.
	if id := req.Msg.GetChatId(); id != "" {
		if _, err := a.chat(ctx, b.ID, id, access.Write); err != nil {
			return nil, err
		}
	}
	text := req.Msg.GetText()
	ex := run.Extra{Files: cleanAttachments(req.Msg.GetAttachments()), Quotes: cleanQuotes(req.Msg.GetQuotes())}
	if text == "" && len(ex.Files) == 0 && len(ex.Quotes) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("text required"))
	}
	chatID := req.Msg.GetChatId()
	// A slash command runs instead of a message; one sent with files or
	// quotes is a message (nothing would carry them).
	if p := a.Slash.Parse(text); len(ex.Files) == 0 && len(ex.Quotes) == 0 {
		if p.Def != nil {
			return a.sendCommand(ctx, b, chatID, p, req.Msg.GetConfirmed())
		}
		text = p.Text
	}
	var ch *db.Chat
	made := false
	if chatID == "" && req.Msg.GetNewChat() {
		// The console's New chat page: the chat begins with this message.
		model, merr := a.chatModel(req.Msg.GetModel())
		if merr != nil {
			return nil, merr
		}
		level := strings.ToLower(strings.TrimSpace(req.Msg.GetThinking()))
		if !llm.ValidThinking(level) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown thinking level"))
		}
		ch = &db.Chat{ID: ids.New(), BotID: b.ID, UserID: access.User(ctx).ID, Title: "New chat", Model: model, Thinking: level, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := a.DB.Create(ch).Error; err != nil {
			return nil, err
		}
		made = true
	} else if chatID == "" {
		main, err := a.mainChat(b.ID, access.User(ctx).ID)
		if err != nil {
			return nil, err
		}
		ch = main
	} else {
		owned, cerr := a.chat(ctx, b.ID, chatID, access.Write)
		if cerr != nil {
			return nil, cerr
		}
		ch = owned
	}
	if err := writableChat(ch); err != nil {
		return nil, err
	}
	a.followZone(b, req.Msg.GetTimeZone())
	runID, err := a.StartOrInject(b.ID, ch.ID, text, ex, nil)
	if err != nil {
		if made {
			a.DB.Delete(&db.Chat{}, "id = ?", ch.ID)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.SendResponse{RunId: runID, ChatId: ch.ID}), nil
}

// sendCommand runs a command typed in a chat's composer.
func (a *App) sendCommand(ctx context.Context, b *db.Bot, chatID string, p command.Parsed, confirmed bool) (*connect.Response[v1.SendResponse], error) {
	out := &v1.SendResponse{Command: p.Def.Name}
	if chatID != "" {
		ch, err := a.chat(ctx, b.ID, chatID, access.Write)
		if err != nil {
			return nil, err
		}
		if err := writableChat(ch); err != nil {
			return nil, err
		}
	} else if p.Def.Name != "new" && p.Def.Name != "help" {
		// The New chat page has no conversation to act on yet.
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("/"+p.Def.Name+" needs a chat; send a message first"))
	}
	res, err := a.runCommand(ctx, p.Def, command.Call{BotID: b.ID, ChatID: chatID, Args: p.Args, Surface: command.Web, Confirmed: confirmed})
	if err != nil {
		return nil, err
	}
	out.Notice, out.Confirm, out.RunId, out.NewChat, out.Value = res.Notice, res.Confirm, res.RunID, res.NewChat, res.Value
	out.ChatId = chatID
	if res.ChatID != "" {
		out.ChatId = res.ChatID
	}
	return connect.NewResponse(out), nil
}

const maxAttachments = 20

// cleanAttachments normalizes client-supplied upload metadata into
// workspace-relative paths. The bytes already landed via PutFile; this is
// prompt metadata, so bad entries are dropped rather than rejected.
func cleanAttachments(in []*v1.Attachment) []*v1.Attachment {
	out := make([]*v1.Attachment, 0, len(in))
	for _, at := range in {
		if at == nil {
			continue
		}
		p := workspace.Rel(at.GetPath())
		if p == "" {
			continue
		}
		name := strings.TrimSpace(at.GetName())
		if name == "" {
			name = filepath.Base(p)
		}
		out = append(out, &v1.Attachment{Name: name, Path: p, Size: at.GetSize(), Mime: at.GetMime()})
		if len(out) == maxAttachments {
			break
		}
	}
	return out
}

func (a *App) StopRun(ctx context.Context, req *connect.Request[v1.StopRunRequest]) (*connect.Response[v1.StopRunResponse], error) {
	b, err := a.bot(ctx, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	chatID := req.Msg.GetChatId()
	if chatID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("chat_id required"))
	}
	if _, err := a.chat(ctx, b.ID, chatID, access.Read); err != nil {
		return nil, err
	}
	a.stopChat(b.ID, chatID)
	// Stop in a lead chat halts its subagents too, without waking it.
	a.stopSubagents(b.ID, chatID)
	return connect.NewResponse(&v1.StopRunResponse{}), nil
}

func (a *App) StreamRun(ctx context.Context, req *connect.Request[v1.StreamRunRequest], stream *connect.ServerStream[v1.RunEvent]) error {
	if _, err := a.bot(ctx, req.Msg.GetBotId(), access.Use); err != nil {
		return err
	}
	botID := req.Msg.GetBotId()
	chatID := req.Msg.GetChatId()
	after := req.Msg.GetAfterEventId()
	if chatID != "" {
		if _, err := a.chat(ctx, botID, chatID, access.Read); err != nil {
			return err
		}
	}
	// The whole Bot's stream carries only the chats this person reads.
	readable := a.chatReader(botID, access.User(ctx).ID)
	// The stream ends with the viewer's access to the Bot.
	lost, stop := a.Gone.Lost(botID, access.User(ctx).ID)
	defer stop()
	ch, unsub := a.Bus.Subscribe(botID)
	defer unsub()
	runQ := a.DB.Model(&db.Run{}).Select("id").Where("bot_id = ?", botID)
	if chatID != "" {
		runQ = runQ.Where("chat_id = ?", chatID)
	}
	var rows []db.RunEvent
	// Replay-only events are the model's context, not the thread.
	q := a.DB.Where("run_id IN (?) AND kind NOT IN ?", runQ, internalKinds)
	// A reconnect reads only what came after the client's last event. An
	// anchor that is gone (an edit truncated it) replays the newest page
	// again, so the client does not stall on an open run.
	anchored := false
	if after != "" {
		var anchor db.RunEvent
		if a.DB.Select("seq").Where("id = ? AND run_id IN (?)", after, runQ).Limit(1).Find(&anchor).RowsAffected > 0 {
			q = q.Where("seq > ?", anchor.Seq)
			anchored = true
		}
	}
	if !anchored && chatID != "" {
		// A first look gets the newest turns; older ones are a page away.
		if start, more := a.threadPage(runQ, 0); more {
			q = q.Where("seq >= ?", start)
			if err := stream.Send(&v1.RunEvent{ChatId: chatID, Kind: earlierKind}); err != nil {
				return err
			}
		}
	}
	if err := q.Order("seq").Find(&rows).Error; err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for i := range rows {
		seen[rows[i].ID] = struct{}{}
		if err := stream.Send(a.threadEvent(&rows[i], chatID)); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-lost:
			return connect.NewError(connect.CodeNotFound, errors.New(access.NoBot))
		case ev, ok := <-ch:
			if !ok {
				return connect.NewError(connect.CodeAborted, errors.New("stream overflow"))
			}
			// Only this chat's events; an orphan (no chat) belongs to none.
			if chatID != "" && ev.GetChatId() != chatID {
				continue
			}
			if chatID == "" && ev.GetChatId() != "" && !readable(ev.GetChatId()) {
				continue
			}
			if ev.GetId() != "" {
				if _, dup := seen[ev.GetId()]; dup {
					continue
				}
				seen[ev.GetId()] = struct{}{}
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

const descMax = 400

func clipDesc(s string) string {
	return textx.TruncateUTF8(strings.TrimSpace(s), descMax)
}

// threadEvent is a persisted event as a viewer receives it. A card (or a
// quote of one) carries the card as it stands now.
func (a *App) threadEvent(r *db.RunEvent, chatID string) *v1.RunEvent {
	meta := decodeUserMeta(r.Meta)
	out := &v1.RunEvent{Id: r.ID, RunId: r.RunID, ChatId: chatID, Kind: r.Kind, Author: r.Author, Body: textx.ValidUTF8(r.Body), Tool: textx.ValidUTF8(r.Tool), Attachments: v1Attachments(meta.Files), Quotes: v1Quotes(meta.Quotes), Quoted: quote.Proto(r), CreatedAt: r.CreatedAt.Format(time.RFC3339)}
	switch {
	case r.Kind == dashboard.CardKind:
		out.Card = a.Dashboard.CardProto(r.Body)
	case r.Kind == quote.Kind:
		out.Card = a.Dashboard.CardProto(quote.CardOf(r))
	case r.Kind == setup.Kind:
		out.SetupOffer = a.Setup.Proto(r.Body)
	}
	return out
}

// Proactive reports whether the Bot may write into its main chat unasked.
func (a *App) Proactive(botID string) bool { return a.Proactivity.Proactive(botID) }

// A thread is sent a page at a time: the newest pageTurns of the human's
// messages and everything after, then the same again before that on request.
// A conversation that runs for months (a Bot's main chat) would otherwise be
// replayed whole every time it is opened.
const pageTurns = 20

// earlierKind is a transient event StreamRun opens with when the thread has
// more before what it replays.
const earlierKind = "earlier"

// threadPage is where the page of the thread before beforeSeq (0: the end)
// starts, and whether anything comes before it. A page starts on a human
// message, so it never opens in the middle of a turn.
func (a *App) threadPage(runQ any, beforeSeq int64) (start int64, more bool) {
	q := a.DB.Model(&db.RunEvent{}).Select("seq").Where("run_id IN (?) AND kind = ?", runQ, "user")
	if beforeSeq > 0 {
		q = q.Where("seq < ?", beforeSeq)
	}
	var seqs []int64
	q.Order("seq DESC").Limit(pageTurns).Pluck("seq", &seqs)
	if len(seqs) < pageTurns {
		return 0, false
	}
	start = seqs[len(seqs)-1]
	var n int64
	a.DB.Model(&db.RunEvent{}).Where("run_id IN (?) AND kind NOT IN ? AND seq < ?", runQ, internalKinds, start).Limit(1).Count(&n)
	return start, n > 0
}

// ListChatEvents is the page of a thread before the oldest event the client
// has.
func (a *App) ListChatEvents(ctx context.Context, req *connect.Request[v1.ListChatEventsRequest]) (*connect.Response[v1.ListChatEventsResponse], error) {
	b, err := a.bot(ctx, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	chatID := req.Msg.GetChatId()
	if _, err := a.chat(ctx, b.ID, chatID, access.Read); err != nil {
		return nil, err
	}
	runQ := a.DB.Model(&db.Run{}).Select("id").Where("bot_id = ? AND chat_id = ?", b.ID, chatID)
	var anchor db.RunEvent
	if a.DB.Select("seq").Where("id = ? AND run_id IN (?)", req.Msg.GetBeforeEventId(), runQ).Limit(1).Find(&anchor).RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("that message is gone; reload the chat"))
	}
	start, more := a.threadPage(runQ, anchor.Seq)
	var rows []db.RunEvent
	if err := a.DB.Where("run_id IN (?) AND kind NOT IN ? AND seq >= ? AND seq < ?", runQ, internalKinds, start, anchor.Seq).Order("seq").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := &v1.ListChatEventsResponse{More: more}
	for i := range rows {
		out.Events = append(out.Events, a.threadEvent(&rows[i], chatID))
	}
	return connect.NewResponse(out), nil
}
