package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpbridge"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

func setConnectorImage(row *db.Connector, m *v1.UpdateConnectorRequest) error {
	if m.GetClearImage() {
		row.Image, row.ImageType = nil, ""
	} else if len(m.GetImage()) > 0 {
		img, imgType, err := clipImage(m.GetImage(), m.GetImageType())
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.Image, row.ImageType = img, imgType
	}
	return nil
}

func parseConnector(in connectorIn) (db.Connector, error) {
	if strings.TrimSpace(in.Name) == "" {
		return db.Connector{}, errors.New("name required")
	}
	tr := strings.ToLower(in.Transport)
	if tr == "" {
		tr = transportHTTP
	}
	if err := checkTransport(tr); err != nil {
		return db.Connector{}, err
	}
	img, imgType, err := clipImage(in.Image, in.ImageType)
	if err != nil {
		return db.Connector{}, err
	}
	row := db.Connector{
		ID: ids.New(), Type: connTypeMCP, Name: strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Desc), Category: strings.TrimSpace(in.Category), Image: img, ImageType: imgType,
		Transport: tr, DefaultMode: security.Rule(in.Mode), Prompt: strings.TrimSpace(in.Prompt),
		CreatedAt: time.Now(),
	}
	if tr == transportSTDIO {
		row.Auth = authNone
		if in.RequireComplete || strings.TrimSpace(in.StdioCommand) != "" {
			if err := mcpbridge.ValidCommand(in.StdioCommand); err != nil {
				return db.Connector{}, err
			}
			row.StdioCommand = strings.TrimSpace(in.StdioCommand)
		}
		for _, a := range in.StdioArgs {
			if err := mcpbridge.ValidArg(a); err != nil {
				return db.Connector{}, err
			}
		}
		args := in.StdioArgs
		if args == nil {
			args = []string{}
		}
		row.StdioArgsJSON = mustJSON(args)
		if in.AllowImage {
			if err := mcpbridge.ValidImage(in.StdioImage); err != nil {
				return db.Connector{}, err
			}
			row.StdioImage = strings.TrimSpace(in.StdioImage)
		}
		env, err := mergeEnv("", in.Env, in.RequireComplete)
		if err != nil {
			return db.Connector{}, err
		}
		row.EnvJSON = env
		return row, nil
	}
	au := strings.ToLower(in.Auth)
	if au == "" {
		au = authNone
	}
	if au != authNone && au != authOAuth {
		return db.Connector{}, errors.New("auth must be none or oauth")
	}
	if in.RequireComplete && strings.TrimSpace(in.HTTPURL) == "" {
		return db.Connector{}, errors.New("http_url required")
	}
	hdr, err := mergeHeaders("", in.Headers, in.RequireComplete)
	if err != nil {
		return db.Connector{}, err
	}
	row.HTTPURL = strings.TrimSpace(in.HTTPURL)
	row.Auth = au
	row.HeadersJSON = hdr
	return row, nil
}

type connectorIn struct {
	Name, Desc, Category, Transport, HTTPURL, Auth, Mode string
	Image                                                []byte
	ImageType                                            string
	Headers                                              []*v1.HeaderInput
	StdioCommand                                         string
	StdioArgs                                            []string
	StdioImage                                           string
	Env                                                  []*v1.EnvInput
	Prompt                                               string
	AllowImage                                           bool
	RequireComplete                                      bool
}

func checkTransport(tr string) error {
	switch tr {
	case transportHTTP, transportSTDIO:
		return nil
	default:
		return errors.New("transport must be http or stdio")
	}
}

func cloneLibrary(lib *db.Connector, botID string) db.Connector {
	return db.Connector{
		ID: ids.New(), Kind: catalog.KindCustom, BotID: botID, SourceID: lib.ID,
		Type: lib.Type, Name: lib.Name, Description: lib.Description, Category: lib.Category,
		Image: append([]byte(nil), lib.Image...), ImageType: lib.ImageType,
		Transport: lib.Transport, HTTPURL: lib.HTTPURL, Auth: lib.Auth,
		OAuthClientID: lib.OAuthClientID, OAuthClientSecret: lib.OAuthClientSecret,
		HeadersJSON: lib.HeadersJSON, StdioCommand: lib.StdioCommand,
		StdioArgsJSON: lib.StdioArgsJSON, StdioImage: lib.StdioImage, EnvJSON: lib.EnvJSON,
		DefaultMode: lib.DefaultMode, Prompt: lib.Prompt, Builtin: lib.Builtin,
		ConfigJSON: lib.ConfigJSON, SecretsJSON: lib.SecretsJSON, CreatedAt: time.Now(),
	}
}

