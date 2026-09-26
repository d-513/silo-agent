package dummy

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"

	"silo.agent/internal/llm"
)

// Embed is a deterministic bag-of-words embedding: each lowercased word adds
// to one hashed dimension, then the vector is unit-normalized. Texts that
// share words are close, so recall ranking is testable without a model.
func (c *client) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = bagOfWords(t)
	}
	return out, nil
}

func bagOfWords(s string) []float32 {
	v := make([]float32, llm.EmbedDims)
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%llm.EmbedDims]++
	}
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	if n == 0 {
		v[0] = 1
		return v
	}
	inv := float32(1 / math.Sqrt(n))
	for i := range v {
		v[i] *= inv
	}
	return v
}
