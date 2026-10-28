package voice

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"

	"silo.agent/internal/speech"
	"silo.agent/internal/speech/opus"
)

// frameDur is one Opus frame: the pacer's tick.
const frameDur = opus.FrameMs * time.Millisecond

// lookahead is how many pieces are synthesized ahead of the one playing: the
// next sentence is ready when this one ends, and an interruption wastes
// little.
const lookahead = 2

// piece is one chunk of speech: its text and, as they are made, its frames.
type piece struct {
	id     int
	runID  string
	text   string
	cancel context.CancelFunc

	mu      sync.Mutex
	frames  [][]byte
	done    bool
	started bool // synthesis started
	sent    int  // frames sent
}

func (p *piece) add(f []byte) {
	p.mu.Lock()
	p.frames = append(p.frames, f)
	p.mu.Unlock()
}

func (p *piece) finish() {
	p.mu.Lock()
	p.done = true
	p.mu.Unlock()
}

// total is how many frames the piece has or, while it is still being made,
// a guess from its length (about 60 ms a character).
func (p *piece) total() int {
	if p.done {
		return len(p.frames)
	}
	return max(len(p.frames), len([]rune(p.text))*3)
}

// outbox holds what the Bot is about to say and paces it onto the call: one
// frame every 20 ms, so a duck or a flush stops the voice within a frame.
type outbox struct {
	mu     sync.Mutex
	queue  []*piece
	nextID int
	ducked bool
	// delay is the browser's playout delay in frames: what was sent but not
	// heard yet when the human starts speaking.
	delay int
	wake  chan struct{}

	synth   func(ctx context.Context, p *piece)
	write   func(frame []byte) error
	onStart func(p *piece)
	onIdle  func()
}

func newOutbox() *outbox {
	return &outbox{wake: make(chan struct{}, 1), delay: 5}
}

func (o *outbox) poke() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// say queues text to be spoken.
func (o *outbox) say(text, runID string) {
	o.mu.Lock()
	o.nextID++
	o.queue = append(o.queue, &piece{id: o.nextID, runID: runID, text: text})
	o.mu.Unlock()
	o.poke()
}

// busy reports whether anything is queued or playing.
func (o *outbox) busy() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.queue) > 0
}

// duck pauses the voice the moment the human may be speaking, and steps back
// over what the browser had not played yet, so resuming loses nothing.
func (o *outbox) duck(on bool) {
	o.mu.Lock()
	if on && !o.ducked && len(o.queue) > 0 {
		p := o.queue[0]
		p.mu.Lock()
		p.sent = max(0, p.sent-o.delay-1)
		p.mu.Unlock()
	}
	o.ducked = on
	o.mu.Unlock()
	o.poke()
}

// flush drops everything queued and reports what the human heard of the
// piece that was playing (and which run it was from).
func (o *outbox) flush() (heard, runID string) {
	o.mu.Lock()
	queue := o.queue
	o.queue, o.ducked = nil, false
	delay := o.delay
	o.mu.Unlock()
	for i, p := range queue {
		if p.cancel != nil {
			p.cancel()
		}
		if i == 0 {
			p.mu.Lock()
			played := max(0, p.sent-delay)
			if p.sent > 0 {
				heard, _ = speech.Heard(p.text, played, p.total())
				runID = p.runID
			}
			p.mu.Unlock()
		}
	}
	o.poke()
	return heard, runID
}

// setDelay records the browser's playout delay.
func (o *outbox) setDelay(ms int) {
	o.mu.Lock()
	o.delay = min(max(ms/int(frameDur/time.Millisecond), 1), 25)
	o.mu.Unlock()
}

