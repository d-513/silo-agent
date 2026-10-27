package access

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	"silo.agent/internal/db"
)

// Roles on a Bot (bot_access.role). A Bot always has a manager.
const (
	RoleManage = "manage"
	RoleUse    = "use"
)

// Roles in a Workspace (space_members.role), weakest first.
const (
	RoleGuest  = "guest"
	RoleMember = "member"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

// Need is what the caller must be allowed to do with a Bot.
type Need int

const (
	// Use is anyone on the Bot: chat, the machine's panes, Dashboard, Feed.
	Use Need = iota + 1
	// Manage is the Bot's managers: whatever widens or changes what it may
	// do, and who may use it.
	Manage
	// Administer is a manager, or an admin of the Bot's Workspace who is not
	// on it: delete it, revoke a grant, remove a person. It never opens it.
	Administer
)

// SpaceNeed is the least Workspace role an action takes.
type SpaceNeed int

const (
	View  SpaceNeed = iota + 1 // any member, guests included
	Build                      // members and up: the hub, new Bots
	Admin                      // admins and the owner
	Own                        // the owner
)

// ChatNeed is what the caller must be allowed to do with a chat.
type ChatNeed int

const (
	// Read is the chat's author and the Bot's managers.
	Read ChatNeed = iota + 1
	// Write is the author alone: nobody writes as another person.
	Write
)

// The NotFound messages. A caller who may not see a thing gets the same
// answer as for a thing that does not exist, so ids cannot be probed.
const (
	NoBot   = "bot not found"
	NoChat  = "chat not found"
	NoSpace = "workspace not found"
)

func notFound(what string) error {
	return connect.NewError(connect.CodeNotFound, errors.New(what))
}

func denied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}

// BotRole is the user's role on the Bot: manage, use, or "" when not on it.
func BotRole(gdb *gorm.DB, botID, userID string) string {
	var a db.BotAccess
	if botID == "" || userID == "" {
		return ""
	}
	gdb.Where("bot_id = ? AND user_id = ?", botID, userID).Limit(1).Find(&a)
	return a.Role
}

// SpaceRole is the user's role in the Workspace, or "" when not in it.
func SpaceRole(gdb *gorm.DB, spaceID, userID string) string {
	var m db.SpaceMember
	if spaceID == "" || userID == "" {
		return ""
	}
	gdb.Where("space_id = ? AND user_id = ?", spaceID, userID).Limit(1).Find(&m)
	return m.Role
}

// Rank orders Workspace roles; an unknown role ranks below a guest.
func Rank(role string) SpaceNeed {
	switch role {
	case RoleGuest:
		return View
	case RoleMember:
		return Build
	case RoleAdmin:
		return Admin
	case RoleOwner:
		return Own
	}
	return 0
}

// Bot loads the Bot with this id if the signed-in user may do what need
// says. A Bot they are not on is NotFound (a Workspace admin's Administer
// aside); one they use but may not manage is PermissionDenied with a
// sentence the UI can show.
func Bot(ctx context.Context, gdb *gorm.DB, id string, need Need) (*db.Bot, error) {
	u := User(ctx)
	if u == nil || id == "" {
		return nil, notFound(NoBot)
	}
	var b db.Bot
	res := gdb.Where("id = ?", id).Limit(1).Find(&b)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, notFound(NoBot)
	}
	switch role := BotRole(gdb, b.ID, u.ID); {
	case role == RoleManage, role == RoleUse && need == Use:
		return &b, nil
	case role == RoleUse:
		return nil, denied(fmt.Sprintf("Only %s's managers can do this.", b.Name))
	case need == Administer && Rank(SpaceRole(gdb, b.SpaceID, u.ID)) >= Admin:
		return &b, nil
	}
	return nil, notFound(NoBot)
}

// Row is BotRow behind Bot's check: one row of a Bot the caller may act on.
func Row[T any](ctx context.Context, gdb *gorm.DB, botID, id, what string, need Need) (*T, error) {
	if _, err := Bot(ctx, gdb, botID, need); err != nil {
		return nil, err
	}
	return BotRow[T](gdb, botID, id, what)
}

