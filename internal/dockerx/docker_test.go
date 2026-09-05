package dockerx

import "testing"

func TestIsNotFoundEmpty(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Fatal("ErrNotFound")
	}
	if IsNotFound(nil) {
		t.Fatal("nil")
	}
}
