package app

import (
	"fmt"
	"strings"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/zone"
)

// botZone is the name of the zone a Bot's schedules and the times it reads are
// in right now: its own, or the server's while it has none.
func (a *App) botZone(botID string) string {
	return zone.OfBot(a.DB, botID).String()
}

// setBotZone moves a Bot to an IANA zone and re-times its automations, whose
// cron is read in it. It writes the one column: the bots row is saved whole
// elsewhere, and this runs on the way in of every message. It reports whether
// the zone changed.
func (a *App) setBotZone(botID, name string) (bool, error) {
	if err := zone.Check(name); err != nil {
		return false, err
	}
	res := a.DB.Model(&db.Bot{}).Where("id = ? AND time_zone <> ?", botID, name).Update("time_zone", name)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	a.Automations.Rezone(botID)
	return true, nil
}

// followZone takes the zone of the device a message was sent from, unless the
// owner pinned the Bot's. A device that names no zone, or one this server does
// not know, changes nothing: a message is never refused over its zone.
func (a *App) followZone(b *db.Bot, device string) {
	if b.TimeZonePinned || device == "" || device == b.TimeZone {
		return
	}
	if changed, err := a.setBotZone(b.ID, device); err == nil && changed {
		b.TimeZone = device
	}
}

// zoneTool is the Bot's set_timezone: for a human who says where they are in a
// conversation that carries no device zone (a channel). It moves a pinned Bot
// too, since it was asked for, and leaves the pin as it is.
func (a *App) zoneTool(botID, name string) (string, error) {
	name = strings.TrimSpace(name)
	changed, err := a.setBotZone(botID, name)
	if err != nil {
		return "", err
	}
	loc := zone.Resolve(name)
	var b strings.Builder
	if changed {
		fmt.Fprintf(&b, "Time zone set to %s.", name)
	} else {
		fmt.Fprintf(&b, "The time zone already was %s.", name)
	}
	fmt.Fprintf(&b, " It is now %s there. Automation schedules are read in it", zone.Stamp(time.Now(), loc))
	var rows []db.Automation
	a.DB.Where("bot_id = ? AND next_run_at IS NOT NULL", botID).Order("next_run_at").Find(&rows)
	if len(rows) == 0 {
		b.WriteString(".")
		return b.String(), nil
	}
	b.WriteString("; next firings:")
	for i := range rows {
		fmt.Fprintf(&b, "\n- %s (`%s`): %s", rows[i].Name, rows[i].Schedule, zone.Stamp(*rows[i].NextRunAt, loc))
	}
	return b.String(), nil
}