// Chat loads a chat of the Bot with its Bot if the caller may do what need
// says with it.
func Chat(ctx context.Context, gdb *gorm.DB, botID, chatID string, need ChatNeed) (*db.Chat, *db.Bot, error) {
	b, err := Bot(ctx, gdb, botID, Use)
	if err != nil {
		return nil, nil, err
	}
	var c db.Chat
	res := gdb.Where("bot_id = ? AND id = ?", b.ID, chatID).Limit(1).Find(&c)
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil, notFound(NoChat)
	}
	u := User(ctx)
	manager := BotRole(gdb, b.ID, u.ID) == RoleManage
	if !CanRead(&c, u.ID, manager) {
		return nil, nil, notFound(NoChat)
	}
	if need == Write && !CanWrite(&c, u.ID, manager) {
		who := Email(gdb, c.UserID)
		if who == "" {
			who = "its author"
		}
		return nil, nil, denied(fmt.Sprintf("This is %s's chat: you can read it and stop it, and only they write in it.", who))
	}
	return &c, b, nil
}

// CanRead says whether a person reads a chat: its author, the Bot's
// managers, and anyone on the Bot for an automation's log.
func CanRead(c *db.Chat, userID string, manager bool) bool {
	return c.UserID == userID || manager || c.AutomationID != ""
}

// CanWrite says whether a person writes in a chat: its author, and the
// Bot's managers in one of the Bot's own (no author: a mail wake).
func CanWrite(c *db.Chat, userID string, manager bool) bool {
	return c.UserID == userID || (c.UserID == "" && manager)
}

// Space loads the Workspace if the signed-in user's role there is at least
// need. A Workspace they are not in is NotFound.
func Space(ctx context.Context, gdb *gorm.DB, id string, need SpaceNeed) (*db.Space, string, error) {
	u := User(ctx)
	if u == nil || id == "" {
		return nil, "", notFound(NoSpace)
	}
	role := SpaceRole(gdb, id, u.ID)
	if role == "" {
		return nil, "", notFound(NoSpace)
	}
	var s db.Space
	res := gdb.Where("id = ?", id).Limit(1).Find(&s)
	if res.Error != nil {
		return nil, "", res.Error
	}
	if res.RowsAffected == 0 {
		return nil, "", notFound(NoSpace)
	}
	if Rank(role) < need {
		return nil, "", denied(spaceDenied(need))
	}
	return &s, role, nil
}

func spaceDenied(need SpaceNeed) string {
	switch need {
	case Own:
		return "Only the workspace's owner can do this."
	case Admin:
		return "Only the workspace's admins can do this."
	}
	return "Guests can only use the Bots they are on."
}

// CanUse reports whether the user may use the Bot and the Bot is not
// suspended: the test of the plain HTTP routes (desktop and console sockets,
// private tunnels, app views), which re-check it on every request.
func CanUse(gdb *gorm.DB, botID, userID string) bool {
	return BotRole(gdb, botID, userID) != "" && !Suspended(gdb, botID)
}

// SuspendedBots selects the id of every Bot that has no enabled manager.
// Such a Bot does not run: no reply, automation, channel or tunnel.
func SuspendedBots(gdb *gorm.DB) *gorm.DB {
	return gdb.Model(&db.Bot{}).Select("bots.id").Where(`NOT EXISTS (
		SELECT 1 FROM bot_access a JOIN users u ON u.id = a.user_id
		WHERE a.bot_id = bots.id AND a.role = ? AND NOT u.disabled)`, RoleManage)
}

// Suspended reports whether the Bot has no enabled manager.
func Suspended(gdb *gorm.DB, botID string) bool {
	var n int64
	gdb.Table("(?) AS s", SuspendedBots(gdb)).Where("s.id = ?", botID).Count(&n)
	return n > 0
}
