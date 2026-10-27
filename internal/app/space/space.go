// Package space is Workspaces: containers of Bots and the people who use them.
// The code says space because "workspace" is the Bot's /workspace; the UI
// says Workspace. Who may do what in one is decided by package access; this
// package keeps the rows (spaces, space_members, bot_access) and serves the
// Workspace RPCs.
package space

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/lang"
	"silo.agent/internal/textx"
)

// nameMax caps a Workspace's name, in runes.
const nameMax = 60

// homeNames name the Workspace every user starts with, in their display
// language. It is theirs to rename.
var homeNames = map[string]string{"en": "Personal", "pl": "Osobisty", "de": "Persönlich"}

// Service is the Workspaces domain.
type Service struct {
	db *gorm.DB
	// dropped are told a Workspace's id once its rows are gone, so the
	// domains that keep things per Workspace (skills on disk) drop theirs.
	dropped []func(spaceID string)
}

// OnDelete registers what else goes when a Workspace is deleted.
func (s *Service) OnDelete(fn func(spaceID string)) { s.dropped = append(s.dropped, fn) }

// New makes the Workspaces domain.
func New(gdb *gorm.DB) *Service { return &Service{db: gdb} }

// Mark is the letters on a Workspace's tile: the given ones (letters and
// digits, at most two, upper case), else the initials of the name.
func Mark(name, given string) string {
	if m := firstRunes(given, 2); m != "" {
		return m
	}
	words := strings.Fields(name)
	if len(words) >= 2 {
		if m := firstRunes(words[0], 1) + firstRunes(words[1], 1); m != "" {
			return m
		}
	}
	if m := firstRunes(name, 2); m != "" {
		return m
	}
	return "W"
}

func firstRunes(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		if n == 0 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
			n--
		}
	}
	return b.String()
}

func cleanName(name string) (string, error) {
	name = textx.CapRunes(strings.TrimSpace(name), nameMax)
	if name == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("a workspace needs a name"))
	}
	return name, nil
}

// Create makes a Workspace owned by the user.
func (s *Service) Create(userID, name, mark string) (*db.Space, error) {
	name, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	sp := db.Space{ID: ids.New(), Name: name, Mark: Mark(name, mark), CreatedAt: time.Now()}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&sp).Error; err != nil {
			return err
		}
		return tx.Create(&db.SpaceMember{SpaceID: sp.ID, UserID: userID, Role: access.RoleOwner, CreatedAt: sp.CreatedAt}).Error
	})
	if err != nil {
		return nil, err
	}
	return &sp, nil
}

// HomeID is the id of the user's first own Workspace, or "" if they own none.
// It is what a client from before Workspaces means by "mine" (the iOS app's
// personal skills).
func HomeID(gdb *gorm.DB, userID string) string {
	var id string
	gdb.Model(&db.SpaceMember{}).Joins("JOIN spaces ON spaces.id = space_members.space_id").
		Where("space_members.user_id = ? AND space_members.role = ?", userID, access.RoleOwner).
		Order("spaces.created_at, spaces.id").Limit(1).Pluck("spaces.id", &id)
	return id
}

// Home is the user's first own Workspace, made if they own none: every user
// owns at least one.
func (s *Service) Home(u *db.User) (*db.Space, error) {
	var sp db.Space
	res := s.db.Joins("JOIN space_members m ON m.space_id = spaces.id").
		Where("m.user_id = ? AND m.role = ?", u.ID, access.RoleOwner).
		Order("spaces.created_at, spaces.id").Limit(1).Find(&sp)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 1 {
		return &sp, nil
	}
	return s.Create(u.ID, homeNames[lang.UI(u.Language)], "")
}

// ForNewBot is where the caller may make a Bot: the Workspace named, if they
// may build there, else (none named) their own first one.
func (s *Service) ForNewBot(ctx context.Context, spaceID string) (*db.Space, error) {
	if spaceID == "" {
		u := access.User(ctx)
		if u == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}
		return s.Home(u)
	}
	sp, _, err := access.Space(ctx, s.db, spaceID, access.Build)
	return sp, err
}

// Admit puts a person on a Bot with a role (manage or use).
func (s *Service) Admit(botID, userID, role string) error {
	return s.db.Create(&db.BotAccess{BotID: botID, UserID: userID, Role: role, CreatedAt: time.Now()}).Error
}

// DropBot forgets who was on a Bot that is being deleted.
func (s *Service) DropBot(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.BotAccess{})
}

// Bots is every Bot in the Workspace.
func (s *Service) Bots(spaceID string) []db.Bot {
	var bots []db.Bot
	s.db.Where("space_id = ?", spaceID).Order("created_at").Find(&bots)
	return bots
}

