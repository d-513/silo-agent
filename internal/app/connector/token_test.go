package connector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

// TestSharedTokenRefreshesOnce: two Bots' sessions on one connection meet an
// expired token at once. One refresh happens, both use the new token, and a
// provider that rotates refresh tokens (refusing the old one) strands
// neither.
func TestSharedTokenRefreshesOnce(t *testing.T) {
	var refreshes atomic.Int32
	var mu sync.Mutex
	live := "refresh-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		if r.Form.Get("refresh_token") != live {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		n := refreshes.Add(1)
		live = "refresh-rotated"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-new", "token_type": "Bearer", "expires_in": 3600, "refresh_token": live, "n": n,
		})
	}))
	defer srv.Close()

	gdb := dbtest.New(t)
	s := &Service{db: gdb}
	expired := storedToken{
		Token:    &oauth2.Token{AccessToken: "access-old", RefreshToken: "refresh-1", Expiry: time.Now().Add(-time.Hour)},
		TokenURL: srv.URL, ClientID: "silo", AuthStyle: oauth2.AuthStyleInParams,
	}
	raw, _ := json.Marshal(expired)
	gdb.Create(&db.Connector{ID: "conn", Kind: "custom", SpaceID: "s1", Name: "GitHub", TokenJSON: string(raw)})

	var wg sync.WaitGroup
	got := make([]string, 2)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each session has its own TokenSource on the same connection.
			tok, err := connToken{s: s, id: "conn", client: srv.Client()}.Token()
			if err != nil {
				t.Errorf("session %d: %v", i, err)
				return
			}
			got[i] = tok.AccessToken
		}(i)
	}
	wg.Wait()
	if refreshes.Load() != 1 || got[0] != "access-new" || got[1] != "access-new" {
		t.Fatalf("%d refreshes, tokens %v", refreshes.Load(), got)
	}
	var row db.Connector
	gdb.First(&row, "id = ?", "conn")
	if st := readToken(row.TokenJSON); st.Token.AccessToken != "access-new" || st.Token.RefreshToken != "refresh-rotated" || st.TokenURL != srv.URL {
		t.Fatalf("stored: %+v", st)
	}
}

func TestNoTokenAsksForSignIn(t *testing.T) {
	gdb := dbtest.New(t)
	s := &Service{db: gdb}
	gdb.Create(&db.Connector{ID: "conn", Kind: "custom", SpaceID: "s1", Name: "GitHub"})
	if _, err := (connToken{s: s, id: "conn", client: http.DefaultClient}).Token(); err != errNoToken {
		t.Fatalf("a connection never signed in: %v", err)
	}
	s.keepSignIn("conn", &oauth2.Config{ClientID: "c", Endpoint: oauth2.Endpoint{TokenURL: "https://idp/token"}},
		&oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)})
	tok, err := (connToken{s: s, id: "conn", client: http.DefaultClient}).Token()
	if err != nil || tok.AccessToken != "a" {
		t.Fatalf("after a sign-in: %v %v", tok, err)
	}
	s.forgetToken("conn")
	if _, err := (connToken{s: s, id: "conn", client: http.DefaultClient}).Token(); err != errNoToken {
		t.Fatalf("after signing out: %v", err)
	}
}
