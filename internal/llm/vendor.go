package llm

// Shared plumbing for the speech vendors' own APIs (Deepgram, ElevenLabs,
// Cartesia, Inworld, Soniox, AssemblyAI): a client with no text models, a
// voice that speaks each piece as one "context" of a persistent socket, and
// one-request transcription through a live session.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"silo.agent/internal/speech"
)

// speechOnly is the text side of a provider that has no text models.
type speechOnly struct{ name string }

func (s speechOnly) Stream(context.Context, Request) (Stream, error) {
	return nil, fmt.Errorf("%s has no text models; pick it for speech or transcription", s.name)
}

func (s speechOnly) Complete(context.Context, Request) (Response, error) {
	return Response{}, fmt.Errorf("%s has no text models; pick it for speech or transcription", s.name)
}

// wsMaxMessage is the largest frame a vendor socket may send (audio comes in
// base64 JSON).
const wsMaxMessage = 16 << 20

// dialWS opens a vendor socket with headers.
func dialWS(ctx context.Context, url string, header http.Header) (*websocket.Conn, error) {
	conn, res, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		if res != nil && res.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
			res.Body.Close()
			if msg := strings.TrimSpace(string(b)); msg != "" {
				return nil, fmt.Errorf("%w: %s", err, msg)
			}
		}
		return nil, err
	}
	conn.SetReadLimit(wsMaxMessage)
	return conn, nil
}

// writeJSON sends one JSON message on a vendor socket.
func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

// vendorError reads a refused HTTP request: the body's message, never the key.
func vendorError(name string, res *http.Response, key string) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	msg := strings.TrimSpace(string(b))
	var e struct {
		Error   any    `json:"error"`
		Message string `json:"message"`
		Detail  any    `json:"detail"`
		ErrMsg  string `json:"err_msg"`
	}
	if json.Unmarshal(b, &e) == nil {
		switch {
		case e.Message != "":
			msg = e.Message
		case e.ErrMsg != "":
			msg = e.ErrMsg
		case e.Error != nil:
			msg = fmt.Sprint(textOf(e.Error))
		case e.Detail != nil:
			msg = fmt.Sprint(textOf(e.Detail))
		}
	}
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "••••")
	}
	if msg == "" {
		return fmt.Errorf("%s: http %d", name, res.StatusCode)
	}
	return fmt.Errorf("%s: http %d: %s", name, res.StatusCode, msg)
}

