package ids

import "testing"

func TestPackCrest(t *testing.T) {
	if PackCrest(0, 0) != 0 {
		t.Fatalf("got %d", PackCrest(0, 0))
	}
	if PackCrest(3, 2) != 2*CrestShapes+3 {
		t.Fatalf("got %d", PackCrest(3, 2))
	}
	if PackCrest(-1, 0) != CrestShapes-1 {
		t.Fatalf("neg shape: %d", PackCrest(-1, 0))
	}
}

func TestClampCrest(t *testing.T) {
	if ClampCrest(0) != 0 || ClampCrest(CrestCount-1) != CrestCount-1 {
		t.Fatal("bounds")
	}
	if ClampCrest(-1) != 0 || ClampCrest(CrestCount) != 0 {
		t.Fatal("out of range")
	}
}
