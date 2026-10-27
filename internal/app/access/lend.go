package access

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	"silo.agent/internal/db"
)

// An identity is lent, never taken. A connection or drive someone signed in
// to (or typed a credential into) carries their identity: lent_by. Personal
// (not shared, the default) it may only be in Bots whose people are exactly
// its lender; shared (the lender's say-so) it may be in any Bot of its
// Workspace, which then acts as them.

// Lend is a resource as the lending checks see it.
type Lend struct {
	// connector or drive.
	Kind   string
	ID     string
	Name   string
	LentBy string
	Shared bool
}

// Personal reports whether the resource carries someone's identity that
// they have not shared.
func (l Lend) Personal() bool { return l.LentBy != "" && !l.Shared }

func failed(msg string) error {
	return connect.NewError(connect.CodeFailedPrecondition, errors.New(msg))
}

// Email is a user's email, or "" for nobody.
func Email(gdb *gorm.DB, userID string) string {
	if userID == "" {
		return ""
	}
	var u db.User
	gdb.Select("email").Where("id = ?", userID).Limit(1).Find(&u)
	return u.Email
}

// People are the ids of everyone on the Bot.
func People(gdb *gorm.DB, botID string) []string {
	var out []string
	gdb.Model(&db.BotAccess{}).Where("bot_id = ?", botID).Order("created_at").Pluck("user_id", &out)
	return out
}

func botName(gdb *gorm.DB, botID string) string {
	var b db.Bot
	gdb.Select("name").Where("id = ?", botID).Limit(1).Find(&b)
	return b.Name
}

// others are the people on the Bot, and those about to be, who are not who.
func others(gdb *gorm.DB, botID, who string, adding ...string) []string {
	var out []string
	seen := map[string]bool{who: true}
	for _, id := range append(People(gdb, botID), adding...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, Email(gdb, id))
		}
	}
	sort.Strings(out)
	return out
}

// CheckLend refuses to put a personal resource in a Bot that someone other
// than its lender uses (or is about to: adding).
func CheckLend(gdb *gorm.DB, l Lend, botID string, adding ...string) error {
	if !l.Personal() {
		return nil
	}
	if o := others(gdb, botID, l.LentBy, adding...); len(o) > 0 {
		name := botName(gdb, botID)
		if name == "" {
			name = "the new Bot"
		}
		return failed(fmt.Sprintf("%s's %s is personal: %s is also used by %s. %s can share it with the workspace.",
			Email(gdb, l.LentBy), l.Name, name, strings.Join(o, ", "), Email(gdb, l.LentBy)))
	}
	return nil
}

// CheckSignIn refuses a sign-in that would make userID the lender of a
// resource in Bots other people use, unless they share it.
func CheckSignIn(gdb *gorm.DB, name, userID string, shared bool, botIDs []string) error {
	if shared {
		return nil
	}
	var used []string
	for _, b := range botIDs {
		if len(others(gdb, b, userID)) > 0 {
			used = append(used, botName(gdb, b))
		}
	}
	if len(used) > 0 {
		sort.Strings(used)
		return failed(fmt.Sprintf("%s is in Bots other people use: %s. Sign in for anyone in the workspace, or remove it from them first.", name, strings.Join(used, ", ")))
	}
	return nil
}

// CheckUnshare refuses to make a resource personal again while it is in
// Bots someone other than its lender uses, and names them.
func CheckUnshare(gdb *gorm.DB, l Lend, botIDs []string) error {
	var used []string
	for _, b := range botIDs {
		if len(others(gdb, b, l.LentBy)) > 0 {
			used = append(used, botName(gdb, b))
		}
	}
	if len(used) > 0 {
		sort.Strings(used)
		return failed(fmt.Sprintf("%s is in Bots other people use: %s. Remove it from them first.", l.Name, strings.Join(used, ", ")))
	}
	return nil
}

// PersonalOn is every personal resource granted to the Bot.
func PersonalOn(gdb *gorm.DB, botID string) []Lend {
	var out []Lend
	var conns []db.Connector
	gdb.Joins("JOIN bot_connectors g ON g.connector_id = connectors.id").
		Where("g.bot_id = ? AND connectors.lent_by <> '' AND NOT connectors.shared", botID).Find(&conns)
	for _, c := range conns {
		out = append(out, Lend{Kind: "connector", ID: c.ID, Name: c.Name, LentBy: c.LentBy})
	}
	var drives []db.Drive
	gdb.Joins("JOIN bot_drives g ON g.drive_id = drives.id").
		Where("g.bot_id = ? AND drives.lent_by <> '' AND NOT drives.shared", botID).Find(&drives)
	for _, d := range drives {
		out = append(out, Lend{Kind: "drive", ID: d.ID, Name: d.Name, LentBy: d.LentBy})
	}
	return out
}

// CheckAdd refuses to put a person on a Bot that holds someone else's
// personal resource, and says whose and the way out.
func CheckAdd(gdb *gorm.DB, botID, userID string) error {
	for _, l := range PersonalOn(gdb, botID) {
		if l.LentBy == userID {
			continue
		}
		who := Email(gdb, l.LentBy)
		return failed(fmt.Sprintf("%s's %s is personal. %s can share it, or you can remove it from %s.", who, l.Name, who, botName(gdb, botID)))
	}
	return nil
}

// LenderOK refuses a resource whose lender's account is disabled: their
// identity is not lent while they cannot sign in. Nothing is deleted, so
// enabling them brings it back.
func LenderOK(gdb *gorm.DB, lentBy string) error {
	if lentBy == "" {
		return nil
	}
	var u db.User
	gdb.Select("email", "disabled").Where("id = ?", lentBy).Limit(1).Find(&u)
	if u.Disabled {
		return failed(fmt.Sprintf("%s's account is disabled.", u.Email))
	}
	return nil
}
