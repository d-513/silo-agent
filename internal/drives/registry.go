package drives

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// RcloneTypes are the rclone backends a template may use. It is an allowlist
// so a template typo cannot reach an rclone backend nobody reviewed.
var RcloneTypes = []string{
	"drive", "onedrive", "dropbox", "box", "pcloud", "webdav", "seafile",
	"s3", "b2", "sftp", "smb", "mega", "koofr", "yandex", "jottacloud",
	"protondrive", "azureblob", "ftp",
}

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Registry holds the loaded templates, sorted by category then title.
type Registry struct {
	list  []*Template
	byKey map[string]*Template
}

// Load reads every <key>/drive.yaml in fsys (plus optional icon.svg and
// GUIDE.md beside it) and validates the whole set. One bad template fails the
// load: a broken catalog is a build bug, not something to limp past.
func Load(fsys fs.FS) (*Registry, error) {
	files, err := fs.Glob(fsys, "*/drive.yaml")
	if err != nil {
		return nil, err
	}
	r := &Registry{byKey: map[string]*Template{}}
	var errs []error
	for _, f := range files {
		dir := path.Dir(f)
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		t := &Template{}
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(t); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		if t.Key != dir {
			errs = append(errs, fmt.Errorf("%s: key %q must match its directory", f, t.Key))
			continue
		}
		if b, err := fs.ReadFile(fsys, path.Join(dir, "GUIDE.md")); err == nil {
			t.Guide = strings.TrimSpace(string(b))
		}
		if b, err := fs.ReadFile(fsys, path.Join(dir, "SETUP.md")); err == nil {
			t.Setup = strings.TrimSpace(string(b))
		}
		if b, err := fs.ReadFile(fsys, path.Join(dir, "icon.svg")); err == nil {
			t.Icon = b
		}
		if err := Validate(t); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		if r.byKey[t.Key] != nil {
			errs = append(errs, fmt.Errorf("%s: duplicate key %q", f, t.Key))
			continue
		}
		r.byKey[t.Key] = t
		r.list = append(r.list, t)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	sort.SliceStable(r.list, func(i, j int) bool {
		ci, cj := slices.Index(Categories, r.list[i].Category), slices.Index(Categories, r.list[j].Category)
		if ci != cj {
			return ci < cj
		}
		return r.list[i].Title < r.list[j].Title
	})
	return r, nil
}

// MustLoad is Load for embedded catalogs, which tests keep valid.
func MustLoad(fsys fs.FS) *Registry {
	r, err := Load(fsys)
	if err != nil {
		panic("drives: " + err.Error())
	}
	return r
}

func (r *Registry) All() []*Template { return r.list }

func (r *Registry) Get(key string) (*Template, bool) {
	t, ok := r.byKey[key]
	return t, ok
}

var (
	varKinds  = []string{KindUser, KindSystem, KindDynamic}
	userTypes = []string{TypeText, TypeSecret, TypeURL, TypeNumber, TypeBool, TypeSelect, TypePick, TypeFolder, TypeArea, TypeHidden}
	sysTypes  = []string{TypeText, TypeSecret, TypeURL}
	sources   = []string{SourceOAuth, SourceCallback, SourceLookup}
)

// Validate checks one template for internal consistency.
func Validate(t *Template) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !keyRe.MatchString(t.Key) {
		bad("key %q must be snake_case", t.Key)
	}
	if strings.TrimSpace(t.Title) == "" {
		bad("title required")
	}
	if !slices.Contains(Categories, t.Category) {
		bad("category %q not in %v", t.Category, Categories)
	}
	if !slices.Contains(RcloneTypes, t.RcloneType) {
		bad("rclone_type %q not allowed", t.RcloneType)
	}
	if _, ok := t.Rclone["type"]; ok {
		bad("rclone.type is set by rclone_type")
	}

	seen := map[string]bool{}
	for _, v := range t.Vars {
		id := v.Kind + "." + v.Key
		if !keyRe.MatchString(v.Key) {
			bad("var %q: key must be snake_case", id)
		}
		if seen[id] {
			bad("var %q declared twice", id)
		}
		seen[id] = true
		switch v.Kind {
		case KindUser:
			if !slices.Contains(userTypes, v.Type) {
				bad("var %q: type %q not in %v", id, v.Type, userTypes)
			}
			if v.Source != "" {
				bad("var %q: only dynamic vars have a source", id)
			}
			if v.Type != TypeHidden && strings.TrimSpace(v.Label) == "" {
				bad("var %q: label required", id)
			}
		case KindSystem:
			if !slices.Contains(sysTypes, v.Type) {
				bad("var %q: type %q not in %v", id, v.Type, sysTypes)
			}
			if strings.TrimSpace(v.Label) == "" {
				bad("var %q: label required", id)
			}
		case KindDynamic:
			if !slices.Contains(sources, v.Source) {
				bad("var %q: source %q not in %v", id, v.Source, sources)
			}
			if v.Type != "" {
				bad("var %q: dynamic vars have no field type", id)
			}
			if v.Source == SourceCallback && t.Auth.Kind != AuthOAuth2 {
				bad("var %q: callback source needs oauth2", id)
			}
		default:
			bad("var %q: kind %q not in %v", id, v.Kind, varKinds)
		}
		if (v.Type == TypeSelect) != (len(v.Options) > 0) {
			bad("var %q: options go with type select, and select needs options", id)
		}
		needLookup := v.Type == TypePick || (v.Kind == KindDynamic && v.Source == SourceLookup)
		if needLookup != (v.Lookup != nil) {
			bad("var %q: lookup goes with type pick or source lookup", id)
		}
		if v.Lookup != nil {
			if t.Auth.Kind != AuthOAuth2 {
				bad("var %q: lookups need oauth2", id)
			}
			if !strings.HasPrefix(v.Lookup.URL, "https://") {
				bad("var %q: lookup url must be https", id)
			}
			if v.Lookup.Value == "" {
				bad("var %q: lookup value path required", id)
			}
			if v.Type == TypePick && v.Lookup.Items == "" {
				bad("var %q: pick lookup needs items", id)
			}
			for extra := range v.Lookup.Extra {
				if _, ok := t.Var(KindUser, extra); !ok {
					bad("var %q: lookup extra %q is not a user var", id, extra)
				}
			}
		}
		for k := range v.VisibleIf {
			if _, ok := t.Var(KindUser, k); !ok {
				bad("var %q: visible_if names unknown user var %q", id, k)
			}
		}
	}

	switch t.Auth.Kind {
	case AuthOAuth2:
		for _, u := range []string{t.Auth.AuthURL, t.Auth.TokenURL} {
			if !strings.HasPrefix(u, "https://") {
				bad("auth urls must be https, got %q", u)
			}
		}
		tok, ok := t.Var(KindDynamic, "token")
		if !ok || tok.Source != SourceOAuth {
			bad("oauth2 templates declare dynamic.token with source oauth")
		}
		if t.Rclone["token"] == "" {
			bad("oauth2 templates pass the token to rclone")
		}
		for _, k := range []string{"client_id", "client_secret"} {
			if _, ok := t.Var(KindSystem, k); !ok {
				bad("oauth2 templates declare system.%s", k)
			}
		}
	case AuthForm:
		if t.Auth.AuthURL != "" || t.Auth.TokenURL != "" || len(t.Auth.Scopes) > 0 {
			bad("form auth takes no oauth settings")
		}
	default:
		bad("auth.kind %q must be oauth2 or form", t.Auth.Kind)
	}

	// Every reference in every expression must point at a declared var, use
	// known filters, and callback vars may appear in the token URL only after
	// the callback delivered them (they cannot shape the authorize URL).
	exprs := map[string]string{"mount.path": t.Mount.Path, "auth.auth_url": t.Auth.AuthURL, "auth.token_url": t.Auth.TokenURL}
	for k, v := range t.Rclone {
		exprs["rclone."+k] = v
	}
	for i, s := range t.Auth.Scopes {
		exprs[fmt.Sprintf("auth.scopes[%d]", i)] = s
	}
	for k, v := range t.Auth.Params {
		exprs["auth.params."+k] = v
	}
	for i, f := range t.Mount.Flags {
		exprs[fmt.Sprintf("mount.flags[%d]", i)] = f
	}
	for _, v := range t.Vars {
		if v.Lookup != nil {
			exprs["var "+v.Key+" lookup.url"] = v.Lookup.URL
			exprs["var "+v.Key+" lookup.body"] = v.Lookup.Body
		}
	}
	for where, expr := range exprs {
		for _, ref := range Refs(expr) {
			d, ok := t.Var(ref.Kind, ref.Key)
			if !ok {
				bad("%s: unknown var %s.%s", where, ref.Kind, ref.Key)
				continue
			}
			for _, f := range ref.Filters {
				if filters[f] == nil {
					bad("%s: unknown filter %q", where, f)
				}
			}
			if where == "auth.auth_url" || strings.HasPrefix(where, "auth.scopes") || strings.HasPrefix(where, "auth.params") {
				if d.Kind == KindDynamic {
					bad("%s: dynamic values do not exist before sign-in", where)
				}
			}
		}
		if strings.Count(expr, "{{") != len(Refs(expr)) {
			bad("%s: malformed reference in %q", where, expr)
		}
	}
	if u := t.Auth.AuthURL; u != "" && len(Refs(u)) == 0 {
		if _, err := url.Parse(u); err != nil {
			bad("auth_url: %v", err)
		}
	}
	return errors.Join(errs...)
}
