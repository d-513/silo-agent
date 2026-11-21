package llm

// Images in the OpenAI shape (POST /images/generations, /images/edits):
// OpenAI itself, a self-hosted server that follows it (LocalAI, and others),
// and OpenRouter's own Images API, which is the same answer under /images.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// imageData is the answer every OpenAI-shaped image API gives: each picture
// as base64 or as an address.
type imageData struct {
	Data []struct {
		B64 string `json:"b64_json"`
		URL string `json:"url"`
	} `json:"data"`
	Usage struct {
		Cost float64 `json:"cost"`
	} `json:"usage"`
}

// images reads the pictures out of an answer.
func (d imageData) images(ctx context.Context) ([]Image, error) {
	out := make([]Image, 0, len(d.Data))
	for _, e := range d.Data {
		var img Image
		var err error
		switch {
		case e.B64 != "":
			img, err = imageFromB64(e.B64)
		case e.URL != "":
			img, err = fetchImage(ctx, e.URL, nil)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	if len(out) == 0 {
		return nil, errors.New("the provider answered with no image")
	}
	return out, nil
}

// bearer is the Authorization header of a key, and nothing for a server that
// checks none.
func bearer(key string) http.Header {
	h := http.Header{}
	if key != "" && key != "none" {
		h.Set("Authorization", "Bearer "+key)
	}
	return h
}

func (c *openAICompatClient) imageHeader() http.Header {
	h := bearer(c.key)
	for k, v := range c.headers {
		h.Set(k, v)
	}
	return h
}

// openAISize is the size OpenAI's image models take for an aspect: any
// multiple of 16 from gpt-image-2 on, three fixed shapes before, DALL-E 3's
// own; "" leaves it to the model.
func openAISize(model, aspect string) string {
	if aspect == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(model, "gpt-image-1"), strings.HasPrefix(model, "chatgpt-image"), strings.HasPrefix(model, "dall-e-2"):
		switch {
		case strings.HasPrefix(model, "dall-e-2"):
			return "1024x1024"
		case wide(aspect):
			return "1536x1024"
		case tall(aspect):
			return "1024x1536"
		}
		return "1024x1024"
	case strings.HasPrefix(model, "dall-e-3"):
		switch {
		case wide(aspect):
			return "1792x1024"
		case tall(aspect):
			return "1024x1792"
		}
		return "1024x1024"
	}
	p := aspectPixels[aspect]
	return fmt.Sprintf("%dx%d", p[0], p[1])
}

// MakeImages asks /images/generations, or /images/edits when there are
// pictures to work from.
func (c *openAICompatClient) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	base := strings.TrimRight(c.base, "/")
	// OpenAI's GPT image models always answer base64 and refuse the legacy
	// response_format; everything else is asked for base64.
	gpt := c.openAI && !strings.HasPrefix(model, "dall-e")
	size := openAISize(model, req.Aspect)
	var raw []byte
	var err error
	switch {
	case len(req.Inputs) == 0:
		body := map[string]any{"model": model, "prompt": req.Prompt, "n": req.Count}
		if size != "" {
			body["size"] = size
		}
		if !gpt {
			body["response_format"] = "b64_json"
		}
		raw, err = imageCall(ctx, "images", http.MethodPost, base+"/images/generations", c.imageHeader(), body, c.key)
	case c.openAI:
		refs := make([]map[string]any, 0, len(req.Inputs))
		for _, img := range req.Inputs {
			refs = append(refs, map[string]any{"image_url": dataURL(img)})
		}
		body := map[string]any{"model": model, "prompt": req.Prompt, "n": req.Count, "images": refs}
		if size != "" {
			body["size"] = size
		}
		raw, err = imageCall(ctx, "images", http.MethodPost, base+"/images/edits", c.imageHeader(), body, c.key)
	default:
		// A self-hosted server takes the older multipart form.
		fields := map[string]string{"model": model, "prompt": req.Prompt, "n": fmt.Sprint(req.Count), "size": size, "response_format": "b64_json"}
		name := func(int) string { return "image[]" }
		if len(req.Inputs) == 1 {
			name = func(int) string { return "image" }
		}
		form, ctype, ferr := imageForm(fields, name, req.Inputs)
		if ferr != nil {
			return ImageResult{}, ferr
		}
		hreq, rerr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/images/edits", form)
		if rerr != nil {
			return ImageResult{}, rerr
		}
		hreq.Header = c.imageHeader()
		hreq.Header.Set("Content-Type", ctype)
		raw, err = imageDo("images", hreq, c.key)
	}
	if err != nil {
		return ImageResult{}, err
	}
	return readImageData(ctx, raw)
}

func readImageData(ctx context.Context, raw []byte) (ImageResult, error) {
	var d imageData
	if err := json.Unmarshal(raw, &d); err != nil {
		return ImageResult{}, fmt.Errorf("images: the answer could not be read: %w", err)
	}
	imgs, err := d.images(ctx)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{Images: imgs, Cost: d.Usage.Cost}, nil
}

// MakeImages asks OpenRouter's Images API (POST /images): one shape for
// every upstream, which clamps the aspect to what it has.
func (c *openRouterClient) MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error) {
	body := map[string]any{"model": model, "prompt": req.Prompt, "response_format": "b64_json"}
	if req.Count > 1 {
		body["n"] = req.Count
	}
	if req.Aspect != "" {
		body["aspect_ratio"] = req.Aspect
	}
	if len(req.Inputs) > 0 {
		refs := make([]map[string]any, 0, len(req.Inputs))
		for _, img := range req.Inputs {
			refs = append(refs, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(img)}})
		}
		body["input_references"] = refs
	}
	raw, err := imageCall(ctx, "images", http.MethodPost, strings.TrimRight(c.base, "/")+"/images", c.imageHeader(), body, c.key)
	if err != nil {
		return ImageResult{}, err
	}
	return readImageData(ctx, raw)
}
