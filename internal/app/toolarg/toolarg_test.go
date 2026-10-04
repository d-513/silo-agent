package toolarg

import (
	"encoding/json"
	"testing"
)

func TestInt(t *testing.T) {
	args := map[string]any{"f": float64(7), "i": 8, "n": json.Number("9"), "s": "10", "bad": json.Number("x")}
	for k, want := range map[string]int{"f": 7, "i": 8, "n": 9, "s": 0, "bad": 0, "missing": 0} {
		if got := Int(args, k); got != want {
			t.Errorf("Int(%q) = %d, want %d", k, got, want)
		}
	}
}
