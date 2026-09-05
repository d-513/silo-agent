package app

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

func (a *App) Commands(ctx context.Context, stream *connect.BidiStream[v1.CmdEvent, v1.Cmd]) error {
	bot := currentBot(ctx)
	sess := a.Hub.Attach(bot.ID)
	defer a.Hub.Detach(bot.ID, sess)
	a.recomputeStatus(bot.ID)
	log.Printf("worker connected bot=%s", bot.ID)
	go a.pushTools(bot.ID)

	errc := make(chan error, 1)
	go func() {
		for {
			ev, err := stream.Receive()
			if err != nil {
				errc <- err
				return
			}
			switch b := ev.GetBody().(type) {
			case *v1.CmdEvent_Heartbeat:
			case *v1.CmdEvent_Chunk:
				runID := a.runOfCmd(ev.GetId())
				a.emit(bot.ID, a.chatOfRun(runID), runID, "tool_chunk", b.Chunk.GetText(), "")
			case *v1.CmdEvent_Done:
				sess.Resolve(ev.GetId(), a.Mask(bot.ID).Apply(b.Done.GetResult()), nil)
			case *v1.CmdEvent_Error:
				sess.Resolve(ev.GetId(), "", errors.New(b.Error.GetMessage()))
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errc:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case cmd := <-sess.Send:
			if err := stream.Send(cmd); err != nil {
				return err
			}
		}
	}
}

func (a *App) runOfCmd(cmdID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cmdRun[cmdID]
}

func (a *App) chatOfRun(runID string) string {
	if runID == "" {
		return ""
	}
	var r db.Run
	if a.DB.First(&r, "id = ?", runID).Error != nil {
		return ""
	}
	return r.ChatID
}

func (a *App) GetSecret(ctx context.Context, req *connect.Request[v1.SecretReq]) (*connect.Response[v1.SecretRes], error) {
	bot := currentBot(ctx)
	name := req.Msg.GetName()
	var sec db.Secret
	if err := a.DB.First(&sec, "bot_id = ? AND name = ?", bot.ID, name).Error; err != nil {
		return connect.NewResponse(&v1.SecretRes{Error: "unknown secret"}), nil
	}
	runID := req.Msg.GetRunId()
	title := security.Describe("secrets", "get", argsJSON(name)).Title
	a.emit(bot.ID, a.chatOfRun(runID), runID, "call", title, "secrets.get")
	decision := security.Rule(a.ruleDecision(bot.ID, "secrets", "get", ""))
	switch decision {
	case "deny":
		a.audit(bot, "worker", "secrets.get", "deny")
		a.emitCallDone(bot.ID, runID, "secrets.get", "denied")
		return connect.NewResponse(&v1.SecretRes{Error: "denied"}), nil
	case "allow":
		now := time.Now()
		sec.LastUsedAt = &now
		a.DB.Save(&sec)
		a.Mask(bot.ID).Add(sec.Value)
		a.audit(bot, "worker", "secrets.get", "allow")
		a.emitCallDone(bot.ID, runID, "secrets.get", "ok")
		return connect.NewResponse(&v1.SecretRes{Value: sec.Value}), nil
	default:
		ap := db.Approval{
			ID: ids.New(), BotID: bot.ID, RunID: runID,
			Connector: "secrets", Action: "get", ArgsJSON: argsJSON(name), Status: "pending", CreatedAt: time.Now(),
		}
		a.DB.Create(&ap)
		ch := make(chan string, 1)
		a.mu.Lock()
		a.approvals[ap.ID] = &waiter{ch: ch, botID: bot.ID}
		a.mu.Unlock()
		a.setBotStatus(bot.ID, "needs_you")
		a.emit(bot.ID, a.chatOfRun(runID), runID, "approval", ap.ID, "secrets.get")
		var dec string
		select {
		case <-ctx.Done():
			a.dropWaiter(ap.ID)
			a.DB.Model(&db.Approval{}).Where("id = ? AND status = ?", ap.ID, "pending").Update("status", "canceled")
			a.recomputeStatus(bot.ID)
			a.emitCallDone(bot.ID, runID, "secrets.get", "canceled")
			return connect.NewResponse(&v1.SecretRes{Error: "canceled"}), nil
		case dec = <-ch:
		}
		if !security.Granted(dec) {
			a.emitCallDone(bot.ID, runID, "secrets.get", "denied")
			return connect.NewResponse(&v1.SecretRes{Error: "denied"}), nil
		}
		now := time.Now()
		sec.LastUsedAt = &now
		a.DB.Save(&sec)
		a.Mask(bot.ID).Add(sec.Value)
		a.emitCallDone(bot.ID, runID, "secrets.get", "ok")
		return connect.NewResponse(&v1.SecretRes{Value: sec.Value}), nil
	}
}

func (a *App) dropWaiter(id string) {
	a.mu.Lock()
	delete(a.approvals, id)
	a.mu.Unlock()
}

func (a *App) VNC(ctx context.Context, stream *connect.BidiStream[v1.Frame, v1.Frame]) error {
	bot := currentBot(ctx)
	var sess *hub.Session
	for i := 0; i < 80; i++ {
		sess = a.Hub.Get(bot.ID)
		if sess != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if sess == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("no command session"))
	}
	log.Printf("vnc worker waiting bot=%s", bot.ID)
	errc := make(chan error, 1)
	go func() {
		for {
			fr, err := stream.Receive()
			if err != nil {
				errc <- err
				return
			}
			if len(fr.GetData()) == 0 {
				continue
			}
			if err := sess.PushBrowser(ctx, fr.GetData()); err != nil {
				errc <- err
				return
			}
		}
	}()
	for {
		if err := sess.WaitViewer(ctx); err != nil {
			return err
		}
		gone := sess.ViewerGone()
		select {
		case <-gone:
			continue
		default:
		}
		_, toWorker := sess.Pipes()
		if !sess.HasViewer() {
			continue
		}
		if err := stream.Send(&v1.Frame{}); err != nil {
			return err
		}
		log.Printf("vnc worker stream bot=%s viewer", bot.ID)
		gone = sess.ViewerGone()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-sess.Done():
				return hub.ErrClosed
			case <-gone:
				return nil
			case err := <-errc:
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			case b := <-toWorker:
				if len(b) == 0 {
					continue
				}
				if err := stream.Send(&v1.Frame{Data: b}); err != nil {
					return err
				}
			}
		}
	}
}

func (a *App) ruleDecision(botID, conn, action, fallback string) string {
	var r db.Rule
	if err := a.DB.First(&r, "bot_id = ? AND connector = ? AND action = ?", botID, conn, action).Error; err == nil && r.Decision != "" {
		return r.Decision
	}
	if fallback != "" {
		return fallback
	}
	return "ask"
}

func (a *App) emitCallDone(botID, runID, tool, body string) {
	a.emit(botID, a.chatOfRun(runID), runID, "call_result", body, tool)
}

func (a *App) audit(bot *db.Bot, actor, action, decision string) {
	a.DB.Create(&db.Audit{
		ID: ids.New(), BotID: bot.ID, BotName: bot.Name, Crest: bot.Crest,
		Actor: actor, Action: action, Decision: decision, CreatedAt: time.Now(),
	})
}