// textOf finds the message in an error field that may be a string or an
// object with one.
func textOf(v any) any {
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"message", "msg", "detail", "status"} {
			if s, ok := m[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return v
}

// pcmMs is how long 16-bit mono PCM lasts.
func pcmMs(n, rate int) int {
	if rate <= 0 {
		return 0
	}
	return n / 2 * 1000 / rate
}

// voiceEvent is one message of a context voice, read by its vendor.
type voiceEvent struct {
	ctx   string
	pcm   []byte
	words []SpeechWord // times from the start of the piece
	chars []charTime   // times from the start of this message's audio
	done  bool
	err   error
}

// charTime is one character and when it is said.
type charTime struct {
	c       string
	startMs int
	endMs   int
}

// ctxVoice is a voice that speaks each piece as its own context on one
// socket: the vendor supplies how a piece starts, how one is cancelled, and
// how to read a message.
type ctxVoice struct {
	conn  *websocket.Conn
	rate  int
	start func(id, text string) []any
	stop  func(id string) any
	bye   any
	read  func(data []byte) voiceEvent

	say sync.Mutex // one piece at a time
	n   int

	mu     sync.Mutex
	cur    *voicePiece
	ended  chan struct{}
	endErr error
	once   sync.Once
}

type voicePiece struct {
	id      string
	audio   func([]byte)
	words   func([]SpeechWord)
	done    chan error
	ms      int // audio so far
	pending []charTime
}

func newCtxVoice(conn *websocket.Conn, rate int) *ctxVoice {
	return &ctxVoice{conn: conn, rate: rate, ended: make(chan struct{})}
}

func (v *ctxVoice) run() {
	for {
		typ, data, err := v.conn.Read(context.Background())
		if err != nil {
			v.fail(err)
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		ev := v.read(data)
		v.mu.Lock()
		p := v.cur
		if ev.err != nil && (ev.ctx == "" || p != nil && ev.ctx == p.id) {
			v.mu.Unlock()
			if ev.ctx == "" {
				v.fail(ev.err)
				return
			}
			p.done <- ev.err
			continue
		}
		if p == nil || ev.ctx != p.id {
			// A cancelled piece's late audio.
			v.mu.Unlock()
			continue
		}
		offset := p.ms
		p.ms += pcmMs(len(ev.pcm), v.rate)
		v.mu.Unlock()
		if len(ev.pcm) > 0 {
			p.audio(ev.pcm)
		}
		if len(ev.chars) > 0 {
			if ws := p.charWords(ev.chars, offset, false); len(ws) > 0 {
				p.words(ws)
			}
		}
		if len(ev.words) > 0 {
			p.words(ev.words)
		}
		if ev.done {
			if ws := p.charWords(nil, 0, true); len(ws) > 0 {
				p.words(ws)
			}
			select {
			case p.done <- nil:
			default:
			}
		}
	}
}

// charWords turns character timings into words as they complete; flush
// ends the last.
func (p *voicePiece) charWords(cs []charTime, offset int, flush bool) []SpeechWord {
	for _, c := range cs {
		p.pending = append(p.pending, charTime{c.c, c.startMs + offset, c.endMs + offset})
	}
	var out []SpeechWord
	var word []charTime
	take := func() {
		if len(word) > 0 {
			var b strings.Builder
			for _, c := range word {
				b.WriteString(c.c)
			}
			out = append(out, SpeechWord{Text: b.String(), StartMs: word[0].startMs, EndMs: word[len(word)-1].endMs})
			word = nil
		}
	}
	last := -1
	for i, c := range p.pending {
		if strings.TrimSpace(c.c) == "" {
			take()
			last = i
			continue
		}
		word = append(word, c)
	}
	if flush {
		take()
		p.pending = nil
		return out
	}
	// The word still being said waits for its end.
	p.pending = p.pending[last+1:]
	return out
}

func (v *ctxVoice) fail(err error) {
	v.once.Do(func() {
		v.mu.Lock()
		v.endErr = err
		p := v.cur
		v.mu.Unlock()
		close(v.ended)
		if p != nil {
			select {
			case p.done <- ErrSpeechClosed:
			default:
			}
		}
	})
}

func (v *ctxVoice) Rate() int { return v.rate }

func (v *ctxVoice) Say(ctx context.Context, text string, audio func([]byte), words func([]SpeechWord)) error {
	v.say.Lock()
	defer v.say.Unlock()
	select {
	case <-v.ended:
		return ErrSpeechClosed
	default:
	}
	v.n++
	p := &voicePiece{id: "p" + strconv.Itoa(v.n), audio: audio, words: words, done: make(chan error, 2)}
	v.mu.Lock()
	v.cur = p
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		if v.cur == p {
			v.cur = nil
		}
		v.mu.Unlock()
	}()
	for _, m := range v.start(p.id, text) {
		if err := writeJSON(ctx, v.conn, m); err != nil {
			v.fail(err)
			return ErrSpeechClosed
		}
	}
	select {
	case err := <-p.done:
		return err
	case <-v.ended:
		return ErrSpeechClosed
	case <-ctx.Done():
		// The connection stays: the piece is cancelled and its late audio
		// dropped.
		if v.stop != nil {
			_ = writeJSON(context.Background(), v.conn, v.stop(p.id))
		}
		return ctx.Err()
	}
}

func (v *ctxVoice) Close() error {
	if v.bye != nil {
		_ = writeJSON(context.Background(), v.conn, v.bye)
	}
	v.fail(ErrSpeechClosed)
	return v.conn.Close(websocket.StatusNormalClosure, "")
}

// pcmFromWAV reads a recording as 16 kHz PCM for a live session: a WAV of
// any rate (what calls send), or nothing.
func pcmFromWAV(a Audio) ([]byte, error) {
	pcm, rate, err := speech.ReadWAV(a.Data)
	if err != nil {
		return nil, errors.New("this model takes live audio only: a recording must be a WAV (calls send one); for dictation pick a model that takes files")
	}
	return speech.Resample(pcm, rate, LiveSampleRate), nil
}

// transcribeByLive transcribes a whole recording through a live session:
// for a model that only streams.
func transcribeByLive(ctx context.Context, lt LiveTranscriber, model string, a Audio) (Transcript, error) {
	pcm, err := pcmFromWAV(a)
	if err != nil {
		return Transcript{}, err
	}
	sess, err := lt.OpenTranscription(ctx, model, LiveOptions{Language: a.Language})
	if err != nil {
		return Transcript{}, err
	}
	defer sess.Close()
	go func() {
		for range sess.Events() {
		}
	}()
	const chunk = LiveSampleRate / 10 * 2 // 100 ms
	for at := 0; at < len(pcm); at += chunk {
		if err := sess.Send(pcm[at:min(at+chunk, len(pcm))]); err != nil {
			return Transcript{}, err
		}
	}
	text, err := sess.Finish(ctx)
	return Transcript{Text: text, Language: a.Language, Seconds: float64(len(pcm)) / 2 / LiveSampleRate}, err
}

// streamLines reads newline-separated JSON from a response body, one value
// per line, blank lines skipped.
func streamLines(r io.Reader, each func(line []byte) error) error {
	dec := json.NewDecoder(r)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := each(raw); err != nil {
			return err
		}
	}
}

