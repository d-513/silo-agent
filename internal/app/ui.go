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
	"silo.agent/internal/auth"
	"silo.agent/internal/catalog"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
)

func (a *App) SignIn(ctx context.Context, req *connect.Request[v1.SignInRequest]) (*connect.Response[v1.SignInResponse], error) {
	email := req.Msg.GetEmail()
	pass := req.Msg.GetPassword()
	if email == "" || pass == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("email and password required"))
	}
	w := httpRW(ctx)
	var count int64
	a.DB.Model(&db.User{}).Count(&count)
	var u db.User
	if count == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("no users; set bootstrap.email and bootstrap.password in silo.yaml"))
	}
	if err := a.DB.First(&u, "email = ?", email).Error; err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}
	if !auth.CheckPassword(u.PasswordHash, pass) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}
	if err := auth.NewSession(a.DB, u.ID, w); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.SignInResponse{
		User: protoUser(&u),
	}), nil
}

func (a *App) SignOut(ctx context.Context, _ *connect.Request[v1.SignOutRequest]) (*connect.Response[v1.SignOutResponse], error) {
	auth.ClearSession(httpRW(ctx))
	return connect.NewResponse(&v1.SignOutResponse{}), nil
}

func protoUser(u *db.User) *v1.User {
	return &v1.User{Id: u.ID, Email: u.Email, Admin: u.Admin}
}

func (a *App) Me(ctx context.Context, _ *connect.Request[v1.MeRequest]) (*connect.Response[v1.MeResponse], error) {
	u := currentUser(ctx)
	return connect.NewResponse(&v1.MeResponse{User: protoUser(u)}), nil
}

func requireAdmin(ctx context.Context) error {
	u := currentUser(ctx)
	if u == nil || !u.Admin {
		return connect.NewError(connect.CodePermissionDenied, errors.New("admin only"))
	}
	return nil
}

func (a *App) protoBot(b *db.Bot, running bool) *v1.Bot {
	connected := a.Hub.Connected(b.ID)
	return &v1.Bot{
		Id:              b.ID,
		Name:            b.Name,
		Status:          DeriveStatus(connected, running, b.Status),
		LastTask:        b.LastTask,
		Crest:           int32(b.Crest),
		WorkerConnected: connected,
		Description:     b.Description,
		Soul:            b.Soul,
		Memory:          b.Memory,
	}
}

func (a *App) viewBot(ctx context.Context, b *db.Bot) *v1.Bot {
	return a.protoBot(b, a.live(ctx, b))
}

func (a *App) ownBot(ctx context.Context, id string) (*db.Bot, error) {
	u := currentUser(ctx)
	var b db.Bot
	if err := a.DB.First(&b, "id = ? AND user_id = ?", id, u.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, err
	}
	return &b, nil
}

func (a *App) ListBots(ctx context.Context, _ *connect.Request[v1.ListBotsRequest]) (*connect.Response[v1.ListBotsResponse], error) {
	u := currentUser(ctx)
	var bots []db.Bot
	if err := a.DB.Where("user_id = ?", u.ID).Order("created_at desc").Find(&bots).Error; err != nil {
		return nil, err
	}
	out := &v1.ListBotsResponse{}
	for i := range bots {
		p := a.viewBot(ctx, &bots[i])
		p.Soul = ""
		p.Memory = ""
		out.Bots = append(out.Bots, p)
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateBot(ctx context.Context, req *connect.Request[v1.CreateBotRequest]) (*connect.Response[v1.Bot], error) {
	u := currentUser(ctx)
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name required"))
	}
	id := ids.New()
	crest := int(req.Msg.GetCrest())
	if crest < 0 || crest >= ids.CrestCount {
		crest = ids.Crest(name, id)
	}
	b := db.Bot{
		ID:          id,
		UserID:      u.ID,
		Name:        name,
		Description: clipDesc(req.Msg.GetDescription()),
		Soul:        defaultSoul,
		Status:      "stopped",
		Crest:       crest,
		CreatedAt:   time.Now(),
	}
	if err := a.DB.Create(&b).Error; err != nil {
		return nil, err
	}
	_ = a.DB.Create(&db.Chat{ID: ids.New(), BotID: id, Title: "New chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error
	a.ensureDefaultSkill(id)
	if err := a.ensureRunning(ctx, &b); err != nil {
		log.Printf("create start %s: %v", id, err)
	}
	return connect.NewResponse(a.viewBot(ctx, &b)), nil
}

func (a *App) UpdateBot(ctx context.Context, req *connect.Request[v1.UpdateBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(req.Msg.GetName()); name != "" {
		b.Name = name
	}
	b.Description = clipDesc(req.Msg.GetDescription())
	b.Soul = req.Msg.GetSoul()
	b.Memory = req.Msg.GetMemory()
	if err := a.DB.Save(b).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) GetBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) GetContainer(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Container], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
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
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := a.ensureRunning(ctx, b); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) StopBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	a.haltBot(ctx, b)
	b.Status = "stopped"
	a.DB.Save(b)
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) ResetContainer(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	a.destroyBot(ctx, b)
	b.Status = "stopped"
	a.DB.Save(b)
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}

func (a *App) DeleteBot(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.DeleteBotResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	a.destroyBot(ctx, b)
	var runs []db.Run
	a.DB.Where("bot_id = ?", b.ID).Find(&runs)
	for _, r := range runs {
		a.DB.Where("run_id = ?", r.ID).Delete(&db.RunEvent{})
	}
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Run{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Chat{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Secret{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.Rule{})
	var bcs []db.BotConnector
	a.DB.Where("bot_id = ?", b.ID).Find(&bcs)
	for _, bc := range bcs {
		a.dropMCP(bc.ID)
	}
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.BotConnector{})
	a.DB.Where("bot_id = ?", b.ID).Delete(&db.BotSkill{})
	a.DB.Where("bot_id = ? AND kind = ?", b.ID, catalog.KindCustom).Delete(&db.Connector{})
	a.DB.Delete(b)
	if a.Cfg != nil && a.Cfg.DataDir != "" {
		_ = os.RemoveAll(filepath.Join(a.Cfg.DataDir, "bots", b.ID))
	}
	return connect.NewResponse(&v1.DeleteBotResponse{}), nil
}

func (a *App) Send(ctx context.Context, req *connect.Request[v1.SendRequest]) (*connect.Response[v1.SendResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	text := req.Msg.GetText()
	if text == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("text required"))
	}
	chatID := req.Msg.GetChatId()
	var ch *db.Chat
	if chatID == "" {
		ch = a.backfillChats(b.ID)
	} else {
		owned, cerr := a.ownChat(ctx, b.ID, chatID)
		if cerr != nil {
			return nil, cerr
		}
		ch = owned
	}
	run := db.Run{ID: ids.New(), BotID: b.ID, ChatID: ch.ID, Status: "running", CreatedAt: time.Now()}
	a.DB.Create(&run)
	b.LastTask = text
	b.Status = "working"
	a.DB.Save(b)
	if !a.Hub.Connected(b.ID) {
		cp := *b
		a.ensureRunningBg(&cp)
	}
	go a.runLoop(b.ID, ch.ID, run.ID, text)
	return connect.NewResponse(&v1.SendResponse{RunId: run.ID, ChatId: ch.ID}), nil
}

func (a *App) StopRun(ctx context.Context, req *connect.Request[v1.StopRunRequest]) (*connect.Response[v1.StopRunResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	chatID := req.Msg.GetChatId()
	if chatID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("chat_id required"))
	}
	if _, err := a.ownChat(ctx, b.ID, chatID); err != nil {
		return nil, err
	}
	a.stopChat(b.ID, chatID)
	return connect.NewResponse(&v1.StopRunResponse{}), nil
}