func protoBotConnector(row *db.BotConnector, c *db.Connector, keys bool) *v1.BotConnector {
	return &v1.BotConnector{
		Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
		StatusDetail: row.StatusDetail,
		Connector:    protoConnector(c, keys),
	}
}

func protoConnector(c *db.Connector, admin bool) *v1.Connector {
	kind := c.Kind
	if kind == "" {
		kind = catalog.KindLibrary
	}
	out := &v1.Connector{
		Id: c.ID, Type: c.Type, Name: c.Name, Description: c.Description,
		Category: c.Category,
		HasImage: len(c.Image) > 0, Transport: c.Transport, HttpUrl: c.HTTPURL, Auth: c.Auth,
		DefaultMode: security.Rule(c.DefaultMode), CreatedAt: c.CreatedAt.Format(time.RFC3339),
		Kind: kind, SourceId: c.SourceID, CatalogGuide: catalog.Guide(c.SeedKey),
		StdioCommand: c.StdioCommand, StdioArgs: mcpbridge.ParseArgs(c.StdioArgsJSON), StdioImage: c.StdioImage,
		Prompt: c.Prompt, AutoAttach: c.AutoAttach, Slug: toolsgen.Slug(c.Name),
	}
	protoBuiltin(c, out)
	hdr, _ := mcpx.HeadersFromJSON(c.HeadersJSON)
	for k := range hdr {
		out.HeaderKeys = append(out.HeaderKeys, &v1.HeaderKey{Name: k})
	}
	for _, e := range parseEnv(c.EnvJSON) {
		out.EnvKeys = append(out.EnvKeys, &v1.EnvKey{Name: e.Name, Secret: e.Secret != "", SecretName: e.Secret})
	}
	if admin {
		out.OauthClientId = c.OAuthClientID
		out.HasOauthClientSecret = c.OAuthClientSecret != ""
	} else {
		out.HeaderKeys = nil
		out.EnvKeys = nil
	}
	return out
}

func clipImage(b []byte, typ string) ([]byte, string, error) {
	if len(b) == 0 {
		return nil, "", nil
	}
	if len(b) > imageMax {
		return nil, "", fmt.Errorf("image too large (%d bytes, max %d)", len(b), imageMax)
	}
	if typ == "" {
		typ = http.DetectContentType(b)
	}
	return b, typ, nil
}

func mergeHeaders(existingJSON string, ins []*v1.HeaderInput, requireValue bool) (string, error) {
	old, _ := mcpx.HeadersFromJSON(existingJSON)
	next := map[string]string{}
	for _, h := range ins {
		name := strings.TrimSpace(h.GetName())
		if name == "" {
			continue
		}
		val := h.GetValue()
		if val == "" {
			if requireValue {
				return "", fmt.Errorf("header %s needs a value", name)
			}
			if v, ok := old[name]; ok {
				next[name] = v
			}
			continue
		}
		next[name] = val
	}
	b, err := json.Marshal(next)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type envEntry struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Secret string `json:"secret,omitempty"`
}

func parseEnv(raw string) []envEntry {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []envEntry
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

func mergeEnv(existingJSON string, ins []*v1.EnvInput, requireValue bool) (string, error) {
	old := map[string]envEntry{}
	for _, e := range parseEnv(existingJSON) {
		old[e.Name] = e
	}
	var next []envEntry
	seen := map[string]bool{}
	for _, in := range ins {
		name := strings.TrimSpace(in.GetName())
		if name == "" {
			continue
		}
		if err := mcpbridge.ValidEnvName(name); err != nil {
			return "", err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		secret := strings.TrimSpace(in.GetSecret())
		val := in.GetValue()
		if val == "" && secret == "" {
			if requireValue {
				return "", fmt.Errorf("env %s needs a value or secret", name)
			}
			if prev, ok := old[name]; ok {
				next = append(next, prev)
			}
			continue
		}
		if secret != "" {
			next = append(next, envEntry{Name: name, Secret: secret})
			continue
		}
		next = append(next, envEntry{Name: name, Value: val})
	}
	if next == nil {
		next = []envEntry{}
	}
	b, err := json.Marshal(next)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
