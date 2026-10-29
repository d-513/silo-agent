package drive

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/oauth2"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/drives"
	"silo.agent/internal/ids"
)

// driveNameRe is a drive's name: its folder in /workspace/drives.
var driveNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// driveAuthTTL bounds how long a started sign-in may take.
const driveAuthTTL = 15 * time.Minute

// Path is where a drive is mounted in the Bot's workspace.
func Path(name string) string { return "/workspace/drives/" + name }

func protoDriveVar(v drives.Var) *v1.DriveVar {
	out := &v1.DriveVar{
		Key: v.Key, Kind: v.Kind, Type: v.Type, Label: v.Label, Help: v.Help, Placeholder: v.Placeholder,
		DefaultValue: v.Default, Required: v.Required, Advanced: v.Advanced, Secret: v.IsSecret(),
		VisibleIf: v.VisibleIf, AutoPick: v.AutoPick, Source: v.Source,
	}
	for _, o := range v.Options {
		out.Options = append(out.Options, &v1.DriveOption{Value: o.Value, Label: o.Label, Detail: o.Detail, Extra: o.Extra})
	}
	return out
}

func (s *Service) protoDriveTemplate(t *drives.Template, public string) *v1.DriveTemplate {
	missing := t.MissingSystem(s.driveSystem(t.Key))
	out := &v1.DriveTemplate{
		Key: t.Key, Title: t.Title, Blurb: t.Blurb, Category: t.Category,
		Guide:     t.Guide,
		Setup:     strings.ReplaceAll(t.Setup, "{public_url}", public),
		IconSvg:   string(t.Icon),
		AuthKind:  t.Auth.Kind,
		AuthLabel: t.Auth.Label,
		Available: len(missing) == 0, MissingSystem: missing,
	}
	for _, v := range t.Vars {
		if v.Kind == drives.KindSystem {
			// The field list, never the values; the add form needs to know
			// what an admin still has to provide.
			out.Vars = append(out.Vars, &v1.DriveVar{Key: v.Key, Kind: v.Kind, Label: v.Label, Required: v.Required})
			continue
		}
		out.Vars = append(out.Vars, protoDriveVar(v))
	}
	return out
}

func (s *Service) driveSystem(key string) map[string]string {
	if s.store == nil {
		return map[string]string{}
	}
	return s.store.DriveSystem(key)
}

func (s *Service) protoDrive(d *db.Drive) *v1.Drive {
	out := &v1.Drive{
		Id: d.ID, BotId: d.BotID, Name: d.Name, Template: d.Template, ReadOnly: d.ReadOnly, Draft: d.Draft,
		State: d.State, StateDetail: d.StateDetail, Options: decodeMap(d.OptionsJSON), SpaceId: d.SpaceID, Shared: d.Shared,
	}
	if !d.Draft {
		out.Path = Path(d.Name)
	} else {
		out.Name = ""
	}
	sec := decodeSecrets(d.SecretsJSON)
	for k, v := range sec.User {
		if v != "" {
			out.SecretsSet = append(out.SecretsSet, k)
		}
	}
	slices.Sort(out.SecretsSet)
	out.Account = sec.Dynamic["account"]
	out.Connected = sec.Dynamic["token"] != ""
	return out
}

func (s *Service) ListDriveTemplates(ctx context.Context, _ *connect.Request[v1.ListDriveTemplatesRequest]) (*connect.Response[v1.ListDriveTemplatesResponse], error) {
	public := s.host.PublicURL(ctx)
	out := &v1.ListDriveTemplatesResponse{RedirectUrl: public + "/oauth/callback"}
	for _, t := range s.templates().All() {
		out.Templates = append(out.Templates, s.protoDriveTemplate(t, public))
	}
	return connect.NewResponse(out), nil
}

