package egress

import (
	"crypto/rand"
	"math/big"
)

// MinSurrogate is the shortest stand-in the gateway will accept. Below it a
// value survives nowhere in the payload and swapping it would be guesswork, so
// the gateway drops it; the CP keeps such a secret plain instead.
const MinSurrogate = 8

const (
	digits  = "0123456789"
	upper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letters = "abcdefghijklmnopqrstuvwxyz"
)

// NewSurrogate returns a stand-in for a secret that keeps its shape: the same
// length, the same leading prefix (everything up to and including the first
// '-' or '_' in the first eight bytes, so "ghp_", "sk-" and "xoxb-" stay), and
// at every later position the same class — a digit stays a digit, an uppercase
// letter an uppercase letter, a lowercase letter a lowercase letter, anything
// else unchanged. A target that validates a token's length or alphabet accepts
// the stand-in; the real value is swapped in by the gateway on the way out.
//
// It returns "" when real is shorter than MinSurrogate (there is nothing safe
// to stand in for), or on a random source failure.
func NewSurrogate(real string) string {
	if len(real) < MinSurrogate {
		return ""
	}
	prefix := surrogatePrefix(real)
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

// surrogatePrefix is the number of leading bytes kept verbatim: everything up
// to and including the first '-' or '_' within the first eight bytes, so a
// well-known token's marker survives.
func surrogatePrefix(real string) int {
	n := len(real)
	if n > 8 {
		n = 8
	}
	for i := 0; i < n; i++ {
		if real[i] == '-' || real[i] == '_' {
			return i + 1
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
