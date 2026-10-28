package opus

import "testing"

func TestEncode(t *testing.T) {
	if err := Load(""); err != nil {
		t.Skipf("no libopus on this host: %v", err)
	}
	e, err := NewEncoder(24000)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	frame := make([]int16, e.FrameSamples())
	for i := range frame {
		frame[i] = int16((i % 48) * 400)
	}
	pkt, err := e.Encode(frame)
	if err != nil || len(pkt) == 0 {
		t.Fatalf("packet %d bytes, %v", len(pkt), err)
	}
	if _, err := e.Encode(frame[:10]); err == nil {
		t.Fatal("a short frame should be refused")
	}
}
