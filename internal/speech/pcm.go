package speech

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// WAVHeader is the 44-byte header of 16-bit mono PCM at rate. dataBytes < 0
// is a stream of unknown length: the sizes are the largest a header holds,
// which browsers read as "until the connection ends".
func WAVHeader(rate, dataBytes int) []byte {
	size := uint32(0xFFFFFFFF)
	riff := uint32(0xFFFFFFFF)
	if dataBytes >= 0 {
		size = uint32(dataBytes)
		riff = 36 + size
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], riff)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], 1) // mono
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], size)
	return h
}

// WAV wraps 16-bit mono PCM as a whole WAV file.
func WAV(pcm []byte, rate int) []byte {
	return append(WAVHeader(rate, len(pcm)), pcm...)
}

// SkipWAVHeader reads past a WAV's chunks to its samples.
func SkipWAVHeader(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if head, err := br.Peek(12); err != nil || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return br
	}
	_, _ = br.Discard(12)
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return br
		}
		n := binary.LittleEndian.Uint32(hdr[4:])
		if string(hdr[0:4]) == "data" {
			return br
		}
		if _, err := br.Discard(int(n + n%2)); err != nil {
			return br
		}
	}
}

// ReadWAV reads a WAV's format and samples: 16-bit PCM, mono or stereo
// (stereo is mixed down).
func ReadWAV(b []byte) (pcm []byte, rate int, err error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV file")
	}
	channels, bits := 0, 0
	for p := 12; p+8 <= len(b); {
		id, n := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:]))
		body := b[p+8:]
		if n > len(body) || n < 0 {
			n = len(body)
		}
		switch id {
		case "fmt ":
			if n < 16 {
				return nil, 0, errors.New("short fmt chunk")
			}
			if binary.LittleEndian.Uint16(body[0:]) != 1 {
				return nil, 0, errors.New("WAV is not PCM")
			}
			channels = int(binary.LittleEndian.Uint16(body[2:]))
			rate = int(binary.LittleEndian.Uint32(body[4:]))
			bits = int(binary.LittleEndian.Uint16(body[14:]))
		case "data":
			if bits != 16 || channels < 1 || channels > 2 {
				return nil, 0, fmt.Errorf("WAV is %d-bit with %d channels; want 16-bit mono", bits, channels)
			}
			data := body[:n-n%(2*channels)]
			if channels == 2 {
				data = MixDown(data)
			}
			return data, rate, nil
		}
		p += 8 + n + n%2
	}
	return nil, 0, errors.New("WAV has no samples")
}

// MixDown averages interleaved 16-bit stereo to mono.
func MixDown(stereo []byte) []byte {
	out := make([]byte, len(stereo)/2)
	for i := 0; i+3 < len(stereo); i += 4 {
		l := int32(int16(binary.LittleEndian.Uint16(stereo[i:])))
		r := int32(int16(binary.LittleEndian.Uint16(stereo[i+2:])))
		binary.LittleEndian.PutUint16(out[i/2:], uint16(int16((l+r)/2)))
	}
	return out
}

// DecodeMP3 decodes MP3 to 16-bit mono PCM at its own rate.
func DecodeMP3(r io.Reader) (pcm []byte, rate int, err error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, 0, err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, d); err != nil {
		return nil, 0, err
	}
	// go-mp3 always answers 16-bit stereo.
	return MixDown(buf.Bytes()), d.SampleRate(), nil
}

// Resample converts 16-bit mono PCM between rates by linear interpolation:
// plain, but enough for a voice going into a speech codec.
func Resample(pcm []byte, from, to int) []byte {
	if from == to || from <= 0 || to <= 0 || len(pcm) < 4 {
		return pcm
	}
	n := len(pcm) / 2
	out := make([]byte, 2*int(int64(n)*int64(to)/int64(from)))
	at := func(i int) float64 { return float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) }
	for j := range len(out) / 2 {
		x := float64(j) * float64(from) / float64(to)
		i := int(x)
		if i >= n-1 {
			binary.LittleEndian.PutUint16(out[2*j:], uint16(int16(at(n-1))))
			continue
		}
		f := x - float64(i)
		binary.LittleEndian.PutUint16(out[2*j:], uint16(int16(at(i)*(1-f)+at(i+1)*f)))
	}
	return out
}
