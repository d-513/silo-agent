package app_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/drives"
)

func driveHarness(t *testing.T) *apptest.H {
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	return apptest.New(t, apptest.WithConfigPath(path), apptest.WithDriveGuest())
}

func listDrives(t *testing.T, h *apptest.H, botID, draftID string) *v1.ListDrivesResponse {
	t.Helper()
	res, err := h.Client.ListDrives(h.Ctx(), connect.NewRequest(&v1.ListDrivesRequest{BotId: botID, DraftId: draftID}))
	if err != nil {
		t.Fatalf("ListDrives: %v", err)
	}
	return res.Msg
}

func findDrive(res *v1.ListDrivesResponse, id string) *v1.Drive {
	for _, d := range res.Drives {
		if d.Id == id {
			return d
		}
	}
	return nil
}

// waitDrive polls until the drive reaches state and returns it.
func waitDrive(t *testing.T, h *apptest.H, botID, id, state string) *v1.Drive {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last *v1.Drive
	for time.Now().Before(deadline) {
		if last = findDrive(listDrives(t, h, botID, ""), id); last != nil && last.State == state {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("drive %s never reached %q (last %+v)", id, state, last)
	return nil
}

func saveDrive(h *apptest.H, req *v1.SaveDriveRequest) (*v1.Drive, error) {
	res, err := h.Client.SaveDrive(h.Ctx(), connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func code(err error) connect.Code {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Code()
	}
	return connect.CodeUnknown
}

func TestDriveTemplatesAvailability(t *testing.T) {
	h := driveHarness(t)
	res, err := h.Client.ListDriveTemplates(h.Ctx(), connect.NewRequest(&v1.ListDriveTemplatesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*v1.DriveTemplate{}
	for _, tp := range res.Msg.Templates {
		byKey[tp.Key] = tp
	}
	if len(byKey) != 15 || !strings.HasSuffix(res.Msg.RedirectUrl, "/oauth/callback") {
		t.Fatalf("templates %d, redirect %q", len(byKey), res.Msg.RedirectUrl)
	}
	gd := byKey["gdrive"]
	if gd.Available || !slices.Equal(gd.MissingSystem, []string{"client_id", "client_secret"}) || gd.AuthLabel != "Google" {
		t.Fatalf("gdrive before setup: %+v", gd)
	}
	if !strings.Contains(gd.Setup, res.Msg.RedirectUrl) || strings.Contains(gd.Setup, "{public_url}") {
		t.Fatalf("setup copy must carry the real redirect uri: %q", gd.Setup)
	}
	if !byKey["s3"].Available || byKey["s3"].IconSvg == "" {
		t.Fatal("form templates need no admin setup")
	}

	if _, err := h.Client.PutDriveSettings(h.Ctx(), connect.NewRequest(&v1.PutDriveSettingsRequest{
		Template: "gdrive", Values: map[string]string{"client_id": "cid", "client_secret": "shh"},
	})); err != nil {
		t.Fatal(err)
	}
	set, err := h.Client.GetDriveSettings(h.Ctx(), connect.NewRequest(&v1.GetDriveSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range set.Msg.Providers {
		if p.Template != "gdrive" {
			continue
		}
		if !p.Ready {
			t.Fatal("gdrive not ready after setup")
		}
		for _, f := range p.Fields {
			if f.Key == "client_secret" && (f.Value != "" || !f.Set || !f.Secret) {
				t.Fatalf("secret leaked or unset: %+v", f)
			}
			if f.Key == "client_id" && f.Value != "cid" {
				t.Fatalf("client_id = %q", f.Value)
			}
		}
	}
	res, _ = h.Client.ListDriveTemplates(h.Ctx(), connect.NewRequest(&v1.ListDriveTemplatesRequest{}))
	for _, tp := range res.Msg.Templates {
		if tp.Key == "gdrive" && !tp.Available {
			t.Fatal("gdrive still unavailable")
		}
	}
	if _, err := h.Client.PutDriveSettings(h.Ctx(), connect.NewRequest(&v1.PutDriveSettingsRequest{
		Template: "gdrive", Values: map[string]string{"clientid": "typo"},
	})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown system key: %v", err)
	}
}

func TestDriveS3AddBrowseMountLifecycle(t *testing.T) {
	h := driveHarness(t)
	bot := h.CreateBot("Keeper")
	run := h.Drives.Runner

	draft, err := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "s3", Draft: true, Options: map[string]string{
		"access_key_id": "AKIA", "secret_access_key": "s3cret", "region": "eu-west-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !draft.Draft || draft.Name != "" || !slices.Equal(draft.SecretsSet, []string{"secret_access_key"}) {
		t.Fatalf("draft = %+v", draft)
	}
	if _, ok := draft.Options["secret_access_key"]; ok {
		t.Fatal("secret echoed in options")
	}
	// Drafts are not in the list unless asked for.
	if findDrive(listDrives(t, h, bot.Id, ""), draft.Id) != nil || findDrive(listDrives(t, h, bot.Id, draft.Id), draft.Id) == nil {
		t.Fatal("draft visibility wrong")
	}

	remote := drives.RemoteName("~" + draft.Id)
	run.Dirs[remote+":"] = []string{"photos", "backups"}
	run.Dirs[remote+":photos"] = []string{"2025", "2026"}
	br, err := h.Client.BrowseDrive(h.Ctx(), connect.NewRequest(&v1.BrowseDriveRequest{Id: draft.Id}))
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if len(br.Msg.Dirs) != 2 || br.Msg.Dirs[0].Name != "backups" {
		t.Fatalf("root dirs = %v", br.Msg.Dirs)
	}
	if h.Fake.DriveCreates.Load() != 1 {
		t.Fatalf("sidecar creates = %d", h.Fake.DriveCreates.Load())
	}
	spec, _ := h.Fake.DriveSpecFor(bot.Id)
	if !slices.ContainsFunc(spec.Env, func(e string) bool { return strings.HasPrefix(e, "SILO_DRIVE_TOKEN=") }) {
		t.Fatalf("sidecar env = %v", spec.Env)
	}
	br, err = h.Client.BrowseDrive(h.Ctx(), connect.NewRequest(&v1.BrowseDriveRequest{Id: draft.Id, Path: "photos"}))
	if err != nil || len(br.Msg.Dirs) != 2 || br.Msg.Dirs[1].Path != "photos/2026" {
		t.Fatalf("photos dirs = %v (%v)", br, err)
	}

	// Validation on the final save.
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "Bad Name", Options: map[string]string{"region": "eu-west-1"}}); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad name: %v", err)
	}
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "x", Options: map[string]string{"region": "mars-1"}}); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad select: %v", err)
	}
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "x", Options: map[string]string{"nope": "1"}}); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown field: %v", err)
	}
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "x", ClearSecrets: []string{"secret_access_key"}, Options: map[string]string{"access_key_id": "AKIA"}}); code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "Secret access key") {
		t.Fatalf("missing secret: %v", err)
	}

	// The failed save above must not have touched the stored secret.
	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "files", ReadOnly: true, Options: map[string]string{
		"access_key_id": "AKIA", "region": "eu-west-1", "folder": "photos",
	}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if d.Path != "/workspace/drives/files" || d.Draft {
		t.Fatalf("saved = %+v", d)
	}
	waitDrive(t, h, bot.Id, d.Id, "mounted")
	starts := run.Starts()
	last := starts[len(starts)-1]
	if !slices.Contains(last.Args, "files:photos") || !slices.Contains(last.Args, "--read-only") || !strings.HasSuffix(last.Args[2], "/files") {
		t.Fatalf("mount args = %v", last.Args)
	}
	if !slices.Contains(last.Env, "RCLONE_CONFIG_FILES_SECRET_ACCESS_KEY=s3cret") || !slices.Contains(last.Env, "RCLONE_CONFIG_FILES_REGION=eu-west-1") {
		t.Fatalf("mount env = %v", last.Env)
	}

	// A crash shows the provider's words, then the mount comes back.
	run.Crash("files", "2026/09/29 10:00:00 CRITICAL: Failed to create file system: 403 Forbidden\n")
	bad := waitDrive(t, h, bot.Id, d.Id, "error")
	if !strings.Contains(bad.StateDetail, "403 Forbidden") || strings.Contains(bad.StateDetail, "2026/") {
		t.Fatalf("detail = %q", bad.StateDetail)
	}
	waitDrive(t, h, bot.Id, d.Id, "mounted")

	// Same name twice is refused.
	other, _ := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "b2", Draft: true, Options: map[string]string{"key_id": "k", "app_key": "a"}})
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: other.Id, Name: "files", Options: map[string]string{"key_id": "k"}}); code(err) != connect.CodeAlreadyExists {
		t.Fatalf("duplicate name: %v", err)
	}

	// Rename: remounted under the new folder, the old one goes away.
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: d.Id, Name: "work", Options: map[string]string{
		"access_key_id": "AKIA", "region": "eu-west-1",
	}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Equal(run.MountedDirs(), []string{"work"}) {
		if time.Now().After(deadline) {
			t.Fatalf("mounted dirs = %v", run.MountedDirs())
		}
		time.Sleep(20 * time.Millisecond)
	}
	starts = run.Starts()
	if !slices.Contains(starts[len(starts)-1].Env, "RCLONE_CONFIG_WORK_SECRET_ACCESS_KEY=s3cret") {
		t.Fatal("secret lost across an update that left it out")
	}

	// Stop Bot stops the sidecar and marks drives stopped.
	if _, err := h.Client.StopBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatal(err)
	}
	if got := findDrive(listDrives(t, h, bot.Id, ""), d.Id); got.State != "stopped" {
		t.Fatalf("after stop: %q", got.State)
	}

	// Delete the drive, then the bot: sidecar and drive dir are gone.
	if _, err := h.Client.DeleteDrive(h.Ctx(), connect.NewRequest(&v1.DeleteDriveRequest{Id: d.Id})); err != nil {
		t.Fatal(err)
	}
	if len(listDrives(t, h, bot.Id, "").Drives) != 0 {
		t.Fatal("drive still listed")
	}
	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatal(err)
	}
	if h.Fake.DriveDrops.Load() == 0 || h.Fake.DriveDirExists(h.Store.Config().DriveMountRoot(), bot.Id) {
		t.Fatal("DeleteBot left the drive sidecar or dir")
	}
	var n int64
	h.DB.Model(&db.Drive{}).Where("bot_id = ?", bot.Id).Count(&n)
	if n != 0 {
		t.Fatalf("%d drive rows left", n)
	}
}

