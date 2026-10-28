//go:build cgo

// Package opus encodes speech with libopus, opened at run time: the control
// plane starts on a host without it, and only a call (which needs it) says
// it is missing.
package opus

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <stdint.h>

typedef struct OpusEncoder OpusEncoder;
typedef OpusEncoder* (*create_fn)(int32_t, int, int, int*);
typedef int32_t (*encode_fn)(OpusEncoder*, const int16_t*, int, unsigned char*, int32_t);
typedef void (*destroy_fn)(OpusEncoder*);
typedef int (*ctl_fn)(OpusEncoder*, int, ...);

static create_fn  p_create;
static encode_fn  p_encode;
static destroy_fn p_destroy;
static ctl_fn     p_ctl;

static const char* silo_opus_open(const char* path) {
	void* h = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (!h) return dlerror();
	p_create  = (create_fn)dlsym(h, "opus_encoder_create");
	p_encode  = (encode_fn)dlsym(h, "opus_encode");
	p_destroy = (destroy_fn)dlsym(h, "opus_encoder_destroy");
	p_ctl     = (ctl_fn)dlsym(h, "opus_encoder_ctl");
	if (!p_create || !p_encode || !p_destroy || !p_ctl) return "libopus lacks the encoder functions";
	return 0;
}

static OpusEncoder* silo_opus_create(int rate, int app, int* err) { return p_create(rate, 1, app, err); }
static int silo_opus_encode(OpusEncoder* e, const int16_t* pcm, int n, unsigned char* out, int max) { return p_encode(e, pcm, n, out, max); }
static void silo_opus_destroy(OpusEncoder* e) { p_destroy(e); }
// OPUS_SET_BITRATE_REQUEST 4002, OPUS_SET_SIGNAL_REQUEST 4024 (voice 3001)
static int silo_opus_bitrate(OpusEncoder* e, int b) { return p_ctl(e, 4002, b); }
static int silo_opus_voice(OpusEncoder* e) { return p_ctl(e, 4024, 3001); }
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"
)

// FrameMs is the frame every encoder makes: 20 ms, what WebRTC expects.
const FrameMs = 20

// appVoIP is OPUS_APPLICATION_VOIP.
const appVoIP = 2048

var (
	loadOnce sync.Once
	loadErr  error
)

// candidates are where libopus usually is, after the configured path and
// the loader's own search.
var candidates = []string{"libopus.so.0", "libopus.so", "/usr/lib/x86_64-linux-gnu/libopus.so.0", "/usr/lib/aarch64-linux-gnu/libopus.so.0", "/usr/lib64/libopus.so.0", "/run/current-system/sw/lib/libopus.so.0", "/opt/homebrew/lib/libopus.0.dylib"}

// Load opens libopus once: at path when one is given, else where it usually
// is (on NixOS, a libopus in the store). Later calls answer the first result.
func Load(path string) error {
	loadOnce.Do(func() {
		tries := candidates
		if path != "" {
			tries = append([]string{path}, tries...)
		}
		if nix, _ := filepath.Glob("/nix/store/*-libopus-*/lib/libopus.so.0"); len(nix) > 0 {
			tries = append(tries, nix...)
		}
		var last string
		for _, p := range tries {
			if filepath.IsAbs(p) {
				if _, err := os.Stat(p); err != nil {
					continue
				}
			}
			cs := C.CString(p)
			msg := C.silo_opus_open(cs)
			C.free(unsafe.Pointer(cs))
			if msg == nil {
				return
			}
			last = C.GoString(msg)
		}
		loadErr = fmt.Errorf("libopus not found (set voice.opus_library): %s", last)
	})
	return loadErr
}

// Encoder turns 16-bit mono PCM into Opus frames of FrameMs.
type Encoder struct {
	enc  *C.OpusEncoder
	rate int
	buf  []byte
}

// NewEncoder makes an encoder for PCM at rate (8, 12, 16, 24 or 48 kHz).
func NewEncoder(rate int) (*Encoder, error) {
	if err := Load(""); err != nil {
		return nil, err
	}
	var cerr C.int
	e := C.silo_opus_create(C.int(rate), appVoIP, &cerr)
	if cerr != 0 || e == nil {
		return nil, fmt.Errorf("opus encoder at %d Hz: error %d", rate, int(cerr))
	}
	C.silo_opus_bitrate(e, 32000)
	C.silo_opus_voice(e)
	return &Encoder{enc: e, rate: rate, buf: make([]byte, 4000)}, nil
}

// FrameSamples is how many samples one frame takes.
func (e *Encoder) FrameSamples() int { return e.rate * FrameMs / 1000 }

// Encode makes one Opus frame of exactly FrameSamples samples.
func (e *Encoder) Encode(pcm []int16) ([]byte, error) {
	if e.enc == nil {
		return nil, errors.New("opus encoder is closed")
	}
	if len(pcm) != e.FrameSamples() {
		return nil, fmt.Errorf("opus frame is %d samples, want %d", len(pcm), e.FrameSamples())
	}
	n := C.silo_opus_encode(e.enc, (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)), (*C.uchar)(unsafe.Pointer(&e.buf[0])), C.int32_t(len(e.buf)))
	if n < 0 {
		return nil, fmt.Errorf("opus encode: error %d", int(n))
	}
	return append([]byte(nil), e.buf[:n]...), nil
}

// Close frees the encoder.
func (e *Encoder) Close() {
	if e.enc != nil {
		C.silo_opus_destroy(e.enc)
		e.enc = nil
	}
}
