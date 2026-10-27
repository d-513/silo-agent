package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/models"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// chat loads a chat of the Bot the caller may do what need says with
// (access.Chat).
func (a *App) chat(ctx context.Context, botID, chatID string, need access.ChatNeed) (*db.Chat, error) {
	c, _, err := access.Chat(ctx, a.DB, botID, chatID, need)
	return c, err
}

// writableChat refuses conversation edits on an automation's log: its runs are
// driven by the automation's prompt, and deleting the automation drops the log.
func writableChat(c *db.Chat) error {
	if c.AutomationID != "" {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("an automation log is read-only; edit the automation instead"))
	}
	if c.SubagentID != "" {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("a subagent log is read-only; only its lead talks to it"))
	}
	return nil
}

// mainChat is the Bot's main chat (made on first use).
func (a *App) mainChat(botID string) (*db.Chat, error) {
	c, err := chats.Main(a.DB, botID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return c, nil
}

// errMainChat refuses what the main chat does not do: it is the Bot's one
// standing conversation.
var errMainChat = connect.NewError(connect.CodeFailedPrecondition, errors.New("the main chat cannot be deleted or renamed; /clear or /wipe it instead"))

func (a *App) ListChats(ctx context.Context, req *connect.Request[v1.ListChatsRequest]) (*connect.Response[v1.ListChatsResponse], error) {
	if _, err := a.bot(ctx, req.Msg.GetBotId(), access.Use); err != nil {
		return nil, err
	}
	q := a.DB.Where("bot_id = ?", req.Msg.GetBotId())
	if req.Msg.GetChannelId() == "" {
		if _, err := a.mainChat(req.Msg.GetBotId()); err != nil {
			return nil, err
		}
		q = q.Scopes(chats.WebScope)
	} else {
		q = q.Where("channel_id = ?", req.Msg.GetChannelId())
	}
	var rows []db.Chat
	q.Order("updated_at desc").Find(&rows)
	out := &v1.ListChatsResponse{}
	for i := range rows {
		out.Chats = append(out.Chats, chats.Proto(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateChat(ctx context.Context, req *connect.Request[v1.CreateChatRequest]) (*connect.Response[v1.Chat], error) {
	if _, err := a.bot(ctx, req.Msg.GetBotId(), access.Use); err != nil {
		return nil, err
	}
	c, err := chats.New(a.DB, req.Msg.GetBotId(), "")
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(chats.Proto(c)), nil
}

func (a *App) RenameChat(ctx context.Context, req *connect.Request[v1.RenameChatRequest]) (*connect.Response[v1.Chat], error) {
	c, err := a.chat(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Write)
	if err != nil {
		return nil, err
	}
	if c.Main {
		return nil, errMainChat
	}
	title := strings.TrimSpace(req.Msg.GetTitle())
	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title required"))
	}
	if n := utf8.RuneCountInString(title); n > 80 {
		title = string([]rune(title)[:80])
	}
	c.Title, c.UpdatedAt = title, time.Now()
	a.DB.Model(&db.Chat{}).Where("id = ?", c.ID).Updates(map[string]any{"title": c.Title, "updated_at": c.UpdatedAt})
	return connect.NewResponse(chats.Proto(c)), nil
}

func (a *App) DeleteChat(ctx context.Context, req *connect.Request[v1.DeleteChatRequest]) (*connect.Response[v1.DeleteChatResponse], error) {
	c, err := a.chat(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Read)
	if err != nil {
		return nil, err
	}
	if err := writableChat(c); err != nil {
		return nil, err
	}
	if c.Main {
		return nil, errMainChat
	}
	a.StopChatLive(c.BotID, c.ID)
	a.dropSubagents(c.BotID, c.ID)
	var runs []db.Run
	a.DB.Where("chat_id = ?", c.ID).Find(&runs)
	for _, r := range runs {
		a.DB.Where("run_id = ?", r.ID).Delete(&db.RunEvent{})
	}
	a.DB.Where("chat_id = ?", c.ID).Delete(&db.Run{})
	a.DB.Delete(c)
	a.Setup.DropChat(c.ID)
	// Its artifacts' pinned bytes go now rather than on the next sweep.
	go a.Artifacts.SweepBot(context.Background(), c.BotID)
	return connect.NewResponse(&v1.DeleteChatResponse{}), nil
}

// ListModels returns the operator's model allowlist for the chat model picker.
// It is available to any owner of the Bot, not just admins.
func (a *App) ListModels(ctx context.Context, req *connect.Request[v1.ListModelsRequest]) (*connect.Response[v1.ListModelsResponse], error) {
	b, err := a.bot(ctx, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	cfg := a.cfg()
	return connect.NewResponse(&v1.ListModelsResponse{
		Models:       models.Protos(a.Models.WithThinking(ctx, a.Models.Allowed())),
		DefaultModel: a.Models.BotDefault(b.ID),
		TitleModel:   cfg.ModelTitle,
		VoiceEnabled: a.Voice.Enabled(),
	}), nil
}

// SetChatModel persists a per-chat model override. It must be in the allowlist.
func (a *App) SetChatModel(ctx context.Context, req *connect.Request[v1.SetChatModelRequest]) (*connect.Response[v1.Chat], error) {
	c, err := a.chat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId(), access.Write)
	if err != nil {
		return nil, err
	}
	model, err := a.chatModel(req.Msg.GetModel())
	if err != nil {
		return nil, err
	}
	c.Model = model
	a.DB.Save(c)
	return connect.NewResponse(chats.Proto(c)), nil
}

// chatModel is a chat's model override as a client names it: "" (the Bot's
// default) or a model on the operator's list.
func (a *App) chatModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if model == "" {
		return "", nil
	}
	if _, _, err := llm.Parse(model); err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	if !llm.Allowed(model, a.cfg().Models) {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("model is not in the allowed list"))
	}
	return model, nil
}

func cleanTitle(raw string) string {
	s := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	s = strings.Trim(s, `"'“”‘’`)
	s = strings.TrimRight(s, " .")
	s = strings.TrimSpace(s)
	if chats.Untitled(s) {
		return ""
	}
	if utf8.RuneCountInString(s) > 60 {
		s = strings.TrimSpace(string([]rune(s)[:60]))
	}
	return s
}

func (a *App) applyGeneratedTitle(chatID, title string) bool {
	title = strings.TrimSpace(title)
	if chats.Untitled(title) {
		return false
	}
	res := a.DB.Model(&db.Chat{}).Where("id = ? AND title IN ?", chatID, chats.Placeholders).
		Updates(map[string]any{"title": title, "updated_at": time.Now()})
	return res.RowsAffected > 0
}
