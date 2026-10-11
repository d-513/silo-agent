package zone

import (
	"time"

	"gorm.io/gorm"

	"silo.agent/internal/db"
)

// OfBot is the zone a Bot's schedules and the times it reads are in right now:
// its own, or the server's while it has none. It asks the database each time,
// because the zone follows the owner and can move during a run.
func OfBot(gdb *gorm.DB, botID string) *time.Location {
	var b db.Bot
	gdb.Select("time_zone").Limit(1).Find(&b, "id = ?", botID)
	return Resolve(b.TimeZone)
}