// ListDrives lists the drives granted to a Bot, as it mounts them, and the
// add form's draft started from it.
func (s *Service) ListDrives(ctx context.Context, req *connect.Request[v1.ListDrivesRequest]) (*connect.Response[v1.ListDrivesResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	out := &v1.ListDrivesResponse{BindOk: true}
	for _, d := range s.BotDrives(b.ID) {
		out.Drives = append(out.Drives, s.spaceDrive(ctx, &d))
	}
	if id := req.Msg.GetDraftId(); id != "" {
		var d db.Drive
		if s.db.Where("id = ? AND space_id = ? AND draft = ?", id, b.SpaceID, true).Limit(1).Find(&d); d.ID != "" {
			out.Drives = append(out.Drives, s.spaceDrive(ctx, &d))
		}
	}
	// A box created before drives existed has no /workspace/drives bind; it
	// sees nothing until it is recreated.
	if b.ContainerID != "" && s.docker != nil && len(out.Drives) > 0 {
		if ok, err := s.docker.HasDriveBind(ctx, b.ContainerID); err == nil && !ok {
			out.BindOk = false
		}
	}
	return connect.NewResponse(out), nil
}

// ListSpaceDrives lists a Workspace's drives (and the hub's draft).
func (s *Service) ListSpaceDrives(ctx context.Context, req *connect.Request[v1.ListSpaceDrivesRequest]) (*connect.Response[v1.ListDrivesResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Build)
	if err != nil {
		return nil, err
	}
	var rows []db.Drive
	s.db.Where("space_id = ? AND (draft = ? OR id = ?)", sp.ID, false, req.Msg.GetDraftId()).Order("name").Find(&rows)
	out := &v1.ListDrivesResponse{BindOk: true}
	for i := range rows {
		out.Drives = append(out.Drives, s.spaceDrive(ctx, &rows[i]))
	}
	return connect.NewResponse(out), nil
}

// spaceDrive is a drive with what its Workspace knows about it: who uses it,
// who added it, whose account it reaches, and whether the caller may edit it.
func (s *Service) spaceDrive(ctx context.Context, d *db.Drive) *v1.Drive {
	out := s.protoDrive(d)
	u := access.User(ctx)
	for _, b := range s.grantees(d.ID) {
		if u != nil && access.BotRole(s.db, b, u.ID) != "" {
			out.UsedBy = append(out.UsedBy, b)
		} else {
			out.UsedByHidden++
		}
	}
	out.CreatedBy, out.LentBy = s.email(d.CreatedBy), s.email(d.LentBy)
	out.CanEdit = s.canEdit(ctx, d) == nil
	return out
}

func (s *Service) email(userID string) string {
	if userID == "" {
		return ""
	}
	var u db.User
	s.db.Select("email").Where("id = ?", userID).Limit(1).Find(&u)
	return u.Email
}

// canEdit says whether the caller may change or delete a drive: who added
// it, or an admin of its Workspace. The change reaches every Bot that mounts
// it, so managing one of them is not enough.
func (s *Service) canEdit(ctx context.Context, d *db.Drive) error {
	_, role, err := access.Space(ctx, s.db, d.SpaceID, access.Build)
	if err != nil {
		return err
	}
	if u := access.User(ctx); u != nil && (d.CreatedBy == u.ID || access.Rank(role) >= access.Admin) {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("only whoever added this drive, or an admin of the workspace, can change it"))
}

// ownDrive loads a drive the caller may change.
func (s *Service) ownDrive(ctx context.Context, id string) (*db.Drive, *drives.Template, error) {
	var d db.Drive
	if s.db.Where("id = ?", id).Limit(1).Find(&d); d.ID == "" {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("drive not found"))
	}
	if err := s.canEdit(ctx, &d); err != nil {
		return nil, nil, err
	}
	t, ok := s.templates().Get(d.Template)
	if !ok {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("unknown drive type %q", d.Template))
	}
	return &d, t, nil
}

// readsAccount says whether the caller may look into the drive's account
// (browse its folders, pick from its lists): its lender, or with no lender,
// whoever may edit it. An admin may not browse someone else's files.
func (s *Service) readsAccount(ctx context.Context, d *db.Drive) error {
	if d.LentBy == "" {
		return nil
	}
	if u := access.User(ctx); u != nil && u.ID == d.LentBy {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("only whoever signed this drive in can look into it"))
}

// GrantDrive mounts a Workspace drive in one of its Bots.
func (s *Service) GrantDrive(ctx context.Context, req *connect.Request[v1.GrantDriveRequest]) (*connect.Response[v1.Drive], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	var d db.Drive
	if s.db.Where("id = ? AND draft = ?", req.Msg.GetDriveId(), false).Limit(1).Find(&d); d.ID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("drive not found"))
	}
	if err := s.grant(b.ID, &d, req.Msg.GetReadOnly()); err != nil {
		return nil, err
	}
	access.Activity(s.db, d.SpaceID, access.User(ctx).ID, access.Granted, d.Name, b.ID, "drive")
	s.applyDrives(b.ID)
	return connect.NewResponse(s.spaceDrive(ctx, &d)), nil
}

