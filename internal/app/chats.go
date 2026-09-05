package app

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

func protoChat(c *db.Chat) *v1.Chat {
	return &v1.Chat{
		Id: c.ID, BotId: c.BotID, Title: c.Title,
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

func (a *App) ownChat(ctx context.Context, botID, chatID string) (*db.Chat, error) {
	if _, err := a.ownBot(ctx, botID); err != nil {
		return nil, err
	}
	var c db.Chat
	if err := a.DB.First(&c, "id = ? AND bot_id = ?", chatID, botID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, err
	}
	return &c, nil
}

func (a *App) backfillChats(botID string) *db.Chat {
	var chats []db.Chat
	a.DB.Where("bot_id = ?", botID).Order("updated_at desc").Find(&chats)
	if len(chats) == 0 {
		c := db.Chat{ID: ids.New(), BotID: botID, Title: "New chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		a.DB.Create(&c)
		a.DB.Model(&db.Run{}).Where("bot_id = ? AND (chat_id = '' OR chat_id IS NULL)", botID).Update("chat_id", c.ID)
		return &c
	}
	a.DB.Model(&db.Run{}).Where("bot_id = ? AND (chat_id = '' OR chat_id IS NULL)", botID).Update("chat_id", chats[0].ID)
	return &chats[0]
}

func (a *App) ListChats(ctx context.Context, req *connect.Request[v1.ListChatsRequest]) (*connect.Response[v1.ListChatsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	a.backfillChats(req.Msg.GetBotId())
	var rows []db.Chat
	a.DB.Where("bot_id = ?", req.Msg.GetBotId()).Order("updated_at desc").Find(&rows)
	out := &v1.ListChatsResponse{}
	for i := range rows {
		out.Chats = append(out.Chats, protoChat(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateChat(ctx context.Context, req *connect.Request[v1.CreateChatRequest]) (*connect.Response[v1.Chat], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	c := db.Chat{ID: ids.New(), BotID: req.Msg.GetBotId(), Title: "New chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := a.DB.Create(&c).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(protoChat(&c)), nil
}

func (a *App) RenameChat(ctx context.Context, req *connect.Request[v1.RenameChatRequest]) (*connect.Response[v1.Chat], error) {
	c, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	title := req.Msg.GetTitle()
	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title required"))
	}
	c.Title = title
	c.UpdatedAt = time.Now()
	a.DB.Save(c)
	return connect.NewResponse(protoChat(c)), nil
}

func (a *App) DeleteChat(ctx context.Context, req *connect.Request[v1.DeleteChatRequest]) (*connect.Response[v1.DeleteChatResponse], error) {
	c, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	var runs []db.Run
	a.DB.Where("chat_id = ?", c.ID).Find(&runs)
	for _, r := range runs {
		a.DB.Where("run_id = ?", r.ID).Delete(&db.RunEvent{})
	}
	a.DB.Where("chat_id = ?", c.ID).Delete(&db.Run{})
	a.DB.Delete(c)
	return connect.NewResponse(&v1.DeleteChatResponse{}), nil
}
