// Package drives turns declarative drive templates (one rclone remote plus the
// questions needed to fill it in) into rclone environment, OAuth requests, and
// provider lookups. It knows nothing about Postgres, koanf, or containers: the
// app hands it Values and gets back what to run.
package drives

// Var kinds. A user var is answered by the Bot owner on the Drives tab, a system
// var is set once per template by an admin, and a dynamic var is written by the
// system itself (OAuth callback, a lookup, a sidecar token refresh).
const (
	KindUser    = "user"
	KindSystem  = "system"
	KindDynamic = "dynamic"
)

// Auth kinds.
const (
	AuthOAuth2 = "oauth2"
	AuthForm   = "form"
)

// Field types for user and system vars.
const (
	TypeText   = "text"
	TypeSecret = "secret"
	TypeURL    = "url"
	TypeNumber = "number"
	TypeBool   = "bool"
	TypeSelect = "select"
	TypePick   = "pick"   // options come from a lookup once connected
	TypeFolder = "folder" // browsed live through the sidecar
	TypeArea   = "textarea"
	TypeHidden = "hidden" // set by a pick's extra values, never shown
)

// Dynamic var sources.
const (
	SourceOAuth    = "oauth"    // the rclone token JSON
	SourceCallback = "callback" // a query parameter on the OAuth redirect
	SourceLookup   = "lookup"   // a JSON value from the provider's API
)

// Categories group the gallery.
var Categories = []string{"consumer", "self-hosted", "object-storage", "protocol"}

type Template struct {
	Key        string            `yaml:"key" json:"key"`
	Title      string            `yaml:"title" json:"title"`
	Blurb      string            `yaml:"blurb" json:"blurb"`
	Category   string            `yaml:"category" json:"category"`
	RcloneType string            `yaml:"rclone_type" json:"rclone_type"`
	Auth       Auth              `yaml:"auth" json:"auth"`
	Vars       []Var             `yaml:"vars" json:"vars"`
	Rclone     map[string]string `yaml:"rclone" json:"-"`
	Mount      Mount             `yaml:"mount" json:"-"`

	// Filled by the loader from sibling files, not from drive.yaml.
	Guide string `yaml:"-" json:"guide,omitempty"` // GUIDE.md: owner-facing "Before you add"
	Setup string `yaml:"-" json:"setup,omitempty"` // SETUP.md: admin instructions for system vars
	Icon  []byte `yaml:"-" json:"-"`
}

type Auth struct {
	Kind     string            `yaml:"kind" json:"kind"`
	AuthURL  string            `yaml:"auth_url" json:"-"`
	TokenURL string            `yaml:"token_url" json:"-"`
	Scopes   []string          `yaml:"scopes" json:"-"`
	Params   map[string]string `yaml:"params" json:"-"`
	// Label is the sign-in button's provider name ("Google", "Microsoft").
	Label string `yaml:"label" json:"label,omitempty"`
}

type Mount struct {
	Path  string   `yaml:"path"`
	Flags []string `yaml:"flags"`
}

type Var struct {
	Key         string   `yaml:"key" json:"key"`
	Kind        string   `yaml:"kind" json:"kind"`
	Type        string   `yaml:"type" json:"type"`
	Label       string   `yaml:"label" json:"label"`
	Help        string   `yaml:"help" json:"help,omitempty"`
	Placeholder string   `yaml:"placeholder" json:"placeholder,omitempty"`
	Default     string   `yaml:"default" json:"default,omitempty"`
	Required    bool     `yaml:"required" json:"required"`
	Advanced    bool     `yaml:"advanced" json:"advanced,omitempty"`
	Secret      bool     `yaml:"secret" json:"secret,omitempty"`
	Obscure     bool     `yaml:"obscure" json:"-"`
	Options     []Option `yaml:"options" json:"options,omitempty"`
	// VisibleIf shows the var only when every listed user var has that value.
	VisibleIf map[string]string `yaml:"visible_if" json:"visible_if,omitempty"`
	// Source is where a dynamic var comes from; Param names the callback
	// query parameter (default Key).
	Source string  `yaml:"source" json:"source,omitempty"`
	Param  string  `yaml:"param" json:"-"`
	Lookup *Lookup `yaml:"lookup" json:"-"`
	// AutoPick fills a pick var with the first option when it is still empty.
	AutoPick bool `yaml:"auto_pick" json:"auto_pick,omitempty"`
}

// IsSecret is true for values that must live in hidden secret storage.
func (v Var) IsSecret() bool { return v.Secret || v.Type == TypeSecret }

type Option struct {
	Value  string            `yaml:"value" json:"value"`
	Label  string            `yaml:"label" json:"label"`
	Detail string            `yaml:"detail" json:"detail,omitempty"`
	Extra  map[string]string `yaml:"extra" json:"extra,omitempty"`
}

// Lookup is a declarative authenticated JSON call against the provider API.
// For a dynamic var, Value is the dot path of the single result. For a pick
// var, Items is the dot path of the array and Value/Label/Detail/Extra are
// paths inside each item.
type Lookup struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Body    string            `yaml:"body"`
	Headers map[string]string `yaml:"headers"`
	Items   string            `yaml:"items"`
	Value   string            `yaml:"value"`
	Label   string            `yaml:"label"`
	Detail  string            `yaml:"detail"`
	Extra   map[string]string `yaml:"extra"`
}

// Var returns the declared var of kind with key.
func (t *Template) Var(kind, key string) (Var, bool) {
	for _, v := range t.Vars {
		if v.Kind == kind && v.Key == key {
			return v, true
		}
	}
	return Var{}, false
}

// VarsOf returns the vars of one kind, in declared order.
func (t *Template) VarsOf(kind string) []Var {
	var out []Var
	for _, v := range t.Vars {
		if v.Kind == kind {
			out = append(out, v)
		}
	}
	return out
}

// NeedsSystem is true when the template has any required system var.
func (t *Template) NeedsSystem() bool {
	for _, v := range t.VarsOf(KindSystem) {
		if v.Required {
			return true
		}
	}
	return false
}

// MissingSystem lists required system vars that sys leaves empty.
func (t *Template) MissingSystem(sys map[string]string) []string {
	var out []string
	for _, v := range t.VarsOf(KindSystem) {
		if v.Required && sys[v.Key] == "" {
			out = append(out, v.Key)
		}
	}
	return out
}