// pcmPipe hands back raw PCM that a goroutine writes, for a vendor whose
// HTTP answer is JSON around the audio.
func pcmPipe(body io.ReadCloser, decode func(io.Reader, io.Writer) error) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		err := decode(body, pw)
		body.Close()
		pw.CloseWithError(err)
	}()
	return pr
}

// le16 is a sample as bytes, for tests and silence.
func le16(v int16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(v))
	return b[:]
}

// jsonBody is a JSON request body.
func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// wsTurn is a live transcription session on a vendor socket: the shared
// writing, keepalive and closing around the vendor's reader.
type wsTurn struct {
	*liveTurn
	conn *websocket.Conn
	wmu  sync.Mutex
	// finishing is set once Finish asked for the final words.
	finishing bool
	stop      chan struct{}
	stopOnce  sync.Once
	// audio turns PCM into the vendor's message; nil sends binary frames.
	audio func(pcm []byte) (websocket.MessageType, []byte)
	// finish is what asks for the final words; bye what ends the session.
	finish []any
	bye    any
	// grace is how long Finish waits for the vendor's final marker before
	// it answers with what was heard (some send none for a short turn).
	grace time.Duration
}

func newWSTurn(conn *websocket.Conn) *wsTurn {
	return &wsTurn{liveTurn: newLiveTurn(), conn: conn, stop: make(chan struct{}), grace: 3 * time.Second}
}

func (t *wsTurn) write(typ websocket.MessageType, b []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return t.conn.Write(ctx, typ, b)
}

func (t *wsTurn) writeJSON(v any) error {
	if s, ok := v.(string); ok {
		return t.write(websocket.MessageText, []byte(s))
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return t.write(websocket.MessageText, b)
}

// keepalive sends msg every interval until the session stops.
func (t *wsTurn) keepalive(every time.Duration, msg any) {
	go func() {
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-t.stop:
				return
			case <-t.ended:
				return
			case <-tick.C:
				_ = t.writeJSON(msg)
			}
		}
	}()
}

func (t *wsTurn) Send(pcm []byte) error {
	select {
	case <-t.ended:
		return ErrSpeechClosed
	default:
	}
	if t.audio != nil {
		typ, b := t.audio(pcm)
		return t.write(typ, b)
	}
	return t.write(websocket.MessageBinary, pcm)
}

func (t *wsTurn) Events() <-chan TranscriptEvent { return t.events }

func (t *wsTurn) isFinishing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finishing
}

func (t *wsTurn) Finish(ctx context.Context) (string, error) {
	t.mu.Lock()
	t.finishing = true
	t.mu.Unlock()
	for _, m := range t.finish {
		if err := t.writeJSON(m); err != nil {
			break
		}
	}
	gctx, cancel := context.WithTimeout(ctx, t.grace)
	defer cancel()
	text, err := t.wait(gctx)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		// No final marker in time: what was heard is the turn.
		t.mu.Lock()
		text = t.text()
		t.mu.Unlock()
		return text, nil
	}
	return text, err
}

func (t *wsTurn) Close() error {
	t.stopOnce.Do(func() {
		close(t.stop)
		if t.bye != nil {
			_ = t.writeJSON(t.bye)
		}
	})
	t.end(nil)
	return t.conn.Close(websocket.StatusNormalClosure, "")
}