// grant gives a drive to a Bot of its Workspace (read-only when asked).
func (s *Service) grant(botID string, d *db.Drive, readOnly bool) error {
	var b db.Bot
	if s.db.Where("id = ?", botID).Limit(1).Find(&b); b.ID == "" || b.SpaceID == "" || b.SpaceID != d.SpaceID {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot and the drive are in different workspaces"))
	}
	if err := access.CheckLend(s.db, lend(d), botID); err != nil {
		return err
	}
	var n int64
	s.db.Model(&db.BotDrive{}).Where("bot_id = ? AND drive_id = ?", botID, d.ID).Count(&n)
	if n > 0 {
		s.db.Model(&db.BotDrive{}).Where("bot_id = ? AND drive_id = ?", botID, d.ID).Update("read_only", readOnly)
		return nil
	}
	return s.db.Create(&db.BotDrive{BotID: botID, DriveID: d.ID, ReadOnly: readOnly, State: driveMounting, CreatedAt: time.Now()}).Error
}

// RevokeDrive unmounts a drive from one Bot; the drive stays in the
// Workspace.
func (s *Service) RevokeDrive(ctx context.Context, req *connect.Request[v1.RevokeDriveRequest]) (*connect.Response[v1.RevokeDriveResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Administer)
	if err != nil {
		return nil, err
	}
	res := s.db.Where("bot_id = ? AND drive_id = ?", b.ID, req.Msg.GetDriveId()).Delete(&db.BotDrive{})
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the Bot does not mount that drive"))
	}
	s.revoked(b.ID, req.Msg.GetDriveId())
	var d db.Drive
	if s.db.Where("id = ?", req.Msg.GetDriveId()).Limit(1).Find(&d); d.ID != "" {
		access.Activity(s.db, d.SpaceID, access.User(ctx).ID, access.Revoked, d.Name, b.ID, "drive")
	}
	return connect.NewResponse(&v1.RevokeDriveResponse{}), nil
}

// revoked unmounts what a Bot lost and drops its cache of it.
func (s *Service) revoked(botID, driveID string) {
	s.applyDrives(botID)
	if dir := s.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "drives", botID, "cache", driveID))
	}
}

// checkUserValue validates one answer against its field type.
func checkUserValue(v drives.Var, val string) error {
	if val == "" {
		return nil
	}
	switch v.Type {
	case drives.TypeURL:
		u, err := url.Parse(val)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be a web address starting with https://", v.Label)
		}
	case drives.TypeNumber:
		if _, err := strconv.Atoi(val); err != nil {
			return fmt.Errorf("%s must be a number", v.Label)
		}
	case drives.TypeBool:
		if val != "true" && val != "false" {
			return fmt.Errorf("%s must be true or false", v.Label)
		}
	case drives.TypeSelect:
		if !slices.ContainsFunc(v.Options, func(o drives.Option) bool { return o.Value == val }) {
			return fmt.Errorf("%s: %q is not one of the choices", v.Label, val)
		}
	case drives.TypeFolder:
		if strings.Contains(val, "..") {
			return fmt.Errorf("%s must not contain ..", v.Label)
		}
	}
	if len(val) > 16<<10 {
		return fmt.Errorf("%s is too long", v.Label)
	}
	return nil
}

// missingMessage turns a MissingError into one sentence for a human.
func missingMessage(t *drives.Template, m *drives.MissingError) string {
	var labels []string
	for _, x := range m.Missing {
		if v, ok := t.Var(x.Kind, x.Key); ok && v.Label != "" {
			labels = append(labels, v.Label)
		} else {
			labels = append(labels, x.Key)
		}
	}
	switch m.State() {
	case drives.StateNeedsSetup:
		return "An admin has to set up " + t.Title + " first (Admin → Drives)."
	case drives.StateNeedsAuth:
		return "Sign in to " + t.Title + " first."
	}
	return "Fill in " + strings.Join(labels, ", ") + "."
}

