package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func New() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Token() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

const (
	CrestShapes = 8
	CrestColors = 12
	CrestCount  = CrestShapes * CrestColors
)

func PackCrest(shape, color int) int {
	s := shape % CrestShapes
	if s < 0 {
		s += CrestShapes
	}
	c := color % CrestColors
	if c < 0 {
		c += CrestColors
	}
	return c*CrestShapes + s
}

func ClampCrest(n int) int {
	if n < 0 || n >= CrestCount {
		return 0
	}
	return n
}

func Crest(name, id string) int {
	h := sha256.Sum256([]byte(name + "|" + id))
	return int(h[0]) % CrestCount
}
