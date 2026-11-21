package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testPNG is a real, tiny PNG.
func testPNG() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	return buf.Bytes()
}

// imageFake is a vendor's API as a test server: every request is recorded
// with its JSON body, and answered by the handler for its path.
type imageFake struct {
	t    *testing.T
	srv  *httptest.Server
	seen []seenReq
	on   func(f *imageFake, w http.ResponseWriter, r *http.Request, body map[string]any)
}

type seenReq struct {
	method, path string
	header       http.Header
	body         map[string]any
	form         map[string]string
	files        map[string]int
}

func newImageFake(t *testing.T, on func(f *imageFake, w http.ResponseWriter, r *http.Request, body map[string]any)) *imageFake {
	f := &imageFake{t: t, on: on}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := seenReq{method: r.Method, path: r.URL.Path, header: r.Header.Clone()}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart: %v", err)
			}
			req.form, req.files = map[string]string{}, map[string]int{}
			for k, v := range r.MultipartForm.Value {
				req.form[k] = v[0]
			}
			for k, v := range r.MultipartForm.File {
				req.files[k] = len(v)
			}
		} else if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			if err := json.Unmarshal(raw, &req.body); err != nil {
				t.Errorf("%s %s: body is not JSON: %s", r.Method, r.URL.Path, raw)
			}
		}
		f.seen = append(f.seen, req)
		f.on(f, w, r, req.body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *imageFake) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// find is the first recorded request to a path.
func (f *imageFake) find(method, path string) seenReq {
	f.t.Helper()
	for _, r := range f.seen {
		if r.method == method && r.path == path {
			return r
		}
	}
	f.t.Fatalf("no %s %s among %d requests", method, path, len(f.seen))
	return seenReq{}
}

func maker(t *testing.T, provider, base string) ImageMaker {
	t.Helper()
	c, err := New(provider, Settings{"api_key": "sk-secret", "base_url": base})
	if err != nil {
		t.Fatal(err)
	}
	return c.(ImageMaker)
}

func quickPolls(t *testing.T) {
	first, max := imagePollFirst, imagePollMax
	imagePollFirst, imagePollMax = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { imagePollFirst, imagePollMax = first, max })
}

func b64PNG() string { return base64.StdEncoding.EncodeToString(testPNG()) }

func wantPNGs(t *testing.T, res ImageResult, err error, n int) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != n {
		t.Fatalf("%d images, want %d", len(res.Images), n)
	}
	for _, img := range res.Images {
		if img.Mime != "image/png" || !bytes.Equal(img.Data, testPNG()) {
			t.Fatalf("image came back as %s, %d bytes", img.Mime, len(img.Data))
		}
	}
}

func ask(prompt string) ImageRequest {
	r := ImageRequest{Prompt: prompt}
	_ = r.Validate()
	return r
}

func TestImageRequestValidate(t *testing.T) {
	for name, r := range map[string]ImageRequest{
		"no prompt":       {Prompt: "  "},
		"too many":        {Prompt: "x", Count: 5},
		"odd aspect":      {Prompt: "x", Aspect: "7:5"},
		"too many inputs": {Prompt: "x", Inputs: make([]Image, MaxImageInputs+1)},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	r := ImageRequest{Prompt: " a cat ", Aspect: "16:9"}
	if err := r.Validate(); err != nil || r.Count != 1 || r.Prompt != "a cat" {
		t.Fatalf("%+v %v", r, err)
	}
}

// OpenAI: JSON to /images/generations, a size for the aspect, no legacy
// response_format for a GPT image model; edits as JSON image_url entries.
func TestOpenAIImages(t *testing.T) {
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		f.json(w, map[string]any{"data": []map[string]any{{"b64_json": b64PNG()}, {"b64_json": b64PNG()}}})
	})
	// The OpenAI client proper (its default base decides the dialect).
	c := &openAICompatClient{base: f.srv.URL, key: "sk-secret", openAI: true}
	req := ask("a cat")
	req.Aspect, req.Count = "16:9", 2
	res, err := c.MakeImages(context.Background(), "gpt-image-2", req)
	wantPNGs(t, res, err, 2)
	got := f.find("POST", "/images/generations")
	if got.body["size"] != "1344x768" || got.body["n"] != float64(2) || got.body["response_format"] != nil || got.header.Get("Authorization") != "Bearer sk-secret" {
		t.Fatalf("generation request: %v", got.body)
	}
	if s := openAISize("gpt-image-1.5", "9:16"); s != "1024x1536" {
		t.Fatalf("gpt-image-1.5 portrait: %s", s)
	}
	if s := openAISize("dall-e-3", "16:9"); s != "1792x1024" {
		t.Fatalf("dall-e-3 wide: %s", s)
	}

	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err = c.MakeImages(context.Background(), "gpt-image-2", req)
	wantPNGs(t, res, err, 2)
	edit := f.find("POST", "/images/edits")
	refs, _ := edit.body["images"].([]any)
	if len(refs) != 1 || !strings.HasPrefix(refs[0].(map[string]any)["image_url"].(string), "data:image/png;base64,") {
		t.Fatalf("edit request: %v", edit.body)
	}
}

