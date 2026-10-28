package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
	"silo.agent/internal/speech"
	"silo.agent/internal/speech/opus"
)

// Every kind of model has an allowlist of its own, set like the text one.
func TestKindAllowlists(t *testing.T) {
	h := voiceHarness(t)
	set := func(kind string, models ...string) (*v1.Settings, error) {
		res, err := h.Client.SetModels(h.Ctx(), connect.NewRequest(&v1.SetModelsRequest{Kind: kind, Models: models}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	st, err := set(llm.KindSpeech, "dummy/voice", "dummy/voice-2")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string][]string{}
	for _, k := range st.GetAllowed() {
		for _, m := range k.GetModels() {
			allowed[k.GetKind()] = append(allowed[k.GetKind()], m.GetId())
		}
	}
	if strings.Join(allowed["speech"], ",") != "dummy/voice,dummy/voice-2" {
		t.Fatalf("speech models %v", allowed)
	}
	// The transcribe role's model counts as allowed with no list.
	if strings.Join(allowed["transcription"], ",") != "dummy/whisper" {
		t.Fatalf("transcription models %v", allowed)
	}
	if _, err := set(llm.KindSpeech, "anthropic/claude-sonnet-5"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("anthropic cannot speak: %v", err)
	}
	if _, err := set("music", "dummy/x"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("an unknown kind: %v", err)
	}
	if _, err := set(llm.KindSpeech, "dummy/echo"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a text model on the speech list: %v", err)
	}
}

// A Bot's voice is its own, and Read aloud streams it.
func TestReadAloud(t *testing.T) {
	dummy.Reset()
	h := voiceHarness(t)
	id := h.CreateBot("Narrator").GetId()

	lm, err := h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: id}))
	if err != nil || !lm.Msg.GetSpeechEnabled() || !lm.Msg.GetCallEnabled() {
		t.Fatalf("speech %v call %v: %v", lm.Msg.GetSpeechEnabled(), lm.Msg.GetCallEnabled(), err)
	}
	bot, err := h.Client.SetBotVoice(h.Ctx(), connect.NewRequest(&v1.SetBotVoiceRequest{BotId: id, Voice: "warm", Speed: 1.25}))
	if err != nil || bot.Msg.GetVoice() != "warm" || bot.Msg.GetSpeechSpeed() != 1.25 {
		t.Fatalf("set voice: %v %v", bot.Msg, err)
	}
	if _, err := h.Client.SetBotVoice(h.Ctx(), connect.NewRequest(&v1.SetBotVoiceRequest{BotId: id, SpeechModel: "dummy/echo"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a text model as the voice: %v", err)
	}
	if _, err := h.Client.SetBotVoice(h.Ctx(), connect.NewRequest(&v1.SetBotVoiceRequest{BotId: id, Speed: 9})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("speed 9: %v", err)
	}

	prep, err := h.Client.PrepareSpeech(h.Ctx(), connect.NewRequest(&v1.PrepareSpeechRequest{BotId: id, Text: "**Hello** there. See [the docs](https://example.com/a)."}))
	if err != nil {
		t.Fatal(err)
	}
	url := h.URL + prep.Msg.GetUrl()
	_, stranger := h.SignedInUser("eve@test.local")
	if res, err := stranger.Get(url); err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("someone else's ticket: %v %v", res.StatusCode, err)
	}
	res, err := h.HTTP.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "audio/wav" || !strings.HasPrefix(string(body), "RIFF") || len(body) < 1000 {
		t.Fatalf("status %d type %q, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(body))
	}
	said := dummy.Spoken()
	if len(said) == 0 || said[0].Text != "Hello there." || said[0].Voice != "warm" || said[0].Speed != 1.25 {
		t.Fatalf("spoke %+v", said)
	}
	// A player that asks again gets the same audio, not a second round of speech.
	again, err := h.HTTP.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := io.ReadAll(again.Body)
	again.Body.Close()
	if string(replay) != string(body) || len(dummy.Spoken()) != len(said) {
		t.Fatalf("replay: %d bytes (first %d), spoke %d times", len(replay), len(body), len(dummy.Spoken()))
	}
}

// callClient is the browser's side of a call: the control socket and a
// WebRTC peer that receives the Bot's voice.
type callClient struct {
	t       *testing.T
	ws      *websocket.Conn
	pc      *webrtc.PeerConnection
	msgs    chan map[string]any
	packets atomic.Int64
}

func dialCall(t *testing.T, h *apptest.H, botID, chat string) *callClient {
	t.Helper()
	if err := opus.Load(""); err != nil {
		t.Skipf("no libopus: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(h.URL, "http") + "/voice/call?bot=" + botID + "&chat=" + chat
	ws, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: &http.Client{Jar: h.HTTP.Jar}, HTTPHeader: http.Header{"Origin": {h.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(1 << 20)
	c := &callClient{t: t, ws: ws, msgs: make(chan map[string]any, 256)}
	go func() {
		for {
			_, data, err := ws.Read(context.Background())
			if err != nil {
				close(c.msgs)
				return
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			if m["t"] == "ice" && c.pc != nil {
				if cand, ok := m["candidate"].(map[string]any); ok {
					b, _ := json.Marshal(cand)
					var init webrtc.ICECandidateInit
					_ = json.Unmarshal(b, &init)
					_ = c.pc.AddICECandidate(init)
				}
				continue
			}
			c.msgs <- m
		}
	}()
	hello := c.expect("hello")
	if hello["call_id"] == "" {
		t.Fatalf("hello %v", hello)
	}
	se := webrtc.SettingEngine{}
	se.SetIncludeLoopbackCandidate(true)
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	c.pc = pc
	t.Cleanup(func() { pc.Close(); ws.CloseNow() })
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			if _, _, err := tr.ReadRTP(); err != nil {
				return
			}
			c.packets.Add(1)
		}
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	c.send(map[string]any{"t": "offer", "sdp": pc.LocalDescription().SDP})
	answer := c.expect("answer")
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer["sdp"].(string)}); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *callClient) send(m map[string]any) {
	b, _ := json.Marshal(m)
	if err := c.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

// speak sends one turn: the dummy transcriber hears the WAV's samples as text.
func (c *callClient) speak(words string) {
	c.send(map[string]any{"t": "turn", "eou_ms": 240})
	if err := c.ws.Write(context.Background(), websocket.MessageBinary, speech.WAV([]byte(words), 16000)); err != nil {
		c.t.Fatal(err)
	}
}

// expect waits for the next message of a type, skipping others.
func (c *callClient) expect(typ string) map[string]any {
	c.t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case m, ok := <-c.msgs:
			if !ok {
				c.t.Fatalf("socket closed waiting for %q", typ)
			}
			if m["t"] == typ {
				return m
			}
		case <-deadline:
			c.t.Fatalf("no %q message", typ)
		}
	}
}

// A call hears a turn, hands it to the Bot as a spoken message, and reads the
// reply aloud on the WebRTC track, sentence by sentence.
func TestVoiceCall(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_vc1", dummy.Turn{Text: "Hello there. I can hear you **fine**."})
	h := voiceHarness(t)
	id := h.CreateBot("Talker").GetId()
	c := dialCall(t, h, id, "")

	c.speak("Test_vc1 can you hear me")
	if heard := c.expect("heard"); heard["text"] != "Test_vc1 can you hear me" {
		t.Fatalf("heard %v", heard)
	}
	first := c.expect("say")
	if first["text"] != "Hello there." {
		t.Fatalf("first sentence %v", first)
	}
	if second := c.expect("say"); second["text"] != "I can hear you fine." {
		t.Fatalf("second sentence %v", second)
	}
	waitFor(t, func() bool { return c.packets.Load() > 10 }, "the Bot's voice on the track")

	// The turn is in the chat as the human's own message, marked spoken.
	chats, _ := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: id}))
	var main string
	for _, ch := range chats.Msg.GetChats() {
		if ch.GetMain() {
			main = ch.GetId()
		}
	}
	var spoken *db.RunEvent
	waitFor(t, func() bool {
		var ev db.RunEvent
		h.App.DB.Joins("JOIN runs ON runs.id = run_events.run_id").Where("runs.chat_id = ? AND run_events.kind = 'user'", main).Find(&ev)
		if ev.ID != "" {
			spoken = &ev
		}
		return spoken != nil
	}, "the spoken turn")
	if spoken.Tool != "" || spoken.UserID == "" || !strings.Contains(spoken.Meta, `"call_id"`) {
		t.Fatalf("spoken turn %+v", spoken)
	}
	reqs := dummy.Streamed()
	if got := userTexts(reqs[len(reqs)-1]); len(got) == 0 || !strings.Contains(got[len(got)-1], "[spoken]\nTest_vc1 can you hear me") {
		t.Fatalf("the model read %q", got)
	}

	c.send(map[string]any{"t": "hangup"})
	if bye := c.expect("bye"); bye["reason"] != "hangup" {
		t.Fatalf("bye %v", bye)
	}
	var row db.VoiceCall
	waitFor(t, func() bool { h.App.DB.Find(&row, "bot_id = ?", id); return row.EndReason == "hangup" }, "the call's row")
	if row.Turns != 1 || row.ChatID != main {
		t.Fatalf("call row %+v", row)
	}
	var kinds []string
	h.App.DB.Model(&db.RunEvent{}).Joins("JOIN runs ON runs.id = run_events.run_id").Where("runs.chat_id = ? AND run_events.kind IN ?", main, []string{"call_start", "call_end"}).Order("run_events.seq").Pluck("run_events.kind", &kinds)
	if strings.Join(kinds, ",") != "call_start,call_end" {
		t.Fatalf("call events %v", kinds)
	}
}

