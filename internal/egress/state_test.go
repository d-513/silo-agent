package egress

import (
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

func testApply(t *testing.T) *v1.EgressApply {
	t.Helper()
	cert, key, err := GenerateCA("Silo test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &v1.EgressApply{
		Clients: []*v1.EgressClient{
			{Subnet: "10.213.1.0/24", Kind: "bot", BotId: "b1", Name: "Ada"},
			{Subnet: "10.213.2.5/24", Kind: "stdio", BotId: "b1", ConnectorId: "c1", Name: "GitHub"},
			{Subnet: "10.213.3.0/24", Kind: "bot", BotId: "b2"},
			{Subnet: "not a subnet", Kind: "bot", BotId: "b3"},
		},
		Bots: []*v1.EgressBot{
			{
				BotId: "b1",
				Lists: &v1.EgressHostLists{Block: []string{"ads.example"}, Trust: []string{"pastebin.com"}, Pass: []string{"bank.example"}},
				Surrogates: []*v1.EgressSurrogate{
					{Name: "GITHUB_TOKEN", Surrogate: "ghp_SURROGATE0000000000", Real: "ghp_REALVALUE11111111111", Hosts: []string{"api.github.com", "*.githubusercontent.com"}},
					{Name: "SHORT", Surrogate: "short", Real: "x"},
				},
				PlainSecrets:   []string{"hunter2hunter2", "tiny"},
				OtherProtocols: true,
			},
		},
		Operator: &v1.EgressHostLists{Block: []string{"evil.example"}, Trust: []string{"corp.example"}, Pass: []string{"pinned.example"}},
		CaCert:   cert,
		CaKey:    key,
		CpAddr:   "silo-cp:8080",
	}
}

func TestNewStateClients(t *testing.T) {
	s, err := NewState(testApply(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.clients) != 3 {
		t.Fatalf("clients = %d, want 3 (the bad subnet skipped)", len(s.clients))
	}
	c := s.Client(netip.MustParseAddr("10.213.2.1"))
	if c == nil || c.ConnectorID != "c1" || c.Net.String() != "10.213.2.0/24" {
		t.Errorf("client on 10.213.2.1 = %+v (the subnet must be masked)", c)
	}
	if c := s.Client(netip.MustParseAddr("::ffff:10.213.1.1")); c == nil || c.BotID != "b1" || c.Kind != "bot" {
		t.Errorf("a mapped address did not find its client: %+v", c)
	}
	if s.Client(netip.MustParseAddr("10.213.9.1")) != nil {
		t.Error("an unknown network found a client")
	}
	var nilState *State
	if nilState.Client(netip.MustParseAddr("10.213.1.1")) != nil {
		t.Error("a nil state found a client")
	}
	if s.budget != DefaultUploadBudget || s.queryMin != DefaultQueryMin {
		t.Error("limits did not default")
	}
}

func TestStateLists(t *testing.T) {
	s, err := NewState(testApply(t))
	if err != nil {
		t.Fatal(err)
	}
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	sidecar := s.Client(netip.MustParseAddr("10.213.2.1"))
	b2 := s.Client(netip.MustParseAddr("10.213.3.1"))

	if why, ok := s.blocked(b2, "x.evil.example"); !ok || !strings.HasPrefix(why, "operator list") {
		t.Errorf("operator block = %q, %v", why, ok)
	}
	if why, ok := s.blocked(b1, "ads.example"); !ok || !strings.HasPrefix(why, "Bot list") {
		t.Errorf("Bot block = %q, %v", why, ok)
	}
	if _, ok := s.blocked(sidecar, "ads.example"); !ok {
		t.Error("a Bot's sidecar must stand on the Bot's lists")
	}
	if _, ok := s.blocked(b2, "ads.example"); ok {
		t.Error("one Bot's list blocked another Bot")
	}

	if !s.trusted(b2, "corp.example") || !s.trusted(b1, "pastebin.com") || s.trusted(b2, "pastebin.com") {
		t.Error("trust lists are wrong")
	}

	if !s.passThrough(b1, "bank.example") || !s.passThrough(b2, "pinned.example") || s.passThrough(b2, "bank.example") {
		t.Error("pass-through lists are wrong")
	}
	if s.passThrough(b1, "example.com") {
		t.Error("an ordinary host is passed through while the CA is loaded")
	}

	if !s.OtherProtocols(b1) || s.OtherProtocols(b2) || s.OtherProtocols(nil) {
		t.Error("other protocols switch is wrong")
	}
}

func TestStateWithoutCAPassesEverything(t *testing.T) {
	a := testApply(t)
	a.CaKey = []byte("broken")
	s, err := NewState(a)
	if err == nil {
		t.Error("a broken CA was not reported")
	}
	if s == nil || !s.passThrough(nil, "example.com") {
		t.Error("with no CA every TLS connection must be passed through")
	}
	a.CaCert, a.CaKey = nil, nil
	s, err = NewState(a)
	if err != nil || !s.passThrough(nil, "example.com") {
		t.Errorf("no CA: %v", err)
	}
}

func TestSurrogates(t *testing.T) {
	s, _ := NewState(testApply(t))
	b := s.bot(s.Client(netip.MustParseAddr("10.213.1.1")))
	if len(b.surrogates) != 1 {
		t.Fatalf("surrogates = %d, want 1 (a short one dropped)", len(b.surrogates))
	}

	req := "Authorization: Bearer ghp_SURROGATE0000000000"
	out, leaked := b.swapOut(req, "api.github.com")
	if leaked != "" || out != "Authorization: Bearer ghp_REALVALUE11111111111" {
		t.Errorf("to an allowed host: %q, leak %q", out, leaked)
	}
	if out, leaked = b.swapOut(req, "raw.githubusercontent.com"); leaked != "" || !strings.Contains(out, "REALVALUE") {
		t.Error("a wildcard host was not allowed")
	}
	out, leaked = b.swapOut(req, "pastebin.com")
	if leaked != "GITHUB_TOKEN" || out != req {
		t.Errorf("to another host: %q, leak %q; the request must stay as it was", out, leaked)
	}
	if out, leaked = b.swapOut("nothing here", "pastebin.com"); leaked != "" || out != "nothing here" {
		t.Error("a request with no surrogate was touched")
	}

	if got := b.swapBack(`{"token":"ghp_REALVALUE11111111111"}`); got != `{"token":"ghp_SURROGATE0000000000"}` {
		t.Errorf("swapBack = %q", got)
	}
	if !b.surrogateHost("api.github.com") || b.surrogateHost("example.com") {
		t.Error("surrogateHost is wrong")
	}
}

func TestPlainSecrets(t *testing.T) {
	s, _ := NewState(testApply(t))
	b := s.bot(s.Client(netip.MustParseAddr("10.213.1.1")))
	for _, body := range []string{
		"password=hunter2hunter2",
		"x=" + base64.StdEncoding.EncodeToString([]byte("hunter2hunter2")),
		"x=68756e7465723268756e74657232",
	} {
		if !b.plainSecret(body) {
			t.Errorf("plainSecret(%q) = false", body)
		}
	}
	if b.plainSecret("tiny tiny tiny") {
		t.Error("a value under 8 characters must not be watched: it would match ordinary text")
	}
	if (&botState{}).plainSecret("hunter2hunter2") {
		t.Error("an empty bot matched")
	}
}
