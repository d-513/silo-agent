package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// The RFC 6238 appendix B vectors for SHA-1, cut to six digits.
func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		got, err := TOTPCode(secret, TOTPStepAt(time.Unix(unix, 0)))
		if err != nil || got != want {
			t.Errorf("at %d: %q %v, want %q", unix, got, err, want)
		}
	}
}

func TestCheckTOTPWindowAndReplay(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	step := TOTPStepAt(now)
	code := func(s int64) string {
		c, err := TOTPCode(secret, s)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if got, ok := CheckTOTP(secret, code(step), now, 0); !ok || got != step {
		t.Fatalf("current code: %d %v", got, ok)
	}
	// A phone a little behind or ahead still gets in.
	for _, s := range []int64{step - 1, step + 1} {
		if got, ok := CheckTOTP(secret, code(s), now, 0); !ok || got != s {
			t.Fatalf("step %d: %d %v", s-step, got, ok)
		}
	}
	if _, ok := CheckTOTP(secret, code(step-2), now, 0); ok {
		t.Fatal("a code two steps old was accepted")
	}
	// The same code, or an older one, is refused once a step was used.
	if _, ok := CheckTOTP(secret, code(step), now, step); ok {
		t.Fatal("a used code was accepted again")
	}
	if _, ok := CheckTOTP(secret, code(step-1), now, step); ok {
		t.Fatal("an older code was accepted after a newer one")
	}
	// Spaces in what was typed are fine; anything else is not a code.
	c := code(step)
	if _, ok := CheckTOTP(secret, c[:3]+" "+c[3:], now, 0); !ok {
		t.Fatal("a spaced code was refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := CheckTOTP(secret, bad, now, 0); ok {
			t.Fatalf("%q was accepted", bad)
		}
	}
	if _, ok := CheckTOTP("", "000000", now, 0); ok {
		t.Fatal("an empty secret accepted a code")
	}
}

func TestTOTPURLAndQR(t *testing.T) {
	u := TOTPURL("Silo Agent", "a+b@example.com", "ABCDEF")
	if !strings.HasPrefix(u, "otpauth://totp/Silo%20Agent:a+b@example.com?") || !strings.Contains(u, "secret=ABCDEF") || !strings.Contains(u, "issuer=Silo+Agent") {
		t.Fatalf("url %q", u)
	}
	img, err := QRDataURL(u)
	if err != nil || !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("qr %q %v", img[:min(len(img), 40)], err)
	}
}
