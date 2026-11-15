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

// A Bot's crest is one of eight Stack shapes in one of eight hues, packed as
// hue * CrestShapes + shape in the order of brand/tokens.json.
const (
	CrestShapes = 8
	CrestColors = 8
	CrestCount  = CrestShapes * CrestColors
)

func Crest(name, id string) int {
	h := sha256.Sum256([]byte(name + "|" + id))
	return int(h[0]) % CrestCount
}
