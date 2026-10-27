package space

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	"log"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"slices"
)

// Host is what the Workspaces domain needs from the App.
type Host interface {
	// DropBot deletes a Bot: the body of DeleteBot.
	DropBot(ctx context.Context, botID string)
	// AccessEnded ends what a person holds open on a Bot (streams, sockets)
	// once they are off it.
	AccessEnded(botID, userID string)
	// SignUp makes an account with a password and signs it in on this
	// request.
	SignUp(ctx context.Context, email, password string) (*db.User, error)
}

// Lending is a domain whose resources can carry a person's identity:
// connectors and drives.
type Lending interface {
	// Lent is what userID lent in the Workspace.
	Lent(spaceID, userID string) []access.Lend
	// Lending loads one resource with its Workspace.
	Lending(id string) (l access.Lend, spaceID string, ok bool)
	// Grantees are the Bots a resource is granted to.
	Grantees(id string) []string
	// SetShared records the lender's say-so.
	SetShared(id string, shared bool)
	// Unlend takes the lender's identity out of a resource: a personal one
	// is deleted, a shared one signed out (why says so on it).
	Unlend(ctx context.Context, l access.Lend, why string)
}

// Lend registers a domain whose resources can be lent, by kind.
func (s *Service) Lend(kind string, l Lending) {
	if s.lenders == nil {
		s.lenders = map[string]Lending{}
	}
	s.lenders[kind] = l
}

func invalid(msg string) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(msg))
}

func failed(msg string) error {
	return connect.NewError(connect.CodeFailedPrecondition, errors.New(msg))
}

func deny(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// botNames are the names of the Bots, sorted, as one phrase.
func (s *Service) botNames(ids []string) string {
	var names []string
	s.db.Model(&db.Bot{}).Where("id IN ?", ids).Order("name").Pluck("name", &names)
	return strings.Join(names, ", ")
}

// managers counts a Bot's managers.
func (s *Service) managers(botID string) int64 {
	var n int64
	s.db.Model(&db.BotAccess{}).Where("bot_id = ? AND role = ?", botID, access.RoleManage).Count(&n)
	return n
}

// soleManaged are the Workspace's Bots whose only manager is the user.
func (s *Service) soleManaged(spaceID, userID string) []db.Bot {
	var bots []db.Bot
	s.db.Joins("JOIN bot_access a ON a.bot_id = bots.id AND a.user_id = ? AND a.role = ?", userID, access.RoleManage).
		Where("bots.space_id = ?", spaceID).
		Where("NOT EXISTS (SELECT 1 FROM bot_access o WHERE o.bot_id = bots.id AND o.role = ? AND o.user_id <> ?)", access.RoleManage, userID).
		Order("bots.name").Find(&bots)
	return bots
}

func (s *Service) person(spaceID string, m *db.SpaceMember, you string) *v1.SpacePerson {
	var u db.User
	s.db.Select("email", "disabled").Where("id = ?", m.UserID).Limit(1).Find(&u)
	var bots int64
	s.db.Model(&db.BotAccess{}).Joins("JOIN bots ON bots.id = bot_access.bot_id").
		Where("bot_access.user_id = ? AND bots.space_id = ?", m.UserID, spaceID).Count(&bots)
	return &v1.SpacePerson{UserId: m.UserID, Email: u.Email, Role: m.Role, Bots: int32(bots), You: m.UserID == you, Disabled: u.Disabled, JoinedAt: stamp(m.CreatedAt)}
}

func (s *Service) ListSpacePeople(ctx context.Context, req *connect.Request[v1.ListSpacePeopleRequest]) (*connect.Response[v1.ListSpacePeopleResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.View)
	if err != nil {
		return nil, err
	}
	var rows []db.SpaceMember
	s.db.Where("space_id = ?", sp.ID).Order("created_at").Find(&rows)
	out := &v1.ListSpacePeopleResponse{InviteSignup: s.cfg().Spaces.InviteSignup}
	for i := range rows {
		out.People = append(out.People, s.person(sp.ID, &rows[i], access.User(ctx).ID))
	}
	return connect.NewResponse(out), nil
}

// mayChange reports whether a caller with role may change or remove a
// person whose role is theirs: the owner anyone else, an admin members and
// guests.
func mayChange(caller, theirs string) error {
	switch {
	case theirs == access.RoleOwner:
		return failed("The owner stays: transfer ownership first.")
	case caller == access.RoleOwner:
		return nil
	case access.Rank(caller) >= access.Admin && access.Rank(theirs) < access.Admin:
		return nil
	}
	return deny("Only the workspace's owner can change an admin.")
}

func (s *Service) SetSpaceRole(ctx context.Context, req *connect.Request[v1.SetSpaceRoleRequest]) (*connect.Response[v1.SpacePerson], error) {
	sp, caller, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	role := req.Msg.GetRole()
	if role != access.RoleAdmin && role != access.RoleMember && role != access.RoleGuest {
		return nil, invalid("a role is admin, member or guest")
	}
	var m db.SpaceMember
	if s.db.Where("space_id = ? AND user_id = ?", sp.ID, req.Msg.GetUserId()).Limit(1).Find(&m); m.UserID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("they are not in this workspace"))
	}
	if m.UserID == access.User(ctx).ID {
		return nil, failed("You cannot change your own role.")
	}
	if err := mayChange(caller, m.Role); err != nil {
		return nil, err
	}
	if role == access.RoleAdmin && caller != access.RoleOwner {
		return nil, deny("Only the workspace's owner can make an admin.")
	}
	if role == access.RoleGuest {
		// A guest only uses Bots: their Can manage becomes Can use, which
		// must leave every Bot a manager.
		if bots := s.soleManaged(sp.ID, m.UserID); len(bots) > 0 {
			var names []string
			for _, b := range bots {
				names = append(names, b.Name)
			}
			return nil, failed(fmt.Sprintf("They are the only manager of %s. Make someone else a manager first.", strings.Join(names, ", ")))
		}
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.SpaceMember{}).Where("space_id = ? AND user_id = ?", sp.ID, m.UserID).Update("role", role).Error; err != nil {
			return err
		}
		if role != access.RoleGuest {
			return nil
		}
		return tx.Model(&db.BotAccess{}).Where("user_id = ? AND role = ? AND bot_id IN (?)", m.UserID, access.RoleManage,
			tx.Model(&db.Bot{}).Select("id").Where("space_id = ?", sp.ID)).Update("role", access.RoleUse).Error
	})
	if err != nil {
		return nil, err
	}
	m.Role = role
	return connect.NewResponse(s.person(sp.ID, &m, access.User(ctx).ID)), nil
}