func (s *Service) SaveDrive(ctx context.Context, req *connect.Request[v1.SaveDriveRequest]) (*connect.Response[v1.Drive], error) {
	m := req.Msg
	var d *db.Drive
	var t *drives.Template
	if m.GetId() == "" {
		spaceID, via, err := s.draftPlace(ctx, m.GetBotId(), m.GetSpaceId())
		if err != nil {
			return nil, err
		}
		tt, ok := s.templates().Get(m.GetTemplate())
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown drive type %q", m.GetTemplate()))
		}
		id := ids.New()
		d = &db.Drive{ID: id, SpaceID: spaceID, BotID: via, CreatedBy: access.User(ctx).ID, Template: tt.Key, Draft: true, Name: "~" + id, State: driveDraft}
		t = tt
	} else {
		var err error
		if d, t, err = s.ownDrive(ctx, m.GetId()); err != nil {
			return nil, err
		}
	}
	// Every Bot it is to go to is one the caller manages in its Workspace.
	grantTo := []string{}
	for _, id := range m.GetBotIds() {
		b, err := access.Bot(ctx, s.db, id, access.Manage)
		if err != nil {
			return nil, err
		}
		if b.SpaceID != d.SpaceID {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot and the drive are in different workspaces"))
		}
		grantTo = append(grantTo, b.ID)
	}

	opts := map[string]string{}
	sec := decodeSecrets(d.SecretsJSON)
	for k, val := range m.GetOptions() {
		v, ok := t.Var(drives.KindUser, k)
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s has no field %q", t.Title, k))
		}
		val = strings.TrimSpace(val)
		if v.Type == drives.TypeArea {
			val = strings.TrimRight(m.GetOptions()[k], " \t\r\n")
		}
		if err := checkUserValue(v, val); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if v.IsSecret() {
			if val != "" {
				sec.User[k] = val
			}
			continue
		}
		if val != "" {
			opts[k] = val
		}
	}
	for _, k := range m.GetClearSecrets() {
		delete(sec.User, k)
	}
	// Credentials typed in now are the typist's to lend.
	if mustJSONString(sec.User) != mustJSONString(decodeSecrets(d.SecretsJSON).User) && len(sec.User) > 0 && d.LentBy != access.User(ctx).ID {
		// A new lender starts personal.
		d.LentBy, d.Shared = access.User(ctx).ID, false
		if err := access.CheckSignIn(s.db, d.Name, d.LentBy, false, s.grantees(d.ID)); err != nil {
			return nil, err
		}
	}
	d.OptionsJSON = mustJSONString(opts)
	d.SecretsJSON = mustJSONString(sec)
	d.ReadOnly = m.GetReadOnly()

	wasDraft := d.Draft
	if !m.GetDraft() {
		name := strings.TrimSpace(m.GetName())
		if !driveNameRe.MatchString(name) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a drive name is lowercase letters, digits, and dashes (up to 40)"))
		}
		var clash int64
		s.db.Model(&db.Drive{}).Where("space_id = ? AND name = ? AND id <> ?", d.SpaceID, name, d.ID).Count(&clash)
		if clash > 0 {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("this workspace already has a drive named %q", name))
		}
		// Adding a drive must be complete: a system value an admin later
		// removes is fine (the row says so), but the owner's own part is not.
		if err := t.Check(s.driveValues(d, t)); err != nil {
			if mi, ok := drives.AsMissing(err); ok && mi.State() != drives.StateNeedsSetup {
				return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(missingMessage(t, mi)))
			}
		}
		d.Name = name
		d.Draft = false
		if wasDraft || d.State == "" || d.State == driveDraft {
			d.State = driveMounting
			d.StateDetail = ""
		}
		// A draft started from a Bot goes to that Bot; a saved drive lists
		// through the Bots it is granted to.
		if wasDraft && d.BotID != "" && !slices.Contains(grantTo, d.BotID) {
			grantTo = append([]string{d.BotID}, grantTo...)
		}
		d.BotID = ""
	}
	if err := s.db.Save(d).Error; err != nil {
		return nil, err
	}
	if !d.Draft {
		for _, b := range grantTo {
			if err := s.grant(b, d, false); err != nil {
				return nil, err
			}
		}
		s.applyAll(d.ID)
	}
	return connect.NewResponse(s.spaceDrive(ctx, d)), nil
}

