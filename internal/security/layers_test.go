package security

import "testing"

// A locked operator row is a floor: the engine takes the stricter of the two
// modes, and an unknown value must never look looser than it is.
func TestStricterTakesTheStricter(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{Allow, Deny, Deny},
		{Deny, Allow, Deny},
		{Auto, Ask, Ask},
		{Ask, Auto, Ask},
		{Allow, Allow, Allow},
		// An empty or unknown mode ranks as ask: it never loosens the other
		// side. The layering itself never asks with an empty side.
		{"", Ask, ""},
		{"nonsense", Allow, "nonsense"},
		{"nonsense", Deny, Deny},
	}
	for _, c := range cases {
		if got := Stricter(c.a, c.b); got != c.want {
			t.Errorf("Stricter(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestStricterOutputTakesTheStricter(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{OutPass, OutWithhold, OutWithhold},
		{OutFlag, OutCheck, OutCheck},
		{OutCheck, OutFlag, OutCheck},
		// An empty or unknown mode ranks as check, so it beats pass. The
		// layering never asks with an empty side: it returns the other one.
		{"", OutPass, ""},
		{"nonsense", OutPass, "nonsense"},
	}
	for _, c := range cases {
		if got := StricterOutput(c.a, c.b); got != c.want {
			t.Errorf("StricterOutput(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

// Looser is what a locked operator row refuses: allow is looser than ask, and
// an unknown value ranks as ask (or check), never below what it is compared to.
func TestLooserOnlyForALowerRank(t *testing.T) {
	if !Looser(Allow, Ask) {
		t.Error("allow must be looser than ask")
	}
	if Looser(Deny, Ask) {
		t.Error("deny must not be looser than ask")
	}
	if Looser("nonsense", Ask) {
		t.Error("an unknown mode ranks as ask, so it is not looser than ask")
	}
	if !LooserOutput(OutPass, OutWithhold) {
		t.Error("pass must be looser than withhold")
	}
	if LooserOutput(OutWithhold, OutCheck) {
		t.Error("withhold must not be looser than check")
	}
	// Callers normalise first (Output("nonsense") is pass, Rule("nonsense") is
	// ask), so a bad value can never be read as looser than it is.
	if !LooserOutput(Output("nonsense"), OutFlag) {
		t.Error("an unknown output value normalises to pass, which is looser than flag")
	}
	if !Looser(Rule("nonsense"), Deny) {
		t.Error("an unknown call value normalises to ask, which is looser than deny")
	}
}
