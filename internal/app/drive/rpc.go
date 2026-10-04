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
		State: d.State, StateDetail: d.StateDetail, Options: decodeMap(d.OptionsJSON),
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

func (s *Service) ListDrives(ctx context.Context, req *connect.Request[v1.ListDrivesRequest]) (*connect.Response[v1.ListDrivesResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	out := &v1.ListDrivesResponse{BindOk: true}
	for _, d := range s.BotDrives(b.ID) {
		out.Drives = append(out.Drives, s.protoDrive(&d))
	}
	if id := req.Msg.GetDraftId(); id != "" {
		var d db.Drive
		if s.db.Where("id = ? AND bot_id = ? AND draft = ?", id, b.ID, true).Limit(1).Find(&d); d.ID != "" {
			out.Drives = append(out.Drives, s.protoDrive(&d))
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

// ownDrive loads a drive the signed-in user owns through its Bot.
func (s *Service) ownDrive(ctx context.Context, id string) (*db.Drive, *drives.Template, error) {
	var d db.Drive
	if s.db.Where("id = ?", id).Limit(1).Find(&d); d.ID == "" {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("drive not found"))
	}
	if _, err := access.OwnBot(ctx, s.db, d.BotID); err != nil {
		return nil, nil, err
	}
	t, ok := s.templates().Get(d.Template)
	if !ok {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("unknown drive type %q", d.Template))
	}
	return &d, t, nil
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
		b, err := access.OwnBot(ctx, s.db, m.GetBotId())
		if err != nil {
			return nil, err
		}
		tt, ok := s.templates().Get(m.GetTemplate())
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown drive type %q", m.GetTemplate()))
		}
		id := ids.New()
		d = &db.Drive{ID: id, BotID: b.ID, Template: tt.Key, Draft: true, Name: "~" + id, State: driveDraft}
		t = tt
	} else {
		var err error
		if d, t, err = s.ownDrive(ctx, m.GetId()); err != nil {
			return nil, err
		}
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
		s.db.Model(&db.Drive{}).Where("bot_id = ? AND name = ? AND id <> ?", d.BotID, name, d.ID).Count(&clash)
		if clash > 0 {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("this Bot already has a drive named %q", name))
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
	}
	if err := s.db.Save(d).Error; err != nil {
		return nil, err
	}
	if !d.Draft {
		s.applyDrives(d.BotID)
	}
	return connect.NewResponse(s.protoDrive(d)), nil
}

func (s *Service) DeleteDrive(ctx context.Context, req *connect.Request[v1.DeleteDriveRequest]) (*connect.Response[v1.DeleteDriveResponse], error) {
	d, _, err := s.ownDrive(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	s.db.Delete(&db.Drive{}, "id = ?", d.ID)
	if !d.Draft {
		s.applyDrives(d.BotID)
	}
	if dir := s.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "drives", d.BotID, "cache", d.ID))
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
	driveID := d.ID
	s.host.AwaitOAuth(state, driveAuthTTL, func(ctx context.Context, q url.Values) error {
		return s.completeDriveAuth(ctx, driveID, redirect, q)
	})
	return connect.NewResponse(&v1.BeginDriveAuthResponse{Url: u}), nil
}

// completeDriveAuth finishes a sign-in: token exchange, account label and
// other lookups, auto-picks, then a remount if the drive is live.
func (s *Service) completeDriveAuth(ctx context.Context, driveID, redirect string, q url.Values) error {
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
	s.db.Model(&db.Drive{}).Where("id = ?", d.ID).Updates(map[string]any{
		"secrets_json": mustJSONString(sec),
		"options_json": mustJSONString(opts),
	})
	if !d.Draft {
		s.applyDrives(d.BotID)
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
		s.applyDrives(d.BotID)
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
	b.WriteString("The owner mounted these remote drives. They are ordinary folders: use `read`, `write`, `grep`, the terminal, and Python on them like any workspace path. Every access goes over the network to the provider, so open specific paths — never walk or grep a whole drive (a workspace-wide `grep` skips drives; pass a path inside one to search it). If a drive's folder is empty or missing, it is disconnected: tell the owner to check the Drives tab rather than retrying. You cannot add or remove drives.\n")
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