// synthLoop makes the speech of the pieces at the head of the queue, in
// order, never more than lookahead past the one playing.
func (o *outbox) synthLoop(ctx context.Context) {
	for {
		var next *piece
		o.mu.Lock()
		for i, p := range o.queue {
			if i > lookahead {
				break
			}
			if !p.started {
				p.started = true
				next = p
				break
			}
		}
		o.mu.Unlock()
		if next == nil {
			select {
			case <-ctx.Done():
				return
			case <-o.wake:
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		pctx, cancel := context.WithCancel(ctx)
		o.mu.Lock()
		next.cancel = cancel
		o.mu.Unlock()
		o.synth(pctx, next)
		cancel()
		next.finish()
		o.poke()
	}
}

// pace sends a frame every frameDur while there is one to send.
func (o *outbox) pace(ctx context.Context) {
	next := time.Now()
	for {
		frame, start, idle := o.take()
		if frame == nil {
			if idle && o.onIdle != nil {
				o.onIdle()
			}
			select {
			case <-ctx.Done():
				return
			case <-o.wake:
			case <-time.After(frameDur):
			}
			next = time.Now()
			continue
		}
		if start != nil && o.onStart != nil {
			o.onStart(start)
		}
		if err := o.write(frame); err != nil {
			return
		}
		next = next.Add(frameDur)
		d := time.Until(next)
		if d < -5*frameDur {
			next = time.Now()
			continue
		}
		if d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
	}
}

// take is the next frame to send, the piece it starts if it is that piece's
// first, and whether the queue just ran dry.
func (o *outbox) take() (frame []byte, start *piece, idle bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.queue) > 0 && !o.ducked {
		p := o.queue[0]
		p.mu.Lock()
		switch {
		case p.sent < len(p.frames):
			frame = p.frames[p.sent]
			if p.sent == 0 {
				start = p
			}
			p.sent++
			p.mu.Unlock()
			return frame, start, false
		case p.done:
			// Played out (or failed to be made): the next one.
			p.mu.Unlock()
			o.queue = o.queue[1:]
			if len(o.queue) == 0 {
				return nil, nil, true
			}
		default:
			// Still being made: wait for it.
			p.mu.Unlock()
			return nil, nil, false
		}
	}
	return nil, nil, false
}

// encodeSpeech reads a provider's answer as it streams and turns it into
// Opus frames on the piece.
func encodeSpeech(body io.Reader, format string, rate int, p *piece) error {
	var pcm io.Reader = body
	switch format {
	case "mp3":
		d, err := mp3.NewDecoder(body)
		if err != nil {
			return err
		}
		pcm, rate = &monoReader{r: d}, d.SampleRate()
	case "wav":
		br := bufio.NewReader(body)
		head, _ := br.Peek(44)
		if len(head) >= 28 && string(head[0:4]) == "RIFF" {
			rate = int(binary.LittleEndian.Uint32(head[24:]))
		}
		pcm = speech.SkipWAVHeader(br)
	}
	encRate := rate
	switch rate {
	case 8000, 12000, 16000, 24000, 48000:
	default:
		encRate = 24000
	}
	enc, err := opus.NewEncoder(encRate)
	if err != nil {
		return err
	}
	defer enc.Close()
	frame := enc.FrameSamples()
	need := frame * 2 * rate / encRate
	buf := make([]byte, need)
	samples := make([]int16, frame)
	for {
		n, err := io.ReadFull(pcm, buf)
		if n == 0 {
			if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		clear(buf[n:])
		block := buf
		if rate != encRate {
			block = speech.Resample(buf, rate, encRate)
		}
		for i := range samples {
			if 2*i+1 < len(block) {
				samples[i] = int16(binary.LittleEndian.Uint16(block[2*i:]))
			} else {
				samples[i] = 0
			}
		}
		f, eerr := enc.Encode(samples)
		if eerr != nil {
			return eerr
		}
		p.add(f)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
	}
}

// monoReader mixes go-mp3's 16-bit stereo down to mono as it reads.
type monoReader struct {
	r   io.Reader
	buf []byte
}

func (m *monoReader) Read(p []byte) (int, error) {
	want := len(p) &^ 1
	if want == 0 {
		return 0, nil
	}
	if cap(m.buf) < 2*want {
		m.buf = make([]byte, 2*want)
	}
	n, err := io.ReadFull(m.r, m.buf[:2*want])
	n &^= 3
	copy(p, speech.MixDown(m.buf[:n]))
	if n == 0 && err == nil {
		err = io.EOF
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = nil
	}
	return n / 2, err
}
