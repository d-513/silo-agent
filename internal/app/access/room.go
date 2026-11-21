package access

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"silo.agent/internal/db"
)

// NoRoom is the room's NotFound: the same for a room that does not exist and
// one the caller is not in.
const NoRoom = "room not found"

// Room is a room the caller is in. Everyone in a room is on every Bot in it
// (the Bot is the unit of trust, and a room shares what each Bot says with
// everyone there), so a person taken off one of its Bots is refused until
// someone takes that Bot out or them.
func Room(ctx context.Context, gdb *gorm.DB, id string) (*db.Room, error) {
	u := User(ctx)
	if u == nil || id == "" {
		return nil, notFound(NoRoom)
	}
	var r db.Room
	if gdb.Where("id = ?", id).Limit(1).Find(&r); r.ID == "" || !InRoom(gdb, r.ID, u.ID) {
		return nil, notFound(NoRoom)
	}
	if SpaceRole(gdb, r.SpaceID, u.ID) == "" {
		return nil, notFound(NoRoom)
	}
	if name := RoomBotOff(gdb, r.ID, u.ID); name != "" {
		return nil, denied(fmt.Sprintf("You are no longer on %s, which is in this room: ask its managers to add you back, or someone in the room to take it out.", name))
	}
	return &r, nil
}

// InRoom reports whether the person is in the room.
func InRoom(gdb *gorm.DB, roomID, userID string) bool {
	var n int64
	gdb.Model(&db.RoomPerson{}).Where("room_id = ? AND user_id = ?", roomID, userID).Count(&n)
	return n > 0
}

// RoomBotOff is the name of a Bot in the room the person is not on, "" when
// they are on every one.
func RoomBotOff(gdb *gorm.DB, roomID, userID string) string {
	var bots []db.Bot
	gdb.Where("id IN (?) AND id NOT IN (?)",
		gdb.Model(&db.RoomBot{}).Select("bot_id").Where("room_id = ?", roomID),
		gdb.Model(&db.BotAccess{}).Select("bot_id").Where("user_id = ?", userID),
	).Order("name").Limit(1).Find(&bots)
	if len(bots) == 0 {
		return ""
	}
	return bots[0].Name
}
