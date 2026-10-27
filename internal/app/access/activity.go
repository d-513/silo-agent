package access

import (
	"time"

	"gorm.io/gorm"

	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// Activity kinds: what a Workspace's admins read on its Activity page.
const (
	Joined      = "joined"
	Left        = "left"
	Removed     = "removed"
	RoleChanged = "role_changed"
	BotCreated  = "bot_created"
	BotDeleted  = "bot_deleted"
	Granted     = "granted"
	Revoked     = "revoked"
	Lent        = "lent"
	SignedInTo  = "signed_in"
)

// Activity records a Workspace event: who (actorUserID, "" for Silo
// itself) did what (kind) about what (subject), on which Bot if one. It is
// what happened in the Workspace, never what a Bot did with a grant.
func Activity(gdb *gorm.DB, spaceID, actorUserID, kind, subject, botID, detail string) {
	if spaceID == "" {
		return
	}
	ev := db.AuditEvent{
		ID: ids.New(), SpaceID: spaceID, ActorUser: actorUserID, Actor: Email(gdb, actorUserID),
		Decision: kind, Subject: subject, Detail: detail, BotID: botID, CreatedAt: time.Now(),
	}
	if botID != "" {
		var b db.Bot
		gdb.Select("name", "crest").Where("id = ?", botID).Limit(1).Find(&b)
		ev.BotName, ev.Crest = b.Name, b.Crest
	}
	gdb.Create(&ev)
}
