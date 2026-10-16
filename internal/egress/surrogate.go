package egress

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// MinSurrogate is the shortest stand-in the gateway will accept. Below it a
// value survives nowhere in the payload and swapping it would be guesswork, so
// the gateway drops it; the CP keeps such a secret plain instead.
const MinSurrogate = 8

// minRandom is how many positions of a stand-in must be drawn at random. A
// value that is mostly punctuation, or mostly its kept prefix, would leave a
// stand-in that says too much about it, or is it.
const minRandom = 8

const (
	digits  = "0123456789"
	upper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letters = "abcdefghijklmnopqrstuvwxyz"
)

// NewSurrogate returns a stand-in for a secret that keeps its shape: the same
// length, the same vendor marker ("ghp_", "sk-proj-", "xoxb-": surrogatePrefix), and
// at every later position the same class — a digit stays a digit, an uppercase
// letter an uppercase letter, a lowercase letter a lowercase letter, anything
// else unchanged. A target that validates a token's length or alphabet accepts
// the stand-in; the real value is swapped in by the gateway on the way out.
//
// It returns "" when real is shorter than MinSurrogate, or has fewer than
// minRandom letters and digits past its marker (there is nothing safe to stand
// in for).
func NewSurrogate(real string) string {
	if len(real) < MinSurrogate {
		return ""
	}
	prefix := surrogatePrefix(real)
	random := 0
	for i := prefix; i < len(real); i++ {
		if c := real[i]; c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			random++
		}
	}
	if random < minRandom {
		return ""
	}
	out := []byte(real)
	for i := prefix; i < len(real); i++ {
		c := real[i]
		switch {
		case c >= '0' && c <= '9':
			out[i] = pick(digits)
		case c >= 'A' && c <= 'Z':
			out[i] = pick(upper)
		case c >= 'a' && c <= 'z':
			out[i] = pick(letters)
		default:
			out[i] = c
		}
	}
	if string(out) == real {
		// The random draw happened to reproduce the value; nudge one position
		// so a stand-in is never the secret itself.
		if i := lastAlphaNum(real); i >= prefix {
			out[i] = different(out[i])
		}
	}
	return string(out)
}

// vendorMarkers are the leading markers of well-known tokens, kept verbatim so
// a client library that checks a key's type still accepts the stand-in.
var vendorMarkers = []string{
	"sk-ant-api03-", "sk-ant-admin01-", "sk-or-v1-", "sk-proj-", "sk-svcacct-", "sk-admin-",
	"github_pat_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "glpat-",
	"xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-",
	"sk_live_", "sk_test_", "rk_live_", "rk_test_", "pk_live_", "pk_test_",
	"AKIA", "ASIA", "AIza", "npm_", "pypi-",
}

// surrogatePrefix is the number of leading bytes kept verbatim: a known vendor
// marker, else a short lower-case one ("sk-", "hf_": up to three letters and a
// '-' or '_'). Anything longer may be the secret itself, and a stand-in must
// not carry its first characters out of the gateway.
func surrogatePrefix(real string) int {
	best := 0
	for _, m := range vendorMarkers {
		if len(m) > best && strings.HasPrefix(real, m) && len(m) < len(real) {
			best = len(m)
		}
	}
	if best > 0 {
		return best
	}
	for i := 0; i < len(real) && i <= 3; i++ {
		c := real[i]
		if (c == '-' || c == '_') && i > 0 {
			return i + 1
		}
		if c < 'a' || c > 'z' {
			return 0
		}
	}
	return 0
}

func lastAlphaNum(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return i
		}
	}
	return -1
}

// different returns the next byte of c's own class, so it is never c.
func different(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return '0' + (c-'0'+1)%10
	case c >= 'A' && c <= 'Z':
		return 'A' + (c-'A'+1)%26
	default:
		return 'a' + (c-'a'+1)%26
	}
}

// pick returns one random byte from set. A failure of the random source (which
// crypto/rand does not have in practice) falls back to set[0].
func pick(set string) byte {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
	if err != nil {
		return set[0]
	}
	return set[n.Int64()]
}