// A self-hosted server: base64 is asked for, an edit is the multipart form,
// and an answer that is an address is downloaded.
func TestLocalImages(t *testing.T) {
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch r.URL.Path {
		case "/file.png":
			_, _ = w.Write(testPNG())
		case "/images/edits":
			f.json(w, map[string]any{"data": []map[string]any{{"url": f.srv.URL + "/file.png"}}})
		default:
			f.json(w, map[string]any{"data": []map[string]any{{"b64_json": b64PNG()}}})
		}
	})
	m := maker(t, Local, f.srv.URL)
	res, err := m.MakeImages(context.Background(), "sdxl", ask("a cat"))
	wantPNGs(t, res, err, 1)
	if got := f.find("POST", "/images/generations"); got.body["response_format"] != "b64_json" || got.body["size"] != nil {
		t.Fatalf("local request: %v", got.body)
	}
	req := ask("make it blue")
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err = m.MakeImages(context.Background(), "sdxl", req)
	wantPNGs(t, res, err, 1)
	if got := f.find("POST", "/images/edits"); got.files["image"] != 1 || got.form["prompt"] != "make it blue" {
		t.Fatalf("local edit: %v %v", got.form, got.files)
	}
}

// OpenRouter's Images API: one shape, the aspect by name, inputs as
// input_references, the cost from usage. A refusal never shows the key.
func TestOpenRouterImages(t *testing.T) {
	fail := false
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if fail {
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"error":{"code":402,"message":"Insufficient credits for sk-secret"}}`))
			return
		}
		f.json(w, map[string]any{"data": []map[string]any{{"b64_json": b64PNG(), "media_type": "image/png"}}, "usage": map[string]any{"cost": 0.04}})
	})
	m := maker(t, OpenRouter, f.srv.URL)
	req := ask("a cat")
	req.Aspect = "3:2"
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err := m.MakeImages(context.Background(), "google/gemini-3-pro-image", req)
	wantPNGs(t, res, err, 1)
	if res.Cost != 0.04 {
		t.Fatalf("cost %v", res.Cost)
	}
	got := f.find("POST", "/images")
	refs, _ := got.body["input_references"].([]any)
	if got.body["aspect_ratio"] != "3:2" || got.body["model"] != "google/gemini-3-pro-image" || len(refs) != 1 || got.body["n"] != nil {
		t.Fatalf("openrouter request: %v", got.body)
	}
	fail = true
	_, err = m.MakeImages(context.Background(), "x/y", ask("a cat"))
	if err == nil || !strings.Contains(err.Error(), "402") || !strings.Contains(err.Error(), "Insufficient credits") || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("refusal: %v", err)
	}
}

// fal: submit to the queue, poll the status address it names, read the result
// address, download the picture. A failed request ends COMPLETED with an error.
func TestFalImages(t *testing.T) {
	quickPolls(t)
	polls, broken := 0, false
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch {
		case r.Method == "POST":
			f.json(w, map[string]any{"request_id": "r1", "status_url": f.srv.URL + "/fal-ai/flux-2/requests/r1/status", "response_url": f.srv.URL + "/fal-ai/flux-2/requests/r1"})
		case strings.HasSuffix(r.URL.Path, "/status"):
			polls++
			switch {
			case polls < 3:
				f.json(w, map[string]any{"status": "IN_PROGRESS"})
			case broken:
				f.json(w, map[string]any{"status": "COMPLETED", "error": "the model fell over"})
			default:
				f.json(w, map[string]any{"status": "COMPLETED"})
			}
		case r.URL.Path == "/out.png":
			if r.Header.Get("Authorization") != "" {
				t.Error("the key went with the download of a result")
			}
			_, _ = w.Write(testPNG())
		default:
			f.json(w, map[string]any{"images": []map[string]any{{"url": f.srv.URL + "/out.png", "content_type": "image/png"}}})
		}
	})
	m := maker(t, Fal, f.srv.URL)
	req := ask("a cat")
	req.Aspect = "16:9"
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err := m.MakeImages(context.Background(), "fal-ai/flux-2/edit", req)
	wantPNGs(t, res, err, 1)
	got := f.find("POST", "/fal-ai/flux-2/edit")
	urls, _ := got.body["image_urls"].([]any)
	if got.header.Get("Authorization") != "Key sk-secret" || got.body["aspect_ratio"] != "16:9" || got.body["image_size"] != "landscape_16_9" || len(urls) != 1 {
		t.Fatalf("fal request: %v", got.body)
	}
	polls, broken = 0, true
	if _, err := m.MakeImages(context.Background(), "fal-ai/flux-2/edit", ask("a cat")); err == nil || !strings.Contains(err.Error(), "fell over") {
		t.Fatalf("a failed request: %v", err)
	}
}

// Replicate: the model's schema says what it calls its inputs; a prediction
// still running is polled; a large input is uploaded first.
func TestReplicateImages(t *testing.T) {
	quickPolls(t)
	polls := 0
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		prop := func(kind string) map[string]any { return map[string]any{"type": kind} }
		schema := func(props map[string]any) map[string]any {
			return map[string]any{"latest_version": map[string]any{"openapi_schema": map[string]any{"components": map[string]any{"schemas": map[string]any{"Input": map[string]any{"properties": props}}}}}}
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/models/google/nano-banana":
			f.json(w, schema(map[string]any{"prompt": prop("string"), "image_input": prop("array"), "aspect_ratio": prop("string")}))
		case r.Method == "GET" && r.URL.Path == "/v1/models/acme/plain":
			f.json(w, schema(map[string]any{"prompt": prop("string")}))
		case r.URL.Path == "/v1/files":
			f.json(w, map[string]any{"id": "f1", "urls": map[string]any{"get": f.srv.URL + "/v1/files/f1"}})
		case r.Method == "POST":
			f.json(w, map[string]any{"id": "p1", "status": "starting", "urls": map[string]any{"get": f.srv.URL + "/v1/predictions/p1"}})
		case r.URL.Path == "/v1/predictions/p1":
			if polls++; polls < 2 {
				f.json(w, map[string]any{"status": "processing"})
				return
			}
			f.json(w, map[string]any{"status": "succeeded", "output": []string{f.srv.URL + "/out.png"}})
		case r.URL.Path == "/out.png":
			if r.Header.Get("Authorization") != "Bearer sk-secret" {
				t.Error("an output on Replicate's own host needs the token")
			}
			_, _ = w.Write(testPNG())
		}
	})
	m := maker(t, Replicate, f.srv.URL)
	req := ask("a cat")
	req.Aspect = "4:3"
	big := Image{Mime: "image/png", Data: append(testPNG(), make([]byte, 200<<10)...)}
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}, big}
	res, err := m.MakeImages(context.Background(), "google/nano-banana", req)
	wantPNGs(t, res, err, 1)
	got := f.find("POST", "/v1/models/google/nano-banana/predictions")
	input, _ := got.body["input"].(map[string]any)
	refs, _ := input["image_input"].([]any)
	if input["aspect_ratio"] != "4:3" || len(refs) != 2 || !strings.HasPrefix(refs[0].(string), "data:image/png") || refs[1] != f.srv.URL+"/v1/files/f1" || got.header.Get("Prefer") != "wait" {
		t.Fatalf("replicate request: %v", input)
	}
	// A model with no image input refuses pictures, and is sent no aspect.
	if _, err := m.MakeImages(context.Background(), "acme/plain", req); err == nil || !strings.Contains(err.Error(), "takes no input images") {
		t.Fatalf("inputs to a model that takes none: %v", err)
	}
	if _, err := m.MakeImages(context.Background(), "flux", ask("x")); err == nil {
		t.Fatal("a model with no owner was accepted")
	}
}

// BFL: each family's own body, a job polled at the address the answer names,
// a moderated request worded.
func TestBFLImages(t *testing.T) {
	quickPolls(t)
	status := "Ready"
	polls := 0
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch {
		case r.Method == "POST":
			f.json(w, map[string]any{"id": "j1", "polling_url": f.srv.URL + "/poll/j1"})
		case r.URL.Path == "/poll/j1":
			if polls++; polls < 2 {
				f.json(w, map[string]any{"status": "Pending"})
				return
			}
			f.json(w, map[string]any{"status": status, "result": map[string]any{"sample": f.srv.URL + "/sample.png"}})
		default:
			if r.Header.Get("x-key") != "" {
				t.Error("the key went with the signed result link")
			}
			_, _ = w.Write(testPNG())
		}
	})
	m := maker(t, BFL, f.srv.URL)
	req := ask("a cat")
	req.Aspect, req.Count = "16:9", 2
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}, {Mime: "image/png", Data: testPNG()}}
	res, err := m.MakeImages(context.Background(), "flux-2-pro", req)
	wantPNGs(t, res, err, 2)
	got := f.find("POST", "/v1/flux-2-pro")
	if got.header.Get("x-key") != "sk-secret" || got.body["width"] != float64(1344) || got.body["height"] != float64(768) || got.body["input_image"] != b64PNG() || got.body["input_image_2"] != b64PNG() || got.body["aspect_ratio"] != nil {
		t.Fatalf("flux-2 request: %v", got.body)
	}
	three, _ := bflBody("flux-3-image", req)
	if imgs, _ := three["images"].([]string); len(imgs) != 2 || three["aspect_ratio"] != "16:9" || three["width"] != nil || three["output_format"] != nil {
		t.Fatalf("flux-3 body: %v", three)
	}
	kontext, _ := bflBody("flux-kontext-pro", req)
	if kontext["aspect_ratio"] != "16:9" || kontext["input_image_2"] == nil {
		t.Fatalf("kontext body: %v", kontext)
	}
	if _, err := bflBody("flux-pro-1.1", req); err == nil {
		t.Fatal("a model with no inputs took pictures")
	}
	status, polls = "Request Moderated", 0
	if _, err := m.MakeImages(context.Background(), "flux-2-pro", ask("x")); err == nil || !strings.Contains(err.Error(), "content filter") {
		t.Fatalf("moderated: %v", err)
	}
}

// Stability: a multipart form to the service of the model, the picture's
// bytes back; SD3 names its model and takes no aspect with an input.
func TestStabilityImages(t *testing.T) {
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG())
	})
	m := maker(t, Stability, f.srv.URL)
	req := ask("a cat")
	req.Aspect = "4:3"
	res, err := m.MakeImages(context.Background(), "ultra", req)
	wantPNGs(t, res, err, 1)
	got := f.find("POST", "/v2beta/stable-image/generate/ultra")
	if got.form["aspect_ratio"] != "5:4" || got.form["prompt"] != "a cat" || got.form["model"] != "" || got.header.Get("Accept") != "image/*" {
		t.Fatalf("ultra request: %v", got.form)
	}
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err = m.MakeImages(context.Background(), "sd3.5-large", req)
	wantPNGs(t, res, err, 1)
	got = f.find("POST", "/v2beta/stable-image/generate/sd3")
	if got.form["model"] != "sd3.5-large" || got.form["mode"] != "image-to-image" || got.form["strength"] == "" || got.form["aspect_ratio"] != "" || got.files["image"] != 1 {
		t.Fatalf("sd3 edit: %v %v", got.form, got.files)
	}
	if _, err := m.MakeImages(context.Background(), "core", req); err == nil {
		t.Fatal("core took an input image")
	}
}

// Google: generateContent with the pictures as inline parts; an answer in
// words alone is an error that says them.
func TestGoogleImages(t *testing.T) {
	words := false
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		parts := []map[string]any{{"text": "Here you go."}, {"inlineData": map[string]any{"mimeType": "image/png", "data": b64PNG()}}}
		if words {
			parts = parts[:1]
			parts[0]["text"] = "I can't draw that."
		}
		f.json(w, map[string]any{"candidates": []map[string]any{{"finishReason": "STOP", "content": map[string]any{"parts": parts}}}})
	})
	m := maker(t, Google, f.srv.URL)
	req := ask("a cat")
	req.Aspect = "9:16"
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err := m.MakeImages(context.Background(), "gemini-3-pro-image", req)
	wantPNGs(t, res, err, 1)
	got := f.find("POST", "/models/gemini-3-pro-image:generateContent")
	cfg, _ := got.body["generationConfig"].(map[string]any)
	parts := got.body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if got.header.Get("x-goog-api-key") != "sk-secret" || cfg["imageConfig"].(map[string]any)["aspectRatio"] != "9:16" || len(parts) != 2 || parts[1].(map[string]any)["inlineData"] == nil {
		t.Fatalf("google request: %v", got.body)
	}
	words = true
	if _, err := m.MakeImages(context.Background(), "gemini-3-pro-image", ask("x")); err == nil || !strings.Contains(err.Error(), "I can't draw that.") {
		t.Fatalf("an answer in words: %v", err)
	}
}

// xAI: the OpenAI answer, an aspect by name, an edit as one JSON image.
func TestXAIImages(t *testing.T) {
	f := newImageFake(t, func(f *imageFake, w http.ResponseWriter, r *http.Request, _ map[string]any) {
		f.json(w, map[string]any{"data": []map[string]any{{"b64_json": b64PNG()}}})
	})
	m := maker(t, XAI, f.srv.URL)
	req := ask("a cat")
	req.Aspect = "1:1"
	res, err := m.MakeImages(context.Background(), "grok-imagine-image-2.0", req)
	wantPNGs(t, res, err, 1)
	if got := f.find("POST", "/images/generations"); got.body["aspect_ratio"] != "1:1" || got.body["response_format"] != "b64_json" || got.body["size"] != nil {
		t.Fatalf("xai request: %v", got.body)
	}
	req.Inputs = []Image{{Mime: "image/png", Data: testPNG()}}
	res, err = m.MakeImages(context.Background(), "grok-imagine-image-2.0", req)
	wantPNGs(t, res, err, 1)
	if got := f.find("POST", "/images/edits"); got.body["image"].(map[string]any)["type"] != "image_url" {
		t.Fatalf("xai edit: %v", got.body)
	}
}

// What is not a picture is never handed on as one.
func TestImageBytesAreChecked(t *testing.T) {
	if _, err := imageOf([]byte("<html>sign in</html>")); err == nil {
		t.Fatal("HTML passed for an image")
	}
	if _, err := fetchImage(context.Background(), "file:///etc/passwd", nil); err == nil {
		t.Fatal("a file address was fetched")
	}
	img, err := fetchImage(context.Background(), "data:image/png;base64,"+b64PNG(), nil)
	if err != nil || img.Mime != "image/png" {
		t.Fatalf("data URL: %v", err)
	}
}
