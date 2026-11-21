package llm

// The image vendors' own APIs: fal, Replicate, Black Forest Labs, Stability
// AI, Google's Gemini API and xAI. Each has image models only here. The wire
// shapes are from each vendor's API reference (checked 2026-11) and pinned by
// fakes in image_test.go.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// imageVendor is what the vendors share: a name for errors, the key, the base.
type imageVendor struct {
	imageOnly
	slug string
	key  string
	base string
}

// newImageVendor reads a vendor's key and base URL.
func newImageVendor(name, slug, defaultBase string, s Settings) (imageVendor, error) {
	key := s.Get("api_key")
	if key == "" {
		return imageVendor{}, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	if base == "" {
		base = defaultBase
	}
	return imageVendor{imageOnly: imageOnly{name}, slug: slug, key: key, base: base}, nil
}

// --- fal ---

const (
	falQueue = "https://queue.fal.run"
	falAPI   = "https://api.fal.ai"
)

// fal runs a model as a queued request: submit, poll the status address the
// answer names, then read the result address.
type fal struct {
	imageVendor
	// api is the platform API, which lists models.
	api string
}

func newFal(s Settings) (Client, error) {
	v, err := newImageVendor("fal", "fal", falQueue, s)
	if err != nil {
		return nil, err
	}
	f := &fal{imageVendor: v, api: falAPI}
	if s.Get("base_url") != "" {
		f.api = v.base
	}
	return f, nil
}

func (f *fal) header() http.Header { return http.Header{"Authorization": {"Key " + f.key}} }

// falSizes are fal's names for the shapes its models take as image_size.
var falSizes = map[string]string{
	"1:1": "square_hd", "4:3": "landscape_4_3", "3:4": "portrait_4_3", "16:9": "landscape_16_9", "9:16": "portrait_16_9",
	"3:2": "landscape_4_3", "2:3": "portrait_4_3", "21:9": "landscape_16_9",
}

func (f *fal) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	// Models differ in what they call a shape: some take aspect_ratio, some
	// image_size. Both are sent; a model reads the one it knows.
	body := map[string]any{"prompt": req.Prompt}
	if req.Aspect != "" {
		body["aspect_ratio"] = req.Aspect
		body["image_size"] = falSizes[req.Aspect]
	}
	if len(req.Inputs) > 0 {
		urls := make([]string, 0, len(req.Inputs))
		for _, img := range req.Inputs {
			urls = append(urls, dataURL(img))
		}
		body["image_urls"] = urls
		body["image_url"] = urls[0]
	}
	return many(ctx, req.Count, func(ctx context.Context) ([]Image, float64, error) {
		raw, err := imageCall(ctx, "fal", http.MethodPost, f.base+"/"+strings.Trim(model, "/"), f.header(), body, f.key)
		if err != nil {
			return nil, 0, err
		}
		var job struct {
			StatusURL   string `json:"status_url"`
			ResponseURL string `json:"response_url"`
		}
		if json.Unmarshal(raw, &job) != nil || job.StatusURL == "" || job.ResponseURL == "" {
			return nil, 0, errors.New("fal: the request was not queued")
		}
		// A request that failed also ends COMPLETED, with an error.
		err = imagePoll(ctx, func() (bool, error) {
			raw, err := imageCall(ctx, "fal", http.MethodGet, job.StatusURL, f.header(), nil, f.key)
			if err != nil {
				return false, err
			}
			var st struct {
				Status string `json:"status"`
				Error  any    `json:"error"`
			}
			_ = json.Unmarshal(raw, &st)
			if st.Error != nil {
				return false, fmt.Errorf("fal: %v", textOf(st.Error))
			}
			return st.Status == "COMPLETED", nil
		})
		if err != nil {
			return nil, 0, err
		}
		raw, err = imageCall(ctx, "fal", http.MethodGet, job.ResponseURL, f.header(), nil, f.key)
		if err != nil {
			return nil, 0, err
		}
		var out struct {
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
			Image *struct {
				URL string `json:"url"`
			} `json:"image"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, 0, fmt.Errorf("fal: the answer could not be read: %w", err)
		}
		var urls []string
		for _, i := range out.Images {
			urls = append(urls, i.URL)
		}
		if out.Image != nil {
			urls = append(urls, out.Image.URL)
		}
		imgs, err := fetchImages(ctx, urls, nil)
		return imgs, 0, err
	})
}

// ListModels is fal's active text-to-image and image-to-image endpoints.
func (f *fal) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	var out []ModelInfo
	seen := map[string]bool{}
	for _, category := range []string{"text-to-image", "image-to-image"} {
		cursor := ""
		for page := 0; page < 5; page++ {
			q := url.Values{"category": {category}, "status": {"active"}, "limit": {"100"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			raw, err := imageCall(ctx, "fal", http.MethodGet, f.api+"/v1/models?"+q.Encode(), f.header(), nil, f.key)
			if err != nil {
				if len(out) > 0 {
					break
				}
				return nil, err
			}
			var body struct {
				Models []struct {
					ID       string `json:"endpoint_id"`
					Metadata struct {
						Name string `json:"display_name"`
					} `json:"metadata"`
				} `json:"models"`
				Next string `json:"next_cursor"`
				More bool   `json:"has_more"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				return nil, fmt.Errorf("fal: %w", err)
			}
			for _, m := range body.Models {
				if m.ID != "" && !seen[m.ID] {
					seen[m.ID] = true
					out = append(out, ModelInfo{ID: m.ID, Name: m.Metadata.Name, Kind: KindImage})
				}
			}
			if !body.More || body.Next == "" {
				break
			}
			cursor = body.Next
		}
	}
	sortModels(out)
	return out, nil
}