// fakeGoogle is a TLS server standing in for accounts.google.com,
// oauth2.googleapis.com, and the Drive API.
func fakeGoogle(t *testing.T) (*http.Client, *[]string) {
	var seen []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.PostForm.Get("code") != "good-code" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
		case "/drive/v3/about":
			_, _ = io.WriteString(w, `{"user":{"emailAddress":"sam@example.com"}}`)
		case "/drive/v3/drives":
			_, _ = io.WriteString(w, `{"drives":[{"id":"0AB","name":"Team Space"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	base := srv.Client().Transport
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r2 := r.Clone(r.Context())
		r2.URL.Scheme, r2.URL.Host, r2.Host = u.Scheme, u.Host, u.Host
		return base.RoundTrip(r2)
	})}, &seen
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDriveGoogleSignInPickAndTokenRefresh(t *testing.T) {
	h := driveHarness(t)
	hc, _ := fakeGoogle(t)
	h.App.DriveHTTP = hc
	bot := h.CreateBot("Keeper")

	draft, err := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "gdrive", Draft: true, Options: map[string]string{"access": "drive"}})
	if err != nil {
		t.Fatal(err)
	}
	// No admin setup yet: sign-in explains who has to act.
	if _, err := h.Client.BeginDriveAuth(h.Ctx(), connect.NewRequest(&v1.BeginDriveAuthRequest{Id: draft.Id})); code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("auth before setup: %v", err)
	}
	if _, err := h.Client.PutDriveSettings(h.Ctx(), connect.NewRequest(&v1.PutDriveSettingsRequest{
		Template: "gdrive", Values: map[string]string{"client_id": "cid", "client_secret": "shh"},
	})); err != nil {
		t.Fatal(err)
	}

	begin := func() url.Values {
		res, err := h.Client.BeginDriveAuth(h.Ctx(), connect.NewRequest(&v1.BeginDriveAuthRequest{Id: draft.Id}))
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(res.Msg.Url)
		return u.Query()
	}
	callback := func(q string) string {
		resp, err := http.Get(h.URL + "/oauth/callback?" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	// The provider says no: the popup says why, nothing is stored.
	q := begin()
	if q.Get("client_id") != "cid" || q.Get("access_type") != "offline" {
		t.Fatalf("authorize query = %v", q)
	}
	page := callback("state=" + q.Get("state") + "&error=access_denied")
	if !strings.Contains(page, "access_denied") || !strings.Contains(page, `ok:false`) {
		t.Fatalf("denied page = %s", page)
	}
	// A state is single-use.
	if page := callback("state=" + q.Get("state") + "&code=good-code"); !strings.Contains(page, "Unknown or expired") {
		t.Fatalf("reused state = %s", page)
	}

	q = begin()
	page = callback("state=" + q.Get("state") + "&code=good-code")
	if !strings.Contains(page, "Connected") || !strings.Contains(page, `silo:"drive-auth"`) {
		t.Fatalf("callback page = %s", page)
	}
	got := findDrive(listDrives(t, h, bot.Id, draft.Id), draft.Id)
	if !got.Connected || got.Account != "sam@example.com" {
		t.Fatalf("after sign-in: %+v", got)
	}

	opts, err := h.Client.PickDriveOptions(h.Ctx(), connect.NewRequest(&v1.PickDriveOptionsRequest{Id: draft.Id, Key: "shared_drive"}))
	if err != nil || len(opts.Msg.Options) != 1 || opts.Msg.Options[0].Value != "0AB" || opts.Msg.Options[0].Label != "Team Space" {
		t.Fatalf("pick = %v (%v)", opts, err)
	}

	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "gd", Options: map[string]string{"access": "drive", "shared_drive": "0AB"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDrive(t, h, bot.Id, d.Id, "mounted")
	starts := h.Drives.Runner.Starts()
	env := starts[len(starts)-1].Env
	if !slices.Contains(env, "RCLONE_CONFIG_GD_TEAM_DRIVE=0AB") || !slices.Contains(env, "RCLONE_CONFIG_GD_CLIENT_ID=cid") {
		t.Fatalf("env = %v", env)
	}
	if slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "RCLONE_CONFIG_GD_TOKEN=") }) {
		t.Fatal("token must live in the private config file, not env")
	}
	conf := filepath.Join(h.Drives.Dir, "run", d.Id+".conf")
	b, _ := os.ReadFile(conf)
	if !strings.Contains(string(b), `"access_token":"AT1"`) {
		t.Fatalf("conf = %s", b)
	}

	// rclone refreshes; the sidecar reports it and the CP keeps it.
	refreshed := `{"access_token":"AT2","refresh_token":"RT2","token_type":"Bearer","expiry":"2099-01-01T00:00:00Z"}`
	if err := os.WriteFile(conf, []byte("[gd]\ntoken = "+refreshed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var row db.Drive
		h.DB.First(&row, "id = ?", d.Id)
		var sec struct{ Dynamic map[string]string }
		_ = json.Unmarshal([]byte(row.SecretsJSON), &sec)
		if sec.Dynamic["token"] == refreshed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refreshed token never stored: %s", row.SecretsJSON)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Storing our own refresh must not restart the mount.
	before := len(h.Drives.Runner.Starts())
	time.Sleep(100 * time.Millisecond)
	if after := len(h.Drives.Runner.Starts()); after != before {
		t.Fatalf("mount restarted after a refresh echo: %d -> %d", before, after)
	}
}

func TestDriveNotYoursAndNoWorkerNeeded(t *testing.T) {
	h := driveHarness(t)
	bot := h.CreateBot("Keeper")
	d, err := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "webdav", Draft: true, Options: map[string]string{"url": "https://dav.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: d.Id, Name: "dav", Options: map[string]string{"url": "ftp://nope"}}); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("non-http url: %v", err)
	}
	// Another user's client cannot see or touch the drive.
	var u db.User
	h.DB.First(&u)
	other := h.NewClient()
	if _, err := other.ListDrives(h.Ctx(), connect.NewRequest(&v1.ListDrivesRequest{BotId: bot.Id})); err == nil {
		t.Fatal("anonymous ListDrives allowed")
	}
}

// drivePromptFor sends a message and returns the logged model request.
func drivePromptFor(t *testing.T, h *apptest.H, botID, text string) string {
	t.Helper()
	run, _ := h.Send(botID, h.FirstChat(botID), text)
	h.WaitRun(run)
	logs, err := h.Client.ListLLMLogs(h.Ctx(), connect.NewRequest(&v1.ListLLMLogsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs.Msg.GetLogs() {
		if l.GetLabel() == "chat" && strings.Contains(l.GetRequest(), text) {
			return l.GetRequest()
		}
	}
	t.Fatalf("no chat log for %q", text)
	return ""
}

func TestDrivesInSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)+"debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path), apptest.WithDriveGuest())
	bot := h.CreateBot("Keeper")
	if req := drivePromptFor(t, h, bot.Id, "Drives_Test_1"); strings.Contains(req, "## Drives") {
		t.Fatal("drives section without drives")
	}
	draft, _ := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "s3", Draft: true, Options: map[string]string{
		"access_key_id": "AKIA", "secret_access_key": "s3cret",
	}})
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "archive", ReadOnly: true, Options: map[string]string{"access_key_id": "AKIA"}}); err != nil {
		t.Fatal(err)
	}
	req := drivePromptFor(t, h, bot.Id, "Drives_Test_2")
	if !strings.Contains(req, "/workspace/drives/archive — Amazon S3 (read-only)") {
		t.Fatalf("drives missing from the prompt:\n%s", req)
	}
	if strings.Contains(req, "s3cret") || strings.Contains(req, "AKIA") {
		t.Fatal("credentials in the prompt")
	}
}
