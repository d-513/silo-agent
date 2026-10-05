package tunnel

import "testing"

func TestCheckPort(t *testing.T) {
	for _, p := range []int{1, 80, 8000, 65535} {
		if err := CheckPort(p); err != nil {
			t.Errorf("port %d: %v", p, err)
		}
	}
	for _, p := range []int{-1, 0, 65536, 5900, 9222} {
		if CheckPort(p) == nil {
			t.Errorf("port %d accepted", p)
		}
	}
}
