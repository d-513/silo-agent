package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

// Time-based one-time passwords as authenticator apps make them (RFC 6238):
// SHA-1, six digits, a new code every 30 seconds.
const (
	totpPeriod = 30
	totpDigits = 6
	// totpSkew is how many steps either side of now a code may come from, for
	// a phone whose clock is a little off.
	totpSkew = 1
)

var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret is a fresh 160-bit secret in the base32 the apps take.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return totpBase32.EncodeToString(b), nil
}

// TOTPStepAt is the time step t falls in.
func TOTPStepAt(t time.Time) int64 { return t.Unix() / totpPeriod }

// TOTPCode is the code of one time step.
func TOTPCode(secret string, step int64) (string, error) {
	key, err := totpBase32.DecodeString(strings.ToUpper(strings.TrimRight(strings.TrimSpace(secret), "=")))
	if err != nil || len(key) == 0 {
		return "", fmt.Errorf("bad two-factor secret")
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000), nil
}

// CheckTOTP reports whether code is right for secret around now, and the step
// it belongs to. A step at or before last was already used and is refused, so
// a code that was watched being typed cannot be played back.
func CheckTOTP(secret, code string, now time.Time, last int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := TOTPStepAt(now)
	for step := cur - totpSkew; step <= cur+totpSkew; step++ {
		if step <= last {
			continue
		}
		want, err := TOTPCode(secret, step)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// TOTPURL is the otpauth link an authenticator app enrols from.
func TOTPURL(issuer, account, secret string) string {
	q := url.Values{"secret": {secret}, "issuer": {issuer}}
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// QRDataURL draws text as a QR code, as a PNG data URL an <img> can show.
func QRDataURL(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
