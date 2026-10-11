package connector

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
	"silo.agent/internal/ids"
)

// A connector's sign-in link can be sent to anybody. Whoever opens it and
// consents must not thereby hand their account to the Bot of the user who made
// the link: the callback finishes only in that user's own browser.
func TestOAuthCallbackOnlyForWhoStartedIt(t *testing.T) {
	gdb := dbtest.New(t)
	s := &Service{db: gdb, oauth: map[string]*oauthWait{}, cfg: func() config.Config { return config.Config{} }}
	signIn := func(email string) (string, *http.Cookie) {
		t.Helper()
		u := db.User{ID: ids.New(), Email: email, CreatedAt: time.Now()}
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		if _, err := auth.NewSession(gdb, u.ID, rec, auth.SessionMeta{Method: "password"}); err != nil {
			t.Fatal(err)
		}
		return u.ID, rec.Result().Cookies()[0]
	}
	mallory, malloryCookie := signIn("mallory@example.com")
	_, victimCookie := signIn("victim@example.com")

	ch := make(chan *mcpauth.AuthorizationResult, 1)
	s.oauth["st8"] = &oauthWait{ch: ch, issuer: "https://as.example", user: mallory}
	callback := func(c *http.Cookie) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=st8&code=the-code", nil)
		if c != nil {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		s.ServeOAuthCallback(rec, req)
		return rec.Code
	}
	for name, c := range map[string]*http.Cookie{"signed out": nil, "another user": victimCookie} {
		if got := callback(c); got != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", name, got)
		}
		select {
		case res := <-ch:
			t.Fatalf("%s: the code %q was handed to the waiting sign-in", name, res.Code)
		default:
		}
	}
	// The refusals did not spend the state: the starter can still finish.
	if got := callback(malloryCookie); got != http.StatusOK {
		t.Fatalf("the starter: status %d", got)
	}
	select {
	case res := <-ch:
		if res.Code != "the-code" || res.Iss != "https://as.example" {
			t.Fatalf("result %+v", res)
		}
	default:
		t.Fatal("the starter's callback did not deliver the code")
	}
	if got := callback(malloryCookie); got != http.StatusBadRequest {
		t.Fatalf("a spent state answered %d", got)
	}
}

func TestWebURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://as.example/authorize?x=1": true,
		"http://localhost:5556/auth":       true,
		"javascript:alert(1)":              false,
		"data:text/html,<script>":          false,
		"//as.example/authorize":           false,
		"":                                 false,
	} {
		if got := webURL(raw); got != want {
			t.Errorf("webURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