// --- Replicate ---

const replicateBase = "https://api.replicate.com"

// replicate runs owner/name models. Every model names its own inputs, so the
// model's schema is read first (and kept) to learn what it calls the shape,
// the count and the pictures to work from.
type replicate struct {
	imageVendor
	mu      sync.Mutex
	schemas map[string]replicateInputs
}

// replicateInputs are a model's input fields: the name and whether it is a
// list.
type replicateInputs map[string]bool

func newReplicate(s Settings) (Client, error) {
	v, err := newImageVendor("Replicate", "replicate", replicateBase, s)
	if err != nil {
		return nil, err
	}
	return &replicate{imageVendor: v, schemas: map[string]replicateInputs{}}, nil
}

func (r *replicate) header() http.Header { return bearer(r.key) }

// replicateRefs are the names models give the pictures to work from, the
// most telling first.
var replicateRefs = []string{"image_input", "input_images", "image_urls", "reference_images", "images", "input_image", "image", "image_url"}

// inputs reads a model's input schema.
func (r *replicate) inputs(ctx context.Context, model string) (replicateInputs, error) {
	r.mu.Lock()
	got, ok := r.schemas[model]
	r.mu.Unlock()
	if ok {
		return got, nil
	}
	raw, err := imageCall(ctx, "replicate", http.MethodGet, r.base+"/v1/models/"+model, r.header(), nil, r.key)
	if err != nil {
		return nil, err
	}
	var body struct {
		Latest struct {
			Schema struct {
				Components struct {
					Schemas struct {
						Input struct {
							Properties map[string]struct {
								Type string `json:"type"`
							} `json:"properties"`
						} `json:"Input"`
					} `json:"schemas"`
				} `json:"components"`
			} `json:"openapi_schema"`
		} `json:"latest_version"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("replicate: %w", err)
	}
	got = replicateInputs{}
	for name, p := range body.Latest.Schema.Components.Schemas.Input.Properties {
		got[name] = p.Type == "array"
	}
	r.mu.Lock()
	r.schemas[model] = got
	r.mu.Unlock()
	return got, nil
}

// upload stores a picture with Replicate and answers its address: a data URL
// is taken only up to 256 KB.
func (r *replicate) upload(ctx context.Context, img Image) (string, error) {
	if len(img.Data) <= 180<<10 {
		return dataURL(img), nil
	}
	form, ctype, err := imageForm(nil, func(int) string { return "content" }, []Image{img})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/v1/files", form)
	if err != nil {
		return "", err
	}
	req.Header = r.header()
	req.Header.Set("Content-Type", ctype)
	raw, err := imageDo("replicate", req, r.key)
	if err != nil {
		return "", err
	}
	var f struct {
		URLs struct {
			Get string `json:"get"`
		} `json:"urls"`
	}
	if json.Unmarshal(raw, &f) != nil || f.URLs.Get == "" {
		return "", errors.New("replicate: the input image could not be uploaded")
	}
	return f.URLs.Get, nil
}

func (r *replicate) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	model = strings.Trim(model, "/")
	if strings.Count(model, "/") != 1 {
		return ImageResult{}, fmt.Errorf("replicate: name the model as owner/name, not %q", model)
	}
	fields, err := r.inputs(ctx, model)
	if err != nil {
		return ImageResult{}, err
	}
	input := map[string]any{"prompt": req.Prompt}
	if _, ok := fields["aspect_ratio"]; ok && req.Aspect != "" {
		input["aspect_ratio"] = req.Aspect
	}
	if len(req.Inputs) > 0 {
		field, list, found := "", false, false
		for _, name := range replicateRefs {
			if l, ok := fields[name]; ok {
				field, list, found = name, l, true
				break
			}
		}
		if !found {
			return ImageResult{}, noInputs("replicate/"+model, req)
		}
		if !list && len(req.Inputs) > 1 {
			return ImageResult{}, fmt.Errorf("replicate/%s takes one input image, not %d", model, len(req.Inputs))
		}
		urls := make([]string, 0, len(req.Inputs))
		for _, img := range req.Inputs {
			u, err := r.upload(ctx, img)
			if err != nil {
				return ImageResult{}, err
			}
			urls = append(urls, u)
		}
		if list {
			input[field] = urls
		} else {
			input[field] = urls[0]
		}
	}
	return many(ctx, req.Count, func(ctx context.Context) ([]Image, float64, error) {
		head := r.header()
		head.Set("Prefer", "wait")
		raw, err := imageCall(ctx, "replicate", http.MethodPost, r.base+"/v1/models/"+model+"/predictions", head, map[string]any{"input": input}, r.key)
		if err != nil {
			return nil, 0, err
		}
		type prediction struct {
			Status string `json:"status"`
			Output any    `json:"output"`
			Error  any    `json:"error"`
			URLs   struct {
				Get string `json:"get"`
			} `json:"urls"`
		}
		var p prediction
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, 0, fmt.Errorf("replicate: the answer could not be read: %w", err)
		}
		ended := func(p prediction) (bool, error) {
			switch p.Status {
			case "succeeded":
				return true, nil
			case "failed", "canceled":
				if p.Error != nil {
					return false, fmt.Errorf("replicate: %v", textOf(p.Error))
				}
				return false, fmt.Errorf("replicate: the prediction %s", p.Status)
			}
			return false, nil
		}
		done, err := ended(p)
		if err != nil {
			return nil, 0, err
		}
		if !done {
			if p.URLs.Get == "" {
				return nil, 0, errors.New("replicate: the prediction was not started")
			}
			err = imagePoll(ctx, func() (bool, error) {
				raw, err := imageCall(ctx, "replicate", http.MethodGet, p.URLs.Get, r.header(), nil, r.key)
				if err != nil {
					return false, err
				}
				get := p.URLs.Get
				p = prediction{}
				if err := json.Unmarshal(raw, &p); err != nil {
					return false, fmt.Errorf("replicate: %w", err)
				}
				p.URLs.Get = get
				return ended(p)
			})
			if err != nil {
				return nil, 0, err
			}
		}
		var urls []string
		switch o := p.Output.(type) {
		case string:
			urls = []string{o}
		case []any:
			for _, e := range o {
				if s, ok := e.(string); ok {
					urls = append(urls, s)
				}
			}
		}
		imgs, err := r.fetch(ctx, urls)
		return imgs, 0, err
	})
}

// fetch downloads output files; Replicate's own hosts want the token.
func (r *replicate) fetch(ctx context.Context, urls []string) ([]Image, error) {
	out := make([]Image, 0, len(urls))
	for _, u := range urls {
		var head http.Header
		if p, err := url.Parse(u); err == nil {
			own, _ := url.Parse(r.base)
			host := p.Hostname()
			if host == "replicate.com" || strings.HasSuffix(host, ".replicate.com") || (own != nil && host == own.Hostname()) {
				head = r.header()
			}
		}
		img, err := fetchImage(ctx, u, head)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	if len(out) == 0 {
		return nil, errors.New("replicate: the model answered with no image")
	}
	return out, nil
}

// ListModels is Replicate's text-to-image collection.
func (r *replicate) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	raw, err := imageCall(ctx, "replicate", http.MethodGet, r.base+"/v1/collections/text-to-image", r.header(), nil, r.key)
	if err != nil {
		return nil, err
	}
	var body struct {
		Models []struct {
			Owner string `json:"owner"`
			Name  string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("replicate: %w", err)
	}
	out := make([]ModelInfo, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Owner != "" && m.Name != "" {
			out = append(out, ModelInfo{ID: m.Owner + "/" + m.Name, Kind: KindImage})
		}
	}
	return out, nil
}

// --- Black Forest Labs ---

const bflBase = "https://api.bfl.ai"

// bfl is FLUX from its maker: a model is an endpoint, a request is a job
// polled at the address the answer names, and one request makes one picture.
type bfl struct{ imageVendor }

func newBFL(s Settings) (Client, error) {
	v, err := newImageVendor("Black Forest Labs", "bfl", bflBase, s)
	if err != nil {
		return nil, err
	}
	return &bfl{v}, nil
}

func (b *bfl) header() http.Header { return http.Header{"x-key": {b.key}} }

// bflBody is a request in the form the model's family takes: FLUX 3 a list of
// images and an aspect, Kontext numbered images and an aspect, FLUX.2
// numbered images and a width and height, the older ones a width and height
// and no pictures.
func bflBody(model string, req ImageRequest) (map[string]any, error) {
	body := map[string]any{"prompt": req.Prompt}
	refs := make([]string, 0, len(req.Inputs))
	for _, img := range req.Inputs {
		refs = append(refs, base64.StdEncoding.EncodeToString(img.Data))
	}
	numbered := func(max int) error {
		if len(refs) > max {
			return fmt.Errorf("bfl/%s takes at most %d input images, not %d", model, max, len(refs))
		}
		for i, r := range refs {
			name := "input_image"
			if i > 0 {
				name = fmt.Sprintf("input_image_%d", i+1)
			}
			body[name] = r
		}
		return nil
	}
	pixels := func() {
		if p, ok := aspectPixels[req.Aspect]; ok {
			body["width"], body["height"] = p[0], p[1]
		}
	}
	switch {
	case strings.HasPrefix(model, "flux-3"):
		if len(refs) > 0 {
			body["images"] = refs
		}
		if req.Aspect != "" {
			body["aspect_ratio"] = req.Aspect
		}
	case strings.Contains(model, "kontext"):
		if err := numbered(4); err != nil {
			return nil, err
		}
		if req.Aspect != "" {
			body["aspect_ratio"] = req.Aspect
		}
		body["output_format"] = "png"
	case strings.HasPrefix(model, "flux-2"):
		if err := numbered(8); err != nil {
			return nil, err
		}
		pixels()
		body["output_format"] = "png"
	default:
		if err := noInputs("bfl/"+model, req); err != nil {
			return nil, err
		}
		pixels()
		body["output_format"] = "png"
	}
	return body, nil
}

func (b *bfl) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	body, err := bflBody(model, req)
	if err != nil {
		return ImageResult{}, err
	}
	return many(ctx, req.Count, func(ctx context.Context) ([]Image, float64, error) {
		raw, err := imageCall(ctx, "bfl", http.MethodPost, b.base+"/v1/"+model, b.header(), body, b.key)
		if err != nil {
			return nil, 0, err
		}
		var job struct {
			PollingURL string `json:"polling_url"`
		}
		if json.Unmarshal(raw, &job) != nil || job.PollingURL == "" {
			return nil, 0, errors.New("bfl: the request was not accepted")
		}
		sample := ""
		err = imagePoll(ctx, func() (bool, error) {
			raw, err := imageCall(ctx, "bfl", http.MethodGet, job.PollingURL, b.header(), nil, b.key)
			if err != nil {
				return false, err
			}
			var st struct {
				Status string `json:"status"`
				Result struct {
					Sample string `json:"sample"`
				} `json:"result"`
			}
			if err := json.Unmarshal(raw, &st); err != nil {
				return false, fmt.Errorf("bfl: %w", err)
			}
			switch st.Status {
			case "Ready":
				sample = st.Result.Sample
				return true, nil
			case "Pending", "Reasoning", "Generating", "Queued", "Processing", "":
				return false, nil
			case "Request Moderated", "Content Moderated":
				return false, errors.New("bfl: refused by the content filter (" + st.Status + ")")
			}
			return false, errors.New("bfl: " + st.Status)
		})
		if err != nil {
			return nil, 0, err
		}
		// The result is a signed link that expires in minutes: no key goes
		// with it.
		img, err := fetchImage(ctx, sample, nil)
		if err != nil {
			return nil, 0, err
		}
		return []Image{img}, 0, nil
	})
}

// ListModels is BFL's current endpoints (no list endpoint).
func (b *bfl) ListModels(context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "flux-3-image", Name: "FLUX 3", Kind: KindImage},
		{ID: "flux-2-max", Name: "FLUX.2 [max]", Kind: KindImage},
		{ID: "flux-2-pro", Name: "FLUX.2 [pro]", Kind: KindImage},
		{ID: "flux-2-flex", Name: "FLUX.2 [flex]", Kind: KindImage},
		{ID: "flux-kontext-max", Name: "FLUX.1 Kontext [max] (edits)", Kind: KindImage},
		{ID: "flux-kontext-pro", Name: "FLUX.1 Kontext [pro] (edits)", Kind: KindImage},
	}, nil
}

// --- Stability AI ---

const stabilityBase = "https://api.stability.ai"

// stability is Stable Image: a multipart form in, the picture's bytes out,
// one picture a request.
type stability struct{ imageVendor }

func newStability(s Settings) (Client, error) {
	v, err := newImageVendor("Stability AI", "stability", stabilityBase, s)
	if err != nil {
		return nil, err
	}
	return &stability{v}, nil
}

// stabilityAspects are the shapes Stable Image has; 4:3 and 3:4 are not
// among them.
var stabilityAspects = map[string]string{
	"1:1": "1:1", "3:2": "3:2", "2:3": "2:3", "4:3": "5:4", "3:4": "4:5", "16:9": "16:9", "9:16": "9:16", "21:9": "21:9",
}

func (s *stability) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	// ultra and core are services; anything else is an SD3 model.
	service := model
	fields := map[string]string{"prompt": req.Prompt, "output_format": "png"}
	if model != "ultra" && model != "core" {
		service = "sd3"
		fields["model"] = model
	}
	if len(req.Inputs) > 1 {
		return ImageResult{}, fmt.Errorf("stability/%s takes one input image, not %d", model, len(req.Inputs))
	}
	if len(req.Inputs) == 1 {
		if service == "core" {
			return ImageResult{}, noInputs("stability/core", req)
		}
		// How far the result may move from the input: mostly redrawn, still
		// recognisably it.
		fields["strength"] = "0.7"
		if service == "sd3" {
			fields["mode"] = "image-to-image"
		}
	}
	// SD3 takes a shape only with no picture to start from.
	if req.Aspect != "" && !(service == "sd3" && len(req.Inputs) == 1) {
		fields["aspect_ratio"] = stabilityAspects[req.Aspect]
	}
	return many(ctx, req.Count, func(ctx context.Context) ([]Image, float64, error) {
		form, ctype, err := imageForm(fields, func(int) string { return "image" }, req.Inputs)
		if err != nil {
			return nil, 0, err
		}
		hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/v2beta/stable-image/generate/"+service, form)
		if err != nil {
			return nil, 0, err
		}
		hreq.Header = bearer(s.key)
		hreq.Header.Set("Content-Type", ctype)
		hreq.Header.Set("Accept", "image/*")
		raw, err := imageDo("stability", hreq, s.key)
		if err != nil {
			return nil, 0, err
		}
		img, err := imageOf(raw)
		if err != nil {
			return nil, 0, err
		}
		return []Image{img}, 0, nil
	})
}

// ListModels is Stable Image's services and SD3.5 models (no list endpoint).
func (s *stability) ListModels(context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "ultra", Name: "Stable Image Ultra", Kind: KindImage},
		{ID: "core", Name: "Stable Image Core (no input images)", Kind: KindImage},
		{ID: "sd3.5-large", Name: "Stable Diffusion 3.5 Large", Kind: KindImage},
		{ID: "sd3.5-large-turbo", Name: "Stable Diffusion 3.5 Large Turbo", Kind: KindImage},
		{ID: "sd3.5-medium", Name: "Stable Diffusion 3.5 Medium", Kind: KindImage},
	}, nil
}

// --- Google (Gemini API) ---

const googleBase = "https://generativelanguage.googleapis.com/v1beta"

// google is the Gemini API's image models (Nano Banana) through
// generateContent: the prompt and pictures as parts in, pictures as parts
// out, one request a picture.
type google struct{ imageVendor }

func newGoogle(s Settings) (Client, error) {
	v, err := newImageVendor("Google", "google", googleBase, s)
	if err != nil {
		return nil, err
	}
	return &google{v}, nil
}

func (g *google) header() http.Header { return http.Header{"x-goog-api-key": {g.key}} }

func (g *google) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	parts := []map[string]any{{"text": req.Prompt}}
	for _, img := range req.Inputs {
		parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": img.Mime, "data": base64.StdEncoding.EncodeToString(img.Data)}})
	}
	config := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	if req.Aspect != "" {
		config["imageConfig"] = map[string]any{"aspectRatio": req.Aspect}
	}
	body := map[string]any{"contents": []map[string]any{{"role": "user", "parts": parts}}, "generationConfig": config}
	model = strings.TrimPrefix(model, "models/")
	return many(ctx, req.Count, func(ctx context.Context) ([]Image, float64, error) {
		raw, err := imageCall(ctx, "google", http.MethodPost, g.base+"/models/"+model+":generateContent", g.header(), body, g.key)
		if err != nil {
			return nil, 0, err
		}
		var out struct {
			Candidates []struct {
				FinishReason string `json:"finishReason"`
				Content      struct {
					Parts []struct {
						Text   string `json:"text"`
						Inline *struct {
							Data string `json:"data"`
						} `json:"inlineData"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Feedback struct {
				BlockReason string `json:"blockReason"`
			} `json:"promptFeedback"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, 0, fmt.Errorf("google: the answer could not be read: %w", err)
		}
		var imgs []Image
		said, reason := "", out.Feedback.BlockReason
		for _, c := range out.Candidates {
			if reason == "" && c.FinishReason != "" && c.FinishReason != "STOP" {
				reason = c.FinishReason
			}
			for _, p := range c.Content.Parts {
				if p.Inline != nil && p.Inline.Data != "" {
					img, err := imageFromB64(p.Inline.Data)
					if err != nil {
						return nil, 0, err
					}
					imgs = append(imgs, img)
				} else if said == "" {
					said = strings.TrimSpace(p.Text)
				}
			}
		}
		if len(imgs) == 0 {
			// The model answers in words when it will not draw.
			switch {
			case reason != "":
				return nil, 0, errors.New("google: no image (" + reason + ")")
			case said != "":
				if len(said) > 300 {
					said = said[:300]
				}
				return nil, 0, errors.New("google: the model made no image and said: " + said)
			}
			return nil, 0, errors.New("google: the model answered with no image")
		}
		return imgs, 0, nil
	})
}

// ListModels is the models whose name says they make images: the listing has
// no field for it.
func (g *google) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	raw, err := imageCall(ctx, "google", http.MethodGet, g.base+"/models?pageSize=1000", g.header(), nil, g.key)
	if err != nil {
		return nil, err
	}
	var body struct {
		Models []struct {
			Name    string `json:"name"`
			Display string `json:"displayName"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("google: %w", err)
	}
	var out []ModelInfo
	for _, m := range body.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if low := strings.ToLower(id); strings.Contains(low, "image") || strings.Contains(low, "banana") {
			out = append(out, ModelInfo{ID: id, Name: m.Display, Kind: KindImage})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// --- xAI ---

const xaiBase = "https://api.x.ai/v1"

// xai is Grok Imagine: the OpenAI answer, with an aspect in place of a size
// and edits as JSON.
type xai struct{ imageVendor }

func newXAI(s Settings) (Client, error) {
	v, err := newImageVendor("xAI", "xai", xaiBase, s)
	if err != nil {
		return nil, err
	}
	return &xai{v}, nil
}

func (x *xai) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	body := map[string]any{"model": model, "prompt": req.Prompt, "n": req.Count, "response_format": "b64_json"}
	if req.Aspect != "" {
		body["aspect_ratio"] = req.Aspect
	}
	path := "/images/generations"
	if len(req.Inputs) > 1 {
		return ImageResult{}, fmt.Errorf("xai/%s takes one input image, not %d", model, len(req.Inputs))
	}
	if len(req.Inputs) == 1 {
		path = "/images/edits"
		body["image"] = map[string]any{"url": dataURL(req.Inputs[0]), "type": "image_url"}
	}
	raw, err := imageCall(ctx, "xai", http.MethodPost, x.base+path, bearer(x.key), body, x.key)
	if err != nil {
		return ImageResult{}, err
	}
	return readImageData(ctx, raw)
}

// ListModels is the image models among xAI's models, else the one known.
func (x *xai) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	var out []ModelInfo
	if list, err := fetchModels(ctx, x.base, x.key); err == nil {
		for _, m := range modelInfos(list, KindImage) {
			if strings.Contains(strings.ToLower(m.ID), "imag") {
				out = append(out, m)
			}
		}
	}
	if len(out) == 0 {
		out = []ModelInfo{{ID: "grok-imagine-image-2.0", Name: "Grok Imagine Image 2.0", Kind: KindImage}}
	}
	sortModels(out)
	return out, nil
}
