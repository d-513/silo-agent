package drives

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/oauth2"
)

var update = flag.Bool("update", false, "rewrite testdata/golden")

func init() {
	obscure = func(s string) (string, error) { return "OBSCURED(" + s + ")", nil }
}

func TestBuiltinCatalog(t *testing.T) {
	r := Builtin()
	if n := len(r.All()); n != 15 {
		t.Fatalf("templates = %d, want 15", n)
	}
	for _, key := range []string{"gdrive", "onedrive", "dropbox", "box", "pcloud", "nextcloud", "owncloud", "seafile", "webdav", "s3", "r2", "s3compat", "b2", "sftp", "smb"} {
		tp, ok := r.Get(key)
		if !ok {
			t.Fatalf("missing template %q", key)
		}
		if len(tp.Icon) == 0 {
			t.Errorf("%s: no icon.svg", key)
		}
		if tp.Auth.Kind == AuthOAuth2 && !strings.Contains(tp.Setup, "{public_url}/oauth/callback") {
			t.Errorf("%s: SETUP.md must name the redirect URI", key)
		}
		if tp.Auth.Kind == AuthOAuth2 && tp.Auth.Label == "" {
			t.Errorf("%s: oauth2 needs a sign-in label", key)
		}
	}
	// Sorted by category order, then title.
	prev := -1
	for _, tp := range r.All() {
		c := indexOf(Categories, tp.Category)
		if c < prev {
			t.Fatalf("%s out of category order", tp.Key)
		}
		prev = c
	}
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// fixture answers every var of a template with a recognizable value.
func fixture(tp *Template) Values {
	v := Values{User: map[string]string{}, System: map[string]string{}, Dynamic: map[string]string{}}
	for _, d := range tp.Vars {
		switch d.Kind {
		case KindUser:
			switch {
			case d.Type == TypeSelect:
				v.User[d.Key] = d.Options[0].Value
			case d.Type == TypeURL:
				v.User[d.Key] = "https://host.example/" + d.Key + "/"
			case d.Default != "":
				// keep the default
			default:
				v.User[d.Key] = d.Key + "-v"
			}
		case KindSystem:
			v.System[d.Key] = "sys-" + d.Key
		case KindDynamic:
			if d.Source == SourceOAuth {
				v.Dynamic[d.Key] = `{"access_token":"at","refresh_token":"rt"}`
			} else {
				v.Dynamic[d.Key] = "dyn-" + d.Key
			}
		}
	}
	return v
}

// TestGoldenRender pins the exact rclone environment of every template.
// Regenerate with: go test ./internal/drives -run TestGoldenRender -update
func TestGoldenRender(t *testing.T) {
	for _, tp := range Builtin().All() {
		t.Run(tp.Key, func(t *testing.T) {
			r, err := tp.Render(fixture(tp), "my-"+tp.Key)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			got := strings.Join(r.EnvList(), "\n") + "\npath=" + r.Path + "\nflags=" + strings.Join(r.Flags, " ") + "\n"
			file := filepath.Join("testdata", "golden", tp.Key+".env")
			if *update {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("render changed:\n--- got\n%s--- want\n%s", got, want)
			}
		})
	}
}

func TestRenderSpecifics(t *testing.T) {
	nc, _ := Builtin().Get("nextcloud")
	r, err := nc.Render(Values{User: map[string]string{
		"server": "https://cloud.example.com/", "username": "sam smith", "password": "pw",
	}}, "cloud")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Env["RCLONE_CONFIG_CLOUD_URL"]; got != "https://cloud.example.com/remote.php/dav/files/sam%20smith" {
		t.Fatalf("nextcloud url = %q", got)
	}
	if got := r.Env["RCLONE_CONFIG_CLOUD_PASS"]; got != "OBSCURED(pw)" {
		t.Fatalf("pass must be obscured, got %q", got)
	}
	if _, ok := r.Env["RCLONE_CONFIG_CLOUD_TYPE"]; !ok {
		t.Fatal("type env missing")
	}
	if r.Path != "" {
		t.Fatalf("empty folder should mount the root, got %q", r.Path)
	}

	// Empty optional options are left out so rclone keeps its default.
	gd, _ := Builtin().Get("gdrive")
	v := fixture(gd)
	delete(v.User, "shared_drive")
	v.User["folder"] = "/Projects/2026/"
	r, err = gd.Render(v, "gd")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Env["RCLONE_CONFIG_GD_TEAM_DRIVE"]; ok {
		t.Fatal("empty team_drive must be omitted")
	}
	if r.Path != "Projects/2026" {
		t.Fatalf("path = %q", r.Path)
	}

	// visible_if: a key-auth SFTP drive needs no password, and a hidden
	// password never leaks into the env.
	sf, _ := Builtin().Get("sftp")
	r, err = sf.Render(Values{User: map[string]string{
		"host": "h", "username": "u", "method": "key", "private_key": "PEM", "password": "stale",
	}}, "box1")
	if err != nil {
		t.Fatalf("key auth: %v", err)
	}
	if _, ok := r.Env["RCLONE_CONFIG_BOX1_PASS"]; ok {
		t.Fatal("hidden password rendered")
	}
	if r.Env["RCLONE_CONFIG_BOX1_PORT"] != "22" {
		t.Fatalf("default port not applied: %v", r.Env)
	}
}

func TestMissingStates(t *testing.T) {
	gd, _ := Builtin().Get("gdrive")
	_, err := gd.Render(Values{}, "gd")
	m, ok := AsMissing(err)
	if !ok || m.State() != StateNeedsSetup {
		t.Fatalf("no system values: %v", err)
	}
	_, err = gd.Render(Values{System: map[string]string{"client_id": "a", "client_secret": "b"}}, "gd")
	if m, ok = AsMissing(err); !ok || m.State() != StateNeedsAuth {
		t.Fatalf("no token: %v", err)
	}
	od, _ := Builtin().Get("onedrive")
	v := fixture(od)
	delete(v.User, "drive_id")
	_, err = od.Render(v, "od")
	if m, ok = AsMissing(err); !ok || m.State() != StateNeedsInput || m.Missing[0].Key != "drive_id" {
		t.Fatalf("no drive id: %v", err)
	}
	if got := gd.MissingSystem(map[string]string{"client_id": "x"}); len(got) != 1 || got[0] != "client_secret" {
		t.Fatalf("MissingSystem = %v", got)
	}
	s3, _ := Builtin().Get("s3")
	if s3.NeedsSystem() || !gd.NeedsSystem() {
		t.Fatal("NeedsSystem wrong")
	}
}

func TestRemoteNames(t *testing.T) {
	for in, want := range map[string]string{
		"google-drive": "google_drive", "Work Files": "work_files", "2026": "d_2026", "": "d_",
	} {
		if got := RemoteName(in); got != want {
			t.Errorf("RemoteName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := EnvKey("google_drive", "team-drive"); got != "RCLONE_CONFIG_GOOGLE_DRIVE_TEAM_DRIVE" {
		t.Fatalf("EnvKey = %q", got)
	}
}

func TestObscureMatchesRclone(t *testing.T) {
	// Produced by `rclone obscure secret` (rclone v1.75.1).
	got, err := Reveal("L6_Plj9y2J10M4AG7vu0xhdgBQeThw")
	if err != nil || got != "secret" {
		t.Fatalf("Reveal = %q, %v", got, err)
	}
	o, err := Obscure("pässwörd")
	if err != nil {
		t.Fatal(err)
	}
	if back, _ := Reveal(o); back != "pässwörd" {
		t.Fatalf("round trip = %q", back)
	}
	if _, err := Reveal("short"); err == nil {
		t.Fatal("short input must fail")
	}
}

const goodYAML = `
key: demo
title: Demo
category: protocol
rclone_type: sftp
auth: {kind: form}
vars:
  - {key: host, kind: user, type: text, label: Host, required: true}
rclone:
  host: "{{user.host}}"
mount:
  path: ""
`

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct{ from, to string }{
		"unknown var":        {`"{{user.host}}"`, `"{{user.nope}}"`},
		"unknown filter":     {`"{{user.host}}"`, `"{{user.host|shout}}"`},
		"malformed ref":      {`"{{user.host}}"`, `"{{user.Host}}"`},
		"bad rclone type":    {"rclone_type: sftp", "rclone_type: localfs"},
		"bad category":       {"category: protocol", "category: misc"},
		"typo field":         {"required: true}", "requird: true}"},
		"key/dir mismatch":   {"key: demo", "key: other"},
		"select w/o options": {"type: text, label: Host", "type: select, label: Host"},
		"dynamic w/ type":    {"kind: user, type: text", "kind: dynamic, source: oauth, type: text"},
		"oauth w/o token":    {"auth: {kind: form}", "auth: {kind: oauth2, auth_url: https://a, token_url: https://b}"},
		"type in rclone map": {"  host: \"{{user.host}}\"", "  host: \"{{user.host}}\"\n  type: sftp"},
	}
	if _, err := Load(fstest.MapFS{"demo/drive.yaml": {Data: []byte(goodYAML)}}); err != nil {
		t.Fatalf("good template rejected: %v", err)
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			bad := strings.Replace(goodYAML, c.from, c.to, 1)
			if bad == goodYAML {
				t.Fatalf("case did not change the yaml")
			}
			if _, err := Load(fstest.MapFS{"demo/drive.yaml": {Data: []byte(bad)}}); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestValidateDynamicBeforeSignIn(t *testing.T) {
	gd, _ := Builtin().Get("gdrive")
	cp := *gd
	cp.Auth.Params = map[string]string{"hint": "{{dynamic.account}}"}
	if err := Validate(&cp); err == nil || !strings.Contains(err.Error(), "before sign-in") {
		t.Fatalf("dynamic in auth params accepted: %v", err)
	}
}

// rewrite sends every request to the test server, whatever host it names.
type rewrite struct {
	srv  *httptest.Server
	seen []string
}

func (rw *rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	rw.seen = append(rw.seen, r.Method+" "+r.URL.Host+r.URL.Path)
	u, _ := url.Parse(rw.srv.URL)
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host, r2.Host = u.Scheme, u.Host, u.Host
	return rw.srv.Client().Transport.RoundTrip(r2)
}

func testClient(h http.Handler) (*http.Client, *rewrite, func()) {
	srv := httptest.NewTLSServer(h)
	rw := &rewrite{srv: srv}
	return &http.Client{Transport: rw}, rw, srv.Close
}

func TestOAuthFlow(t *testing.T) {
	gd, _ := Builtin().Get("gdrive")
	v := Values{System: map[string]string{"client_id": "cid", "client_secret": "cs"}, User: map[string]string{"access": "drive.readonly"}}
	raw, err := gd.AuthCodeURL(v, "https://silo.example/oauth/callback", "st8")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("client_id") != "cid" || q.Get("state") != "st8" ||
		q.Get("scope") != "https://www.googleapis.com/auth/drive.readonly" || q.Get("access_type") != "offline" ||
		q.Get("redirect_uri") != "https://silo.example/oauth/callback" {
		t.Fatalf("auth url = %s", raw)
	}
	if _, err := gd.AuthCodeURL(Values{}, "r", "s"); err == nil {
		t.Fatal("auth url without client id")
	} else if m, ok := AsMissing(err); !ok || m.State() != StateNeedsSetup {
		t.Fatalf("want needs_setup, got %v", err)
	}

	var form url.Values
	hc, rw, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		if user, pass, ok := r.BasicAuth(); ok {
			form.Set("basic", user+":"+pass)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":3600}`)
	}))
	defer done()
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)
	tok, err := gd.Exchange(ctx, v, "https://silo.example/oauth/callback", "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("code") != "the-code" {
		t.Fatalf("token form = %v", form)
	}
	if rw.seen[0] != "POST oauth2.googleapis.com/token" {
		t.Fatalf("token endpoint = %v", rw.seen)
	}
	pt, err := ParseToken(tok)
	if err != nil || pt.AccessToken != "AT" || pt.RefreshToken != "RT" || pt.Expiry.IsZero() {
		t.Fatalf("token = %s (%v)", tok, err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(tok), &m)
	if _, ok := m["expires_in"]; ok {
		t.Fatal("rclone token json must not carry expires_in")
	}
}

func TestPCloudTokenHostFromCallback(t *testing.T) {
	pc, _ := Builtin().Get("pcloud")
	dyn := pc.CallbackValues(url.Values{"code": {"c"}, "hostname": {"eapi.pcloud.com"}, "locationid": {"2"}})
	if dyn["hostname"] != "eapi.pcloud.com" || len(dyn) != 1 {
		t.Fatalf("callback values = %v", dyn)
	}
	hc, rw, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"AT","token_type":"bearer"}`)
	}))
	defer done()
	v := Values{System: map[string]string{"client_id": "a", "client_secret": "b"}, Dynamic: dyn}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)
	if _, err := pc.Exchange(ctx, v, "r", "c"); err != nil {
		t.Fatal(err)
	}
	if rw.seen[0] != "POST eapi.pcloud.com/oauth2_token" {
		t.Fatalf("token endpoint = %v", rw.seen)
	}
	// Without a callback value the default host applies.
	if got := pc.Expand(pc.Auth.TokenURL, Values{}); got != "https://api.pcloud.com/oauth2_token" {
		t.Fatalf("default token url = %q", got)
	}
}

func TestLookups(t *testing.T) {
	var auth string
	hc, rw, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/me":
			_, _ = io.WriteString(w, `{"userPrincipalName":"sam@contoso.com"}`)
		case "/v1.0/me/drives":
			_, _ = io.WriteString(w, `{"value":[{"id":"b!1","name":"OneDrive","driveType":"business"},{"id":"b!2","name":"Team","driveType":"documentLibrary"},{"name":"no id"}]}`)
		case "/2/users/get_current_account":
			if r.Method != http.MethodPost {
				w.WriteHeader(405)
				return
			}
			b, _ := io.ReadAll(r.Body)
			_, _ = fmt.Fprintf(w, `{"email":"sam@dropbox.test","body":%q}`, b)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer done()
	ctx := context.Background()
	tokenV := Values{Dynamic: map[string]string{"token": `{"access_token":"AT"}`}}

	od, _ := Builtin().Get("onedrive")
	got, err := od.Resolve(ctx, hc, tokenV)
	if err != nil || got["account"] != "sam@contoso.com" {
		t.Fatalf("resolve = %v, %v", got, err)
	}
	if auth != "Bearer AT" {
		t.Fatalf("auth header = %q", auth)
	}
	opts, err := od.Pick(ctx, hc, tokenV, "drive_id")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 2 || opts[1].Value != "b!2" || opts[1].Label != "Team" || opts[1].Detail != "documentLibrary" ||
		opts[1].Extra["drive_type"] != "documentLibrary" {
		t.Fatalf("pick = %+v", opts)
	}

	db, _ := Builtin().Get("dropbox")
	got, err = db.Resolve(ctx, hc, tokenV)
	if err != nil || got["account"] != "sam@dropbox.test" {
		t.Fatalf("dropbox resolve = %v, %v (%v)", got, err, rw.seen)
	}

	gd, _ := Builtin().Get("gdrive")
	if _, err := gd.Resolve(ctx, hc, tokenV); err == nil {
		t.Fatal("401 must fail")
	} else if m, ok := AsMissing(err); !ok || m.State() != StateNeedsAuth {
		t.Fatalf("401 must mean reconnect, got %v", err)
	}
	if _, err := gd.Resolve(ctx, hc, Values{}); err == nil {
		t.Fatal("lookup without a token must fail")
	}
}

func TestDig(t *testing.T) {
	var doc any
	_ = json.Unmarshal([]byte(`{"a":{"b":[{"c":"x"},{"c":2}]},"t":true,"n":null}`), &doc)
	for path, want := range map[string]string{"a.b.0.c": "x", "a.b.1.c": "2", "t": "true", "n": "", "a.b.5.c": "", "a.x": ""} {
		if got := DigString(doc, path); got != want {
			t.Errorf("DigString(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestRcloneOptions checks every template against rclone's own option list
// (testdata/rclone_options.json, from `rclone config providers`): option names
// must exist, and exactly the options rclone treats as passwords are obscured.
// Refresh the snapshot when bumping rclone in driveimage/.
func TestRcloneOptions(t *testing.T) {
	raw, err := os.ReadFile("testdata/rclone_options.json")
	if err != nil {
		t.Fatal(err)
	}
	var known map[string]map[string]struct{ Password bool }
	if err := json.Unmarshal(raw, &known); err != nil {
		t.Fatal(err)
	}
	for _, typ := range RcloneTypes {
		if known[typ] == nil {
			t.Errorf("allowlisted rclone type %q is not an rclone backend", typ)
		}
	}
	for _, tp := range Builtin().All() {
		opts := known[tp.RcloneType]
		for opt, expr := range tp.Rclone {
			o, ok := opts[opt]
			if !ok {
				t.Errorf("%s: rclone %s has no option %q", tp.Key, tp.RcloneType, opt)
				continue
			}
			obscured := false
			for _, ref := range Refs(expr) {
				if d, _ := tp.Var(ref.Kind, ref.Key); d.Obscure {
					obscured = true
				}
			}
			if o.Password != obscured {
				t.Errorf("%s: option %q password=%v but obscured=%v", tp.Key, opt, o.Password, obscured)
			}
		}
	}
}

func TestRefresh(t *testing.T) {
	gd, _ := Builtin().Get("gdrive")
	sys := map[string]string{"client_id": "a", "client_secret": "b"}
	fresh := `{"access_token":"x","refresh_token":"r","expiry":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`
	if got, err := gd.Refresh(context.Background(), Values{System: sys, Dynamic: map[string]string{"token": fresh}}); err != nil || got != "" {
		t.Fatalf("fresh token refreshed: %q %v", got, err)
	}
	stale := `{"access_token":"old","refresh_token":"r","expiry":"` + time.Now().Add(-time.Hour).Format(time.RFC3339) + `"}`
	status := http.StatusOK
	hc, _, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = io.WriteString(w, `{"access_token":"new","token_type":"Bearer","expires_in":3600}`)
		} else {
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
		}
	}))
	defer done()
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)
	got, err := gd.Refresh(ctx, Values{System: sys, Dynamic: map[string]string{"token": stale}})
	if err != nil {
		t.Fatal(err)
	}
	pt, _ := ParseToken(got)
	if pt.AccessToken != "new" || pt.RefreshToken != "r" {
		t.Fatalf("refreshed = %s (refresh token must carry over)", got)
	}
	status = http.StatusBadRequest
	if _, err := gd.Refresh(ctx, Values{System: sys, Dynamic: map[string]string{"token": stale}}); err == nil {
		t.Fatal("revoked refresh accepted")
	} else if m, ok := AsMissing(err); !ok || m.State() != StateNeedsAuth {
		t.Fatalf("revoked refresh must mean reconnect: %v", err)
	}
}