// draftPlace is where a new drive is started: the Workspace of the Bot it is
// added from (which lists it), or the one the hub names, listed through a Bot
// the caller manages there if there is one.
func (s *Service) draftPlace(ctx context.Context, botID, spaceID string) (string, string, error) {
	if botID != "" {
		b, err := access.Bot(ctx, s.db, botID, access.Manage)
		if err != nil {
			return "", "", err
		}
		return b.SpaceID, b.ID, nil
	}
	sp, _, err := access.Space(ctx, s.db, spaceID, access.Build)
	if err != nil {
		return "", "", err
	}
	var via string
	s.db.Model(&db.Bot{}).Joins("JOIN bot_access a ON a.bot_id = bots.id").
		Where("bots.space_id = ? AND a.user_id = ? AND a.role = ?", sp.ID, access.User(ctx).ID, access.RoleManage).
		Order("bots.created_at").Limit(1).Pluck("bots.id", &via)
	return sp.ID, via, nil
}

func (s *Service) DeleteDrive(ctx context.Context, req *connect.Request[v1.DeleteDriveRequest]) (*connect.Response[v1.DeleteDriveResponse], error) {
	d, _, err := s.ownDrive(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	// It goes from every Bot that mounts it.
	bots := s.grantees(d.ID)
	s.db.Where("drive_id = ?", d.ID).Delete(&db.BotDrive{})
	s.db.Delete(&db.Drive{}, "id = ?", d.ID)
	for _, b := range bots {
		s.revoked(b, d.ID)
	}
	return connect.NewResponse(&v1.DeleteDriveResponse{}), nil
}

func (s *Service) BeginDriveAuth(ctx context.Context, req *connect.Request[v1.BeginDriveAuthRequest]) (*connect.Response[v1.BeginDriveAuthResponse], error) {
	d, t, err := s.ownDrive(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if t.Auth.Kind != drives.AuthOAuth2 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(t.Title+" does not use sign-in"))
	}
	state := ids.New()
	redirect := s.host.PublicURL(ctx) + "/oauth/callback"
	u, err := t.AuthCodeURL(s.driveValues(d, t), redirect, state)
	if err != nil {
		if m, ok := drives.AsMissing(err); ok {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(missingMessage(t, m)))
		}
		return nil, err
	}
	driveID, starter, shared := d.ID, access.User(ctx).ID, req.Msg.GetShared()
	// Whoever signs in lends their account: a personal sign-in may only
	// reach Bots they alone use.
	if err := access.CheckSignIn(s.db, d.Name, starter, shared, s.grantees(d.ID)); err != nil {
		return nil, err
	}
	s.host.AwaitOAuth(state, driveAuthTTL, func(ctx context.Context, q url.Values) error {
		return s.completeDriveAuth(ctx, driveID, starter, shared, redirect, q)
	})
	return connect.NewResponse(&v1.BeginDriveAuthResponse{Url: u}), nil
}

// completeDriveAuth finishes a sign-in: token exchange, account label and
// other lookups, auto-picks, then a remount if the drive is live.
func (s *Service) completeDriveAuth(ctx context.Context, driveID, starter string, shared bool, redirect string, q url.Values) error {
	if e := q.Get("error"); e != "" {
		if d := q.Get("error_description"); d != "" {
			return errors.New(d)
		}
		return errors.New("the provider refused: " + e)
	}
	var d db.Drive
	if s.db.Where("id = ?", driveID).Limit(1).Find(&d); d.ID == "" {
		return errors.New("this drive was removed while signing in")
	}
	t, ok := s.templates().Get(d.Template)
	if !ok {
		return errors.New("unknown drive type")
	}
	v := s.driveValues(&d, t)
	dyn := map[string]string{}
	for k, val := range t.CallbackValues(q) {
		dyn[k] = val
	}
	v.Dynamic = dyn
	tok, err := t.Exchange(s.driveCtx(ctx), v, redirect, q.Get("code"))
	if err != nil {
		return fmt.Errorf("could not finish signing in: %w", err)
	}
	dyn["token"] = tok
	if res, err := t.Resolve(ctx, s.httpClient(), v); err == nil {
		for k, val := range res {
			dyn[k] = val
		}
	} else {
		log.Printf("drive %s: lookup after sign-in: %v", d.ID, err)
	}
	// Auto-pick fields (OneDrive's drive) fill in only when empty, so a
	// reconnect never overrides a library the owner chose.
	opts := decodeMap(d.OptionsJSON)
	for _, pv := range t.VarsOf(drives.KindUser) {
		if pv.Type != drives.TypePick || !pv.AutoPick || opts[pv.Key] != "" {
			continue
		}
		choices, err := t.Pick(ctx, s.httpClient(), drives.Values{User: opts, System: v.System, Dynamic: dyn}, pv.Key)
		if err != nil || len(choices) == 0 {
			continue
		}
		opts[pv.Key] = choices[0].Value
		for k, val := range choices[0].Extra {
			opts[k] = val
		}
	}
	sec := decodeSecrets(d.SecretsJSON)
	sec.Dynamic = dyn
	// The account is now the drive's, lent by whoever signed in.
	s.db.Model(&db.Drive{}).Where("id = ?", d.ID).Updates(map[string]any{
		"secrets_json": mustJSONString(sec),
		"options_json": mustJSONString(opts),
		"lent_by":      starter,
		"shared":       shared,
	})
	if !d.Draft {
		access.Activity(s.db, d.SpaceID, starter, access.SignedInTo, d.Name, "", "drive")
	}
	if !d.Draft {
		s.applyAll(d.ID)
	}
	return nil
}