// Speaking over the Bot stops its voice, and the next turn tells it where it
// was cut off; the history stays append-only.
func TestVoiceCallInterrupt(t *testing.T) {
	dummy.Reset()
	long := "The meeting is at six. Bring the slides and the budget. Anna will join late. We need to decide on the venue. Then we can talk about dinner."
	// One conversation is one script: the second turn answers the second message.
	dummy.Script("Test_vc2", dummy.Turn{Text: long}, dummy.Turn{Text: "Sure, nine then."})
	h := voiceHarness(t)
	id := h.CreateBot("Talker").GetId()
	c := dialCall(t, h, id, "")

	c.speak("Test_vc2 when is the meeting")
	c.expect("say")
	c.send(map[string]any{"t": "duck"})
	c.send(map[string]any{"t": "interrupt"})
	waitFor(t, func() bool { return h.App.LiveRunID(id, mainChatOf(t, h, id)) == "" }, "the first reply to end")
	c.speak("make it nine")
	if say := c.expect("say"); say["text"] != "Sure, nine then." {
		t.Fatalf("after the interruption %v", say)
	}
	reqs := dummy.Streamed()
	got := userTexts(reqs[len(reqs)-1])
	if last := got[len(got)-1]; !strings.Contains(last, "[spoken; I cut you off after: “") || !strings.HasSuffix(last, "make it nine") {
		t.Fatalf("the model read %q", last)
	}
	assertAppendOnly(t, reqs)
}

func mainChatOf(t *testing.T, h *apptest.H, botID string) string {
	t.Helper()
	chats, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chats.Msg.GetChats() {
		if ch.GetMain() {
			return ch.GetId()
		}
	}
	t.Fatal("no main chat")
	return ""
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
