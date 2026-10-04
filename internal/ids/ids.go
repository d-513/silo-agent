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

func Crest(name, id string) int {
	h := sha256.Sum256([]byte(name + "|" + id))
	return int(h[0]) % CrestCount
}