// freshDriveToken renews an expired token before a lookup or listing, storing
// it (and remounting with it) so the sidecar and the CP never diverge.
func (s *Service) freshDriveToken(ctx context.Context, d *db.Drive, t *drives.Template) error {
	if t.Auth.Kind != drives.AuthOAuth2 {
		return nil
	}
	tok, err := t.Refresh(s.driveCtx(ctx), s.driveValues(d, t))
	if err != nil || tok == "" {
		return err
	}
	sec := decodeSecrets(d.SecretsJSON)
	sec.Dynamic["token"] = tok
	d.SecretsJSON = mustJSONString(sec)
	s.db.Model(&db.Drive{}).Where("id = ?", d.ID).Update("secrets_json", d.SecretsJSON)
	if !d.Draft {
		s.applyAll(d.ID)
	}
	return nil
}

// driveCtx carries DriveHTTP to the oauth2 package.
func (s *Service) driveCtx(ctx context.Context) context.Context {
	if s.httpClient() == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, s.httpClient())
}

// driveErr maps engine errors to what the form should show.
func driveErr(t *drives.Template, err error) error {
	if m, ok := drives.AsMissing(err); ok {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New(missingMessage(t, m)))
	}
	return connect.NewError(connect.CodeUnavailable, err)
}

func (s *Service) PickDriveOptions(ctx context.Context, req *connect.Request[v1.PickDriveOptionsRequest]) (*connect.Response[v1.PickDriveOptionsResponse], error) {
	d, t, err := s.ownDrive(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.readsAccount(ctx, d); err != nil {
		return nil, err
	}
	if err := s.freshDriveToken(ctx, d, t); err != nil {
		return nil, driveErr(t, err)
	}
	opts, err := t.Pick(ctx, s.httpClient(), s.driveValues(d, t), req.Msg.GetKey())
	if err != nil {
		return nil, driveErr(t, err)
	}
	out := &v1.PickDriveOptionsResponse{}
	for _, o := range opts {
		out.Options = append(out.Options, &v1.DriveOption{Value: o.Value, Label: o.Label, Detail: o.Detail, Extra: o.Extra})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) BrowseDrive(ctx context.Context, req *connect.Request[v1.BrowseDriveRequest]) (*connect.Response[v1.BrowseDriveResponse], error) {
	d, t, err := s.ownDrive(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.readsAccount(ctx, d); err != nil {
		return nil, err
	}
	if err := s.freshDriveToken(ctx, d, t); err != nil {
		return nil, driveErr(t, err)
	}
	// Browse from the remote's root: the folder field holds a path from there.
	probe := *d
	opts := decodeMap(probe.OptionsJSON)
	for _, v := range t.VarsOf(drives.KindUser) {
		if v.Type == drives.TypeFolder {
			delete(opts, v.Key)
		}
	}
	probe.OptionsJSON = mustJSONString(opts)
	res, err := s.listDrive(ctx, &probe, req.Msg.GetPath())
	if err != nil {
		return nil, driveErr(t, err)
	}
	if e := res.GetError(); e != "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(e))
	}
	out := &v1.BrowseDriveResponse{}
	for _, dir := range res.GetDirs() {
		out.Dirs = append(out.Dirs, &v1.BrowseDriveDir{Name: dir.GetName(), Path: dir.GetPath()})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) driveSettings(ctx context.Context) *v1.DriveSettings {
	out := &v1.DriveSettings{RedirectUrl: s.host.PublicURL(ctx) + "/oauth/callback"}
	for _, t := range s.templates().All() {
		vars := t.VarsOf(drives.KindSystem)
		if len(vars) == 0 {
			continue
		}
		sys := s.driveSystem(t.Key)
		p := &v1.DriveProviderSettings{Template: t.Key, Ready: len(t.MissingSystem(sys)) == 0}
		for _, v := range vars {
			key := config.DriveSystemKey(t.Key, v.Key)
			f := &v1.DriveSystemField{
				Key: v.Key, Label: v.Label, Help: v.Help, Type: v.Type, Secret: v.IsSecret(), Required: v.Required,
				Set: sys[v.Key] != "", EnvName: config.EnvName(key),
			}
			if s.store != nil {
				f.Source = string(s.store.Source(key))
			}
			if !f.Secret {
				f.Value = sys[v.Key]
			}
			p.Fields = append(p.Fields, f)
		}
		out.Providers = append(out.Providers, p)
	}
	return out
}

func (s *Service) GetDriveSettings(ctx context.Context, _ *connect.Request[v1.GetDriveSettingsRequest]) (*connect.Response[v1.DriveSettings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	return connect.NewResponse(s.driveSettings(ctx)), nil
}

func (s *Service) PutDriveSettings(ctx context.Context, req *connect.Request[v1.PutDriveSettingsRequest]) (*connect.Response[v1.DriveSettings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	t, ok := s.templates().Get(req.Msg.GetTemplate())
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown drive type"))
	}
	patch := map[string]string{}
	for k, val := range req.Msg.GetValues() {
		if _, ok := t.Var(drives.KindSystem, k); !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s has no setting %q", t.Title, k))
		}
		patch[config.DriveSystemKey(t.Key, k)] = strings.TrimSpace(val)
	}
	if s.store == nil {
		return nil, errors.New("no config store")
	}
	if err := s.store.Patch(patch); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	// Drives waiting on this setup can mount now.
	var bots []string
	s.db.Model(&db.Drive{}).Where("template = ? AND draft = ?", t.Key, false).Distinct().Pluck("bot_id", &bots)
	for _, b := range bots {
		s.applyDrives(b)
	}
	return connect.NewResponse(s.driveSettings(ctx)), nil
}

// PopupPage is the page the sign-in popup shows once the provider redirects
// back: it closes the window and tells the add form how it went.
func PopupPage(err error) string {
	msg, ok := "Connected. You can close this window.", "true"
	if err != nil {
		msg, ok = err.Error(), "false"
	}
	esc := html.EscapeString(msg)
	js := strconv.Quote(msg)
	return `<!doctype html><meta charset="utf-8"><title>Silo</title>` +
		`<body style="font:15px system-ui,sans-serif;padding:32px;color:#222">` +
		`<p>` + esc + `</p><script>try{window.opener&&window.opener.postMessage({silo:"drive-auth",ok:` + ok +
		`,message:` + js + `},"*")}catch(e){}` + map[bool]string{true: `setTimeout(function(){window.close()},600)`, false: ``}[err == nil] +
		`</script></body>`
}

// Prompt is the system-prompt section listing a Bot's drives. It carries only
// what rarely changes (names, providers, access) so it stays in the cached
// session tier; live mount state would bust the cache on every reconnect. ""
// when there are none.
func (s *Service) Prompt(list []db.Drive) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The owner mounted these remote drives. They are ordinary folders: use `read`, `write`, `grep`, the terminal, and Python on them like any workspace path. Every access goes over the network to the provider, so open specific paths — never walk or grep a whole drive (a workspace-wide `grep` skips drives; pass a path inside one to search it). If a drive's folder is empty or missing, it is disconnected: tell the owner to check it on the Workspace's Drives page rather than retrying. You cannot add or remove drives.\n")
	for _, d := range list {
		title := d.Template
		if t, ok := s.templates().Get(d.Template); ok {
			title = t.Title
		}
		line := fmt.Sprintf("- %s — %s", Path(d.Name), title)
		if d.ReadOnly {
			line += " (read-only)"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// lend is a drive as the lending checks see it.
func lend(d *db.Drive) access.Lend {
	return access.Lend{Kind: "drive", ID: d.ID, Name: d.Name, LentBy: d.LentBy, Shared: d.Shared}
}

// Lent is every drive userID lent in the Workspace.
func (s *Service) Lent(spaceID, userID string) []access.Lend {
	var rows []db.Drive
	s.db.Where("space_id = ? AND lent_by = ? AND draft = ?", spaceID, userID, false).Order("name").Find(&rows)
	out := make([]access.Lend, 0, len(rows))
	for i := range rows {
		out = append(out, lend(&rows[i]))
	}
	return out
}

// Lending loads a drive for the lending checks.
func (s *Service) Lending(id string) (access.Lend, string, bool) {
	var d db.Drive
	if s.db.Where("id = ? AND draft = ?", id, false).Limit(1).Find(&d); d.ID == "" {
		return access.Lend{}, "", false
	}
	return lend(&d), d.SpaceID, true
}

// Grantees are the Bots a drive is granted to.
func (s *Service) Grantees(driveID string) []string { return s.grantees(driveID) }

// SetShared records the lender's say-so on a drive.
func (s *Service) SetShared(id string, shared bool) {
	s.db.Model(&db.Drive{}).Where("id = ?", id).Update("shared", shared)
}

// Unlend takes the lender's account out of a drive: a personal one is
// unmounted everywhere and deleted; a shared one is signed out (its sign-in
// and typed passwords cleared) and waits for someone to sign it in again.
func (s *Service) Unlend(_ context.Context, l access.Lend, why string) {
	var d db.Drive
	if s.db.Where("id = ?", l.ID).Limit(1).Find(&d); d.ID == "" {
		return
	}
	bots := s.grantees(d.ID)
	if l.Personal() {
		s.db.Where("drive_id = ?", d.ID).Delete(&db.BotDrive{})
		s.db.Delete(&db.Drive{}, "id = ?", d.ID)
		for _, b := range bots {
			s.revoked(b, d.ID)
		}
		return
	}
	s.db.Model(&db.Drive{}).Where("id = ?", d.ID).Updates(map[string]any{"secrets_json": "", "lent_by": ""})
	s.db.Model(&db.BotDrive{}).Where("drive_id = ?", d.ID).Updates(map[string]any{"state": drives.StateNeedsAuth, "state_detail": why})
	s.applyAll(d.ID)
}

// CheckNewBot says whether a new Bot that the people will use may get these
// drives: each of its Workspace, none someone else's personal sign-in.
func (s *Service) CheckNewBot(spaceID string, ids, people []string) error {
	for _, id := range ids {
		var d db.Drive
		if s.db.Where("id = ? AND draft = ?", id, false).Limit(1).Find(&d); d.ID == "" {
			return connect.NewError(connect.CodeNotFound, errors.New("drive not found"))
		}
		if d.SpaceID != spaceID {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot and the drive are in different workspaces"))
		}
		if err := access.CheckLend(s.db, lend(&d), "", people...); err != nil {
			return err
		}
	}
	return nil
}

// GrantNew mounts the drives ticked for a new Bot.
func (s *Service) GrantNew(botID string, ids []string) {
	for _, id := range ids {
		var d db.Drive
		if s.db.Where("id = ? AND draft = ?", id, false).Limit(1).Find(&d); d.ID == "" {
			continue
		}
		if err := s.grant(botID, &d, false); err != nil {
			log.Printf("drive %s to new bot %s: %v", id, botID, err)
		}
	}
	if len(ids) > 0 {
		s.applyDrives(botID)
	}
}

// LenderChanged remounts every drive a user lent, after their account was
// disabled (it comes down) or enabled again (it goes back up).
func (s *Service) LenderChanged(userID string) {
	var ids []string
	s.db.Model(&db.Drive{}).Where("lent_by = ? AND draft = ?", userID, false).Pluck("id", &ids)
	for _, id := range ids {
		s.applyAll(id)
	}
}

// GrantNames are the names of the drives a Bot mounts.
func (s *Service) GrantNames(botID string) []string {
	var out []string
	for _, d := range s.BotDrives(botID) {
		out = append(out, d.Name)
	}
	return out
}

// RevokeAll unmounts every drive from a Bot (it is moving to another
// Workspace).
func (s *Service) RevokeAll(botID string) {
	ids := s.grantedIDs(botID)
	s.db.Where("bot_id = ?", botID).Delete(&db.BotDrive{})
	for _, id := range ids {
		s.revoked(botID, id)
	}
}

func (s *Service) grantedIDs(botID string) []string {
	var out []string
	s.db.Model(&db.BotDrive{}).Where("bot_id = ?", botID).Pluck("drive_id", &out)
	return out
}
