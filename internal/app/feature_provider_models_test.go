package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// The admin's "List models" asks the provider with its saved settings and
// answers full provider/model ids, ready for the allowlist.
func TestListProviderModels(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer sk-saved" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			http.Error(w, `{"error":"bad key sk-saved"}`, status)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"whisper-1","created":1},{"id":"gpt-5.6-luna","created":2,"name":"GPT-5.6 Luna","context_length":400000}]}`))
	}))
	defer srv.Close()

	yaml := strings.Replace(apptest.DefaultYAML(t.TempDir()), "providers:\n",
		"providers:\n  openai:\n    api_key: sk-saved\n    base_url: "+srv.URL+"\n", 1)
	h := apptest.New(t, apptest.WithYAML(yaml))
	list := func(provider string) (*connect.Response[v1.ListProviderModelsResponse], error) {
		return h.Client.ListProviderModels(h.Ctx(), connect.NewRequest(&v1.ListProviderModelsRequest{Provider: provider}))
	}

	res, err := list("openai")
	if err != nil {
		t.Fatalf("ListProviderModels: %v", err)
	}
	got := res.Msg.GetModels()
	if len(got) != 2 || got[0].GetId() != "openai/gpt-5.6-luna" || got[1].GetId() != "openai/whisper-1" {
		t.Fatalf("models %+v", got)
	}
	if got[0].GetName() != "GPT-5.6 Luna" || got[0].GetContextWindow() != 400000 {
		t.Fatalf("first model %+v", got[0])
	}

	// A provider with no key says which setting is missing.
	if _, err := list("anthropic"); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "API key missing") {
		t.Fatalf("keyless provider: %v", err)
	}
	// A provider that has no listing says so.
	if _, err := list("dummy"); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "cannot list") {
		t.Fatalf("dummy provider: %v", err)
	}
	if _, err := list("nope"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown provider: %v", err)
	}

	// The provider refusing is not the control plane being down, and its
	// words never carry the key.
	status = http.StatusUnauthorized
	_, err = list("openai")
	if err == nil || connect.CodeOf(err) == connect.CodeUnavailable || !strings.Contains(err.Error(), "401") {
		t.Fatalf("upstream refusal: %v", err)
	}
	if strings.Contains(err.Error(), "sk-saved") {
		t.Fatalf("error leaks the key: %v", err)
	}
	status = http.StatusOK

	// Admins only.
	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	h.DB.Create(&db.User{ID: ids.New(), Email: "user@test.local", PasswordHash: hash})
	user := h.NewClient()
	if _, err := user.SignIn(h.Ctx(), connect.NewRequest(&v1.SignInRequest{Email: "user@test.local", Password: "pw"})); err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if _, err := user.ListProviderModels(h.Ctx(), connect.NewRequest(&v1.ListProviderModelsRequest{Provider: "openai"})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("non-admin: %v", err)
	}
}