func (s *Service) TransferSpace(ctx context.Context, req *connect.Request[v1.TransferSpaceRequest]) (*connect.Response[v1.Space], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Own)
	if err != nil {
		return nil, err
	}
	me := access.User(ctx).ID
	if access.SpaceRole(s.db, sp.ID, req.Msg.GetUserId()) != access.RoleAdmin {
		return nil, failed("Only an admin can become the owner. Make them an admin first.")
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.SpaceMember{}).Where("space_id = ? AND user_id = ?", sp.ID, me).Update("role", access.RoleAdmin).Error; err != nil {
			return err
		}
		return tx.Model(&db.SpaceMember{}).Where("space_id = ? AND user_id = ?", sp.ID, req.Msg.GetUserId()).Update("role", access.RoleOwner).Error
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.view(sp, access.RoleAdmin, me)), nil
}

// removal checks that the caller may remove userID from the Workspace (or
// leave it: their own id) and returns the Workspace and their member row.
func (s *Service) removal(ctx context.Context, spaceID, userID string) (*db.Space, *db.SpaceMember, error) {
	me := access.User(ctx)
	need := access.Admin
	if me != nil && userID == me.ID {
		need = access.View
	}
	sp, caller, err := access.Space(ctx, s.db, spaceID, need)
	if err != nil {
		return nil, nil, err
	}
	var m db.SpaceMember
	if s.db.Where("space_id = ? AND user_id = ?", sp.ID, userID).Limit(1).Find(&m); m.UserID == "" {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("they are not in this workspace"))
	}
	if m.Role == access.RoleOwner {
		return nil, nil, failed("The owner cannot leave: transfer ownership first.")
	}
	if userID != me.ID {
		if err := mayChange(caller, m.Role); err != nil {
			return nil, nil, err
		}
	}
	return sp, &m, nil
}

// lentProto is a lent resource with the Bots it is in that the caller sees.
func (s *Service) lentProto(ctx context.Context, kind string, l access.Lend) *v1.Lent {
	out := &v1.Lent{Kind: kind, Id: l.ID, Name: l.Name, Shared: l.Shared}
	me := access.User(ctx).ID
	for _, b := range s.lenders[kind].Grantees(l.ID) {
		if access.BotRole(s.db, b, me) == "" {
			out.BotsHidden++
			continue
		}
		var bot db.Bot
		s.db.Select("name").Where("id = ?", b).Limit(1).Find(&bot)
		out.Bots = append(out.Bots, bot.Name)
	}
	return out
}

