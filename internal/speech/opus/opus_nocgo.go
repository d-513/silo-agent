//go:build !cgo

package opus

import "errors"

// FrameMs is the frame every encoder makes: 20 ms, what WebRTC expects.
const FrameMs = 20

// errNoCgo is what a control plane built without cgo answers: it cannot open
// libopus, so calls are unavailable (Read aloud still works).
var errNoCgo = errors.New("this control plane was built without cgo, so it cannot use libopus: calls need a build with CGO_ENABLED=1")

// Load always fails without cgo.
func Load(string) error { return errNoCgo }

// Encoder is unavailable without cgo.
type Encoder struct{}

// NewEncoder always fails without cgo.
func NewEncoder(int) (*Encoder, error) { return nil, errNoCgo }

func (e *Encoder) FrameSamples() int              { return 0 }
func (e *Encoder) Encode([]int16) ([]byte, error) { return nil, errNoCgo }
func (e *Encoder) Close()                         {}
