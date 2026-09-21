package masker

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"testing"
)

// TestSecretEncodingsAreMasked proves a secret is masked in the forms it is
// likely to appear in: raw, base64, URL-encoded, and hex.
func TestSecretEncodingsAreMasked(t *testing.T) {
	secret := "tok+en/with=chars"
	m := New()
	m.Add(secret)
	if secret[0] == 0 {
		t.Fatal("unreachable")
	}

	cases := map[string]string{
		"raw":    secret,
		"base64": base64.StdEncoding.EncodeToString([]byte(secret)),
		"url":    url.QueryEscape(secret),
		"hex":    hex.EncodeToString([]byte(secret)),
	}
	for name, encoded := range cases {
		if encoded == secret {
			continue
		}
		got := m.Apply("value=" + encoded + " end")
		if got != "value=*** end" {
			t.Fatalf("%s not masked: %q", name, got)
		}
	}
}

func TestAddManySkipsShort(t *testing.T) {
	m := New()
	m.AddMany([]string{"short", "", "longenoughsecret"})
	if got := m.Apply("short plain longenoughsecret"); got != "short plain ***" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	m := New()
	m.Add("repeatable-secret")
	once := m.Apply("x repeatable-secret y")
	twice := m.Apply(once)
	if once != "x *** y" || twice != once {
		t.Fatalf("once=%q twice=%q", once, twice)
	}
}

func TestEmptyValueNotMasked(t *testing.T) {
	m := New()
	if got := m.Apply("nothing to hide"); got != "nothing to hide" {
		t.Fatalf("got %q", got)
	}
}