func (s *Service) kinds() []string {
	var out []string
	for k := range s.lenders {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Service) PlanSpaceRemoval(ctx context.Context, req *connect.Request[v1.PlanSpaceRemovalRequest]) (*connect.Response[v1.SpaceRemovalPlan], error) {
	sp, m, err := s.removal(ctx, req.Msg.GetSpaceId(), req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	me := access.User(ctx).ID
	out := &v1.SpaceRemovalPlan{UserId: m.UserID, Email: access.Email(s.db, m.UserID)}
	for _, b := range s.soleManaged(sp.ID, m.UserID) {
		lm := &v1.LastManaged{BotId: b.ID, Name: b.Name, Crest: int32(b.Crest), CallerOn: access.BotRole(s.db, b.ID, me) != ""}
		for _, p := range s.botPeople(b.ID, sp.ID, me) {
			if p.UserId != m.UserID {
				lm.Others = append(lm.Others, p)
			}
		}
		out.LastManaged = append(out.LastManaged, lm)
	}
	for _, k := range s.kinds() {
		for _, l := range s.lenders[k].Lent(sp.ID, m.UserID) {
			out.Lent = append(out.Lent, s.lentProto(ctx, k, l))
		}
	}
	return connect.NewResponse(out), nil
}

func (s *Service) RemoveSpacePerson(ctx context.Context, req *connect.Request[v1.RemoveSpacePersonRequest]) (*connect.Response[v1.RemoveSpacePersonResponse], error) {
	sp, m, err := s.removal(ctx, req.Msg.GetSpaceId(), req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if err := s.Remove(ctx, sp.ID, m.UserID, access.User(ctx).ID, req.Msg.GetPlan()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.RemoveSpacePersonResponse{}), nil
}

// Remove takes a person out of a Workspace: what they lent leaves with them
// (personal deleted, shared signed out), each Bot they were the last
// manager of goes by plan (manager: another person on it; take: by, who is
// removing them; delete), and their access ends. Their chats stay with the
// Bots. An empty plan with such Bots is refused, naming them.
func (s *Service) Remove(ctx context.Context, spaceID, userID, by string, plan []*v1.Handoff) error {
	byBot := map[string]*v1.Handoff{}
	for _, h := range plan {
		byBot[h.GetBotId()] = h
	}
	sole := s.soleManaged(spaceID, userID)
	var missing []string
	for _, b := range sole {
		h := byBot[b.ID]
		if h == nil {
			missing = append(missing, b.Name)
			continue
		}
		switch h.GetAction() {
		case "delete":
		case "take":
			if by == userID || by == "" || access.Rank(access.SpaceRole(s.db, spaceID, by)) < access.Build {
				return invalid(fmt.Sprintf("%s needs another manager.", b.Name))
			}
		case "manager":
			if h.GetUserId() == userID || access.BotRole(s.db, b.ID, h.GetUserId()) == "" {
				return invalid(fmt.Sprintf("Pick someone on %s to manage it.", b.Name))
			}
			if access.SpaceRole(s.db, spaceID, h.GetUserId()) == access.RoleGuest {
				return invalid("A guest can only use a Bot.")
			}
		default:
			return invalid(fmt.Sprintf("Choose what happens to %s.", b.Name))
		}
	}
	if len(missing) > 0 {
		return failed(fmt.Sprintf("Choose what happens to %s: nobody else manages it.", strings.Join(missing, ", ")))
	}
	for _, b := range sole {
		h := byBot[b.ID]
		switch h.GetAction() {
		case "delete":
			s.host.DropBot(ctx, b.ID)
		case "take":
			s.setRole(b.ID, by, access.RoleManage)
		case "manager":
			s.setRole(b.ID, h.GetUserId(), access.RoleManage)
		}
	}
	who := access.Email(s.db, userID)
	for _, k := range s.kinds() {
		for _, l := range s.lenders[k].Lent(spaceID, userID) {
			s.lenders[k].Unlend(ctx, l, fmt.Sprintf("Signed out: %s left the workspace.", who))
		}
	}
	var bots []string
	s.db.Model(&db.BotAccess{}).Joins("JOIN bots ON bots.id = bot_access.bot_id").
		Where("bot_access.user_id = ? AND bots.space_id = ?", userID, spaceID).Pluck("bot_access.bot_id", &bots)
	s.db.Where("user_id = ? AND bot_id IN ?", userID, append(bots, "")).Delete(&db.BotAccess{})
	s.db.Where("space_id = ? AND created_by = ? AND used_at IS NULL", spaceID, userID).Delete(&db.SpaceInvite{})
	s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Delete(&db.SpaceMember{})
	for _, b := range bots {
		s.host.AccessEnded(b, userID)
	}
	return nil
}

// setRole puts the user on the Bot with role, or changes their role there.
func (s *Service) setRole(botID, userID, role string) {
	res := s.db.Model(&db.BotAccess{}).Where("bot_id = ? AND user_id = ?", botID, userID).Update("role", role)
	if res.RowsAffected == 0 {
		_ = s.Admit(botID, userID, role)
	}
}

// botPeople are the people on a Bot.
func (s *Service) botPeople(botID, spaceID, you string) []*v1.BotPerson {
	var rows []db.BotAccess
	s.db.Where("bot_id = ?", botID).Order("created_at").Find(&rows)
	out := make([]*v1.BotPerson, 0, len(rows))
	for _, r := range rows {
		out = append(out, &v1.BotPerson{UserId: r.UserID, Email: access.Email(s.db, r.UserID), Role: r.Role, SpaceRole: access.SpaceRole(s.db, spaceID, r.UserID), You: r.UserID == you})
	}
	return out
}

func (s *Service) ListBotPeople(ctx context.Context, req *connect.Request[v1.ListBotPeopleRequest]) (*connect.Response[v1.ListBotPeopleResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	me := access.User(ctx).ID
	out := &v1.ListBotPeopleResponse{People: s.botPeople(b.ID, b.SpaceID, me)}
	if access.BotRole(s.db, b.ID, me) == access.RoleManage {
		var rows []db.SpaceMember
		s.db.Where("space_id = ? AND user_id NOT IN (?)", b.SpaceID, s.db.Model(&db.BotAccess{}).Select("user_id").Where("bot_id = ?", b.ID)).
			Order("created_at").Find(&rows)
		for i := range rows {
			out.Candidates = append(out.Candidates, s.person(b.SpaceID, &rows[i], me))
		}
	}
	return connect.NewResponse(out), nil
}

// CheckJoin says whether userID may be put on a Bot of spaceID with role:
// they are in the Workspace, a guest only uses, and the Bot holds nobody
// else's personal sign-in.
func (s *Service) CheckJoin(botID, spaceID, userID, role string) error {
	if role != access.RoleManage && role != access.RoleUse {
		return invalid("a role on a Bot is manage or use")
	}
	sr := access.SpaceRole(s.db, spaceID, userID)
	if sr == "" {
		return invalid("They are not in this workspace. Invite them first.")
	}
	if sr == access.RoleGuest && role == access.RoleManage {
		return invalid("A guest can only use a Bot.")
	}
	return access.CheckAdd(s.db, botID, userID)
}

func (s *Service) AddBotPerson(ctx context.Context, req *connect.Request[v1.AddBotPersonRequest]) (*connect.Response[v1.AddBotPersonResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	userID, role := req.Msg.GetUserId(), req.Msg.GetRole()
	if access.BotRole(s.db, b.ID, userID) != "" {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("they are on this Bot already"))
	}
	if err := s.CheckJoin(b.ID, b.SpaceID, userID, role); err != nil {
		return nil, err
	}
	// Secrets are the Bot's: whoever uses it can have it use them. Say whose
	// they are first.
	if !req.Msg.GetConfirmed() {
		var names []string
		s.db.Model(&db.Secret{}).Where("bot_id = ? AND set_by <> '' AND set_by <> ? AND name NOT LIKE ?", b.ID, userID, "channel.%").
			Order("name").Pluck("name", &names)
		if len(names) > 0 {
			return connect.NewResponse(&v1.AddBotPersonResponse{ConfirmSecrets: names}), nil
		}
	}
	if err := s.Admit(b.ID, userID, role); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.AddBotPersonResponse{Person: &v1.BotPerson{
		UserId: userID, Email: access.Email(s.db, userID), Role: role, SpaceRole: access.SpaceRole(s.db, b.SpaceID, userID),
	}}), nil
}

func (s *Service) SetBotRole(ctx context.Context, req *connect.Request[v1.SetBotRoleRequest]) (*connect.Response[v1.BotPerson], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	userID, role := req.Msg.GetUserId(), req.Msg.GetRole()
	cur := access.BotRole(s.db, b.ID, userID)
	if cur == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("they are not on this Bot"))
	}
	if role != access.RoleManage && role != access.RoleUse {
		return nil, invalid("a role on a Bot is manage or use")
	}
	if role == access.RoleManage && access.SpaceRole(s.db, b.SpaceID, userID) == access.RoleGuest {
		return nil, invalid("A guest can only use a Bot.")
	}
	if cur == access.RoleManage && role == access.RoleUse && s.managers(b.ID) <= 1 {
		return nil, failed(fmt.Sprintf("%s needs a manager. Make someone else a manager first.", b.Name))
	}
	s.setRole(b.ID, userID, role)
	return connect.NewResponse(&v1.BotPerson{UserId: userID, Email: access.Email(s.db, userID), Role: role,
		SpaceRole: access.SpaceRole(s.db, b.SpaceID, userID), You: userID == access.User(ctx).ID}), nil
}

func (s *Service) RemoveBotPerson(ctx context.Context, req *connect.Request[v1.RemoveBotPersonRequest]) (*connect.Response[v1.RemoveBotPersonResponse], error) {
	userID := req.Msg.GetUserId()
	// Anyone may leave; taking someone else off is the Bot's managers' (or
	// a Workspace admin's) call.
	need := access.Administer
	if u := access.User(ctx); u != nil && u.ID == userID {
		need = access.Use
	}
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), need)
	if err != nil {
		return nil, err
	}
	cur := access.BotRole(s.db, b.ID, userID)
	if cur == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("they are not on this Bot"))
	}
	if cur == access.RoleManage && s.managers(b.ID) <= 1 {
		return nil, failed(fmt.Sprintf("%s needs a manager. Make someone else a manager first, or delete it.", b.Name))
	}
	s.db.Where("bot_id = ? AND user_id = ?", b.ID, userID).Delete(&db.BotAccess{})
	s.host.AccessEnded(b.ID, userID)
	return connect.NewResponse(&v1.RemoveBotPersonResponse{}), nil
}