// Managed is every Bot the user manages, in any Workspace.
func (s *Service) Managed(userID string) []db.Bot {
	var bots []db.Bot
	s.db.Joins("JOIN bot_access a ON a.bot_id = bots.id").
		Where("a.user_id = ? AND a.role = ?", userID, access.RoleManage).
		Order("bots.created_at").Find(&bots)
	return bots
}

// Alone is every Workspace the user is the only member of.
func (s *Service) Alone(userID string) []db.Space {
	var out []db.Space
	s.db.Joins("JOIN space_members m ON m.space_id = spaces.id AND m.user_id = ?", userID).
		Where("NOT EXISTS (SELECT 1 FROM space_members o WHERE o.space_id = spaces.id AND o.user_id <> ?)", userID).
		Order("spaces.created_at").Find(&out)
	return out
}

// Delete removes an empty Workspace and its members. The caller has moved
// or dropped its Bots.
func (s *Service) Delete(spaceID string) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ?", spaceID).Delete(&db.SpaceMember{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", spaceID).Delete(&db.Space{}).Error
	})
	if err != nil {
		return err
	}
	for _, fn := range s.dropped {
		fn(spaceID)
	}
	return nil
}

// Forget removes the user from every Workspace and Bot they are still in.
func (s *Service) Forget(userID string) {
	s.db.Where("user_id = ?", userID).Delete(&db.BotAccess{})
	s.db.Where("user_id = ?", userID).Delete(&db.SpaceMember{})
}

// view is a Workspace as the caller sees it.
func (s *Service) view(sp *db.Space, role, userID string) *v1.Space {
	var bots, people int64
	s.db.Model(&db.Bot{}).Joins("JOIN bot_access a ON a.bot_id = bots.id").
		Where("bots.space_id = ? AND a.user_id = ?", sp.ID, userID).Count(&bots)
	s.db.Model(&db.SpaceMember{}).Where("space_id = ?", sp.ID).Count(&people)
	return &v1.Space{
		Id: sp.ID, Name: sp.Name, Mark: sp.Mark, Role: role,
		Bots: int32(bots), People: int32(people),
		CreatedAt: sp.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Service) ListSpaces(ctx context.Context, _ *connect.Request[v1.ListSpacesRequest]) (*connect.Response[v1.ListSpacesResponse], error) {
	u := access.User(ctx)
	if _, err := s.Home(u); err != nil {
		return nil, err
	}
	var rows []struct {
		db.Space
		Role string
	}
	if err := s.db.Model(&db.Space{}).Select("spaces.*, m.role").
		Joins("JOIN space_members m ON m.space_id = spaces.id").
		Where("m.user_id = ?", u.ID).Order("spaces.created_at, spaces.id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := &v1.ListSpacesResponse{}
	for i := range rows {
		out.Spaces = append(out.Spaces, s.view(&rows[i].Space, rows[i].Role, u.ID))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) GetSpace(ctx context.Context, req *connect.Request[v1.GetSpaceRequest]) (*connect.Response[v1.Space], error) {
	sp, role, err := access.Space(ctx, s.db, req.Msg.GetId(), access.View)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.view(sp, role, access.User(ctx).ID)), nil
}

func (s *Service) CreateSpace(ctx context.Context, req *connect.Request[v1.CreateSpaceRequest]) (*connect.Response[v1.Space], error) {
	u := access.User(ctx)
	sp, err := s.Create(u.ID, req.Msg.GetName(), req.Msg.GetMark())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.view(sp, access.RoleOwner, u.ID)), nil
}

func (s *Service) UpdateSpace(ctx context.Context, req *connect.Request[v1.UpdateSpaceRequest]) (*connect.Response[v1.Space], error) {
	sp, role, err := access.Space(ctx, s.db, req.Msg.GetId(), access.Admin)
	if err != nil {
		return nil, err
	}
	name, err := cleanName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	sp.Name, sp.Mark = name, Mark(name, req.Msg.GetMark())
	if err := s.db.Model(&db.Space{}).Where("id = ?", sp.ID).
		Updates(map[string]any{"name": sp.Name, "mark": sp.Mark}).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(s.view(sp, role, access.User(ctx).ID)), nil
}

func (s *Service) DeleteSpace(ctx context.Context, req *connect.Request[v1.DeleteSpaceRequest]) (*connect.Response[v1.DeleteSpaceResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetId(), access.Own)
	if err != nil {
		return nil, err
	}
	var bots, owned int64
	s.db.Model(&db.Bot{}).Where("space_id = ?", sp.ID).Count(&bots)
	if bots > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("move or delete its Bots first"))
	}
	s.db.Model(&db.SpaceMember{}).Where("user_id = ? AND role = ?", access.User(ctx).ID, access.RoleOwner).Count(&owned)
	if owned <= 1 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("this is the only workspace you own"))
	}
	if err := s.Delete(sp.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.DeleteSpaceResponse{}), nil
}