func (a *App) StreamRun(ctx context.Context, req *connect.Request[v1.StreamRunRequest], stream *connect.ServerStream[v1.RunEvent]) error {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return err
	}
	botID := req.Msg.GetBotId()
	chatID := req.Msg.GetChatId()
	after := req.Msg.GetAfterEventId()
	ch, unsub := a.Bus.Subscribe(botID)
	defer unsub()
	runQ := a.DB.Model(&db.Run{}).Select("id").Where("bot_id = ?", botID)
	if chatID != "" {
		runQ = runQ.Where("chat_id = ?", chatID)
	}
	var rows []db.RunEvent
	if err := a.DB.Where("run_id IN (?)", runQ).Order("created_at").Find(&rows).Error; err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, r := range eventsAfter(rows, after) {
		seen[r.ID] = struct{}{}
		if err := stream.Send(&v1.RunEvent{Id: r.ID, RunId: r.RunID, ChatId: chatID, Kind: r.Kind, Body: r.Body, Tool: r.Tool}); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return connect.NewError(connect.CodeAborted, errors.New("stream overflow"))
			}
			if chatID != "" && ev.GetChatId() != "" && ev.GetChatId() != chatID {
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

func (a *App) GetSettings(ctx context.Context, _ *connect.Request[v1.GetSettingsRequest]) (*connect.Response[v1.Settings], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	s := a.settings()
	return connect.NewResponse(s), nil
}

func (a *App) PutSettings(ctx context.Context, req *connect.Request[v1.PutSettingsRequest]) (*connect.Response[v1.Settings], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if m := req.Msg.GetModel(); m != "" {
		a.setSetting("model", m)
	}
	return connect.NewResponse(a.settings()), nil
}

func (a *App) settings() *v1.Settings {
	model := a.getSetting("model")
	if model == "" {
		model = config.DefaultModel
	}
	key := ""
	if a.Cfg != nil {
		key = a.Cfg.OpenRouter.APIKey
	}
	return &v1.Settings{Model: model, HasApiKey: key != "", ApiKey: ""}
}

func (a *App) getSetting(k string) string {
	var s db.Setting
	if err := a.DB.Where("key = ?", k).Limit(1).Find(&s).Error; err != nil {
		return ""
	}
	return s.Value
}

func (a *App) setSetting(k, v string) {
	s := db.Setting{Key: k, Value: v}
	a.DB.Save(&s)
}

func (a *App) ListAudit(ctx context.Context, _ *connect.Request[v1.ListAuditRequest]) (*connect.Response[v1.ListAuditResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	var rows []db.Audit
	a.DB.Order("created_at desc").Limit(100).Find(&rows)
	out := &v1.ListAuditResponse{}
	for _, r := range rows {
		out.Rows = append(out.Rows, &v1.AuditRow{
			Id: r.ID, At: r.CreatedAt.Format(time.RFC3339),
			BotId: r.BotID, BotName: r.BotName, Crest: int32(r.Crest),
			Actor: r.Actor, Action: r.Action, Decision: r.Decision,
		})
	}
	return connect.NewResponse(out), nil
}

const descMax = 400

func clipDesc(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > descMax {
		return s[:descMax]
	}
	return s
}

func eventsAfter(rows []db.RunEvent, after string) []db.RunEvent {
	skip := after != ""
	var out []db.RunEvent
	for _, r := range rows {
		if skip {
			if r.ID == after {
				skip = false
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