func (s *Service) SetLendShared(ctx context.Context, req *connect.Request[v1.SetLendSharedRequest]) (*connect.Response[v1.SetLendSharedResponse], error) {
	kind, id := "connector", req.Msg.GetConnectorId()
	if id == "" {
		kind, id = "drive", req.Msg.GetDriveId()
	}
	lender := s.lenders[kind]
	if lender == nil {
		return nil, invalid("name a connector or a drive")
	}
	l, spaceID, ok := lender.Lending(id)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New(access.NoSpace))
	}
	if _, _, err := access.Space(ctx, s.db, spaceID, access.View); err != nil {
		return nil, err
	}
	if l.LentBy == "" || l.LentBy != access.User(ctx).ID {
		return nil, deny("Only whoever signed it in can share it.")
	}
	if !req.Msg.GetShared() {
		if err := access.CheckUnshare(s.db, l, lender.Grantees(id)); err != nil {
			return nil, err
		}
	}
	lender.SetShared(id, req.Msg.GetShared())
	return connect.NewResponse(&v1.SetLendSharedResponse{}), nil
}

// DropUser takes a user who is being deleted out of every Workspace they
// share with someone: one they own passes to its oldest admin, else its
// oldest member, and the Bots they were the last manager of are deleted. The
// Workspaces they are alone in are the caller's to delete first.
func (s *Service) DropUser(ctx context.Context, userID string) {
	var rows []db.SpaceMember
	s.db.Where("user_id = ?", userID).Find(&rows)
	for _, m := range rows {
		if m.Role == access.RoleOwner {
			var heirs []db.SpaceMember
			s.db.Where("space_id = ? AND user_id <> ?", m.SpaceID, userID).Order("created_at").Find(&heirs)
			if len(heirs) == 0 {
				continue
			}
			heir := heirs[0]
			for _, rank := range []string{access.RoleAdmin, access.RoleMember} {
				if i := slices.IndexFunc(heirs, func(h db.SpaceMember) bool { return h.Role == rank }); i >= 0 {
					heir = heirs[i]
					break
				}
			}
			s.db.Model(&db.SpaceMember{}).Where("space_id = ? AND user_id = ?", m.SpaceID, userID).Update("role", access.RoleAdmin)
			s.db.Model(&db.SpaceMember{}).Where("space_id = ? AND user_id = ?", m.SpaceID, heir.UserID).Update("role", access.RoleOwner)
		}
		var plan []*v1.Handoff
		for _, b := range s.soleManaged(m.SpaceID, userID) {
			plan = append(plan, &v1.Handoff{BotId: b.ID, Action: "delete"})
		}
		if err := s.Remove(ctx, m.SpaceID, userID, "", plan); err != nil {
			log.Printf("drop user %s from workspace %s: %v", userID, m.SpaceID, err)
		}
	}
}
