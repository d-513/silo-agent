package llm

// Image models: a prompt (and optionally pictures to work from) in, pictures
// out. The diffusion APIs differ in everything but that, so the request is
// kept to what they all take and each provider maps it itself.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// KindImage makes pictures from a prompt (ImageMaker).
const KindImage = "image"

// The bounds of one image request.
const (
	// MaxImages is how many pictures one request may ask for.
	MaxImages = 4
	// MaxImageInputs is how many pictures a request may be given to work from.
	MaxImageInputs = 8
	// MaxImageBytes caps one picture, in or out.
	MaxImageBytes = 32 << 20
	// MaxImagePrompt caps the prompt, in characters.
	MaxImagePrompt = 32000
)

// ImageAspects are the shapes a request may ask for, as width:height. A
// provider that has no such shape takes the nearest it has.
var ImageAspects = []string{"1:1", "3:2", "2:3", "4:3", "3:4", "16:9", "9:16", "21:9"}

// ImageRequest is one request for pictures.
type ImageRequest struct {
	Prompt string
	// Inputs are pictures to edit or build on; a model that takes none
	// refuses them.
	Inputs []Image
	// Aspect is one of ImageAspects; empty is the model's own default.
	Aspect string
	// Count is how many pictures; 0 means one.
	Count int
}

// ImageResult is what a request made.
type ImageResult struct {
	Images []Image
	// Cost is in USD when the provider reports it.
	Cost float64
}

// ImageMaker is the optional interface a provider implements when it has
// image models.
type ImageMaker interface {
	MakeImages(ctx context.Context, model string, req ImageRequest) (ImageResult, error)
}

// Validate refuses a request no provider could serve, in words the caller can
// act on, and fills the count.
func (r *ImageRequest) Validate() error {
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" {
		return errors.New("prompt is empty: describe the image")
	}
	if n := len([]rune(r.Prompt)); n > MaxImagePrompt {
		return fmt.Errorf("prompt is %d characters; the cap is %d", n, MaxImagePrompt)
	}
	if r.Count <= 0 {
		r.Count = 1
	}
	if r.Count > MaxImages {
		return fmt.Errorf("count is %d; one request makes at most %d images", r.Count, MaxImages)
	}
	if len(r.Inputs) > MaxImageInputs {
		return fmt.Errorf("%d input images; the cap is %d", len(r.Inputs), MaxImageInputs)
	}
	r.Aspect = strings.TrimSpace(r.Aspect)
	if r.Aspect != "" {
		ok := false
		for _, a := range ImageAspects {
			ok = ok || a == r.Aspect
		}
		if !ok {
			return fmt.Errorf("aspect %q is not one of %s", r.Aspect, strings.Join(ImageAspects, ", "))
		}
	}
	return nil
}

// imageOnly is the text side of a provider that has only image models.
type imageOnly struct{ name string }

func (s imageOnly) Stream(context.Context, Request) (Stream, error) {
	return nil, fmt.Errorf("%s has no text models here; pick it for images", s.name)
}

func (s imageOnly) Complete(context.Context, Request) (Response, error) {
	return Response{}, fmt.Errorf("%s has no text models here; pick it for images", s.name)
}

// aspectPick maps an aspect to a provider's own word for it; def answers for
// an empty or unknown one.
func aspectPick(aspect string, table map[string]string, def string) string {
	if v, ok := table[aspect]; ok {
		return v
	}
	return def
}

// imageMIME is a picture's type by its bytes; "" when they are not a picture.
func imageMIME(data []byte) string {
	mt := http.DetectContentType(data)
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = mt[:i]
	}
	if strings.HasPrefix(mt, "image/") {
		return mt
	}
	// An SVG is text to the sniffer.
	head := strings.ToLower(string(data[:min(len(data), 256)]))
	if strings.Contains(head, "<svg") {
		return "image/svg+xml"
	}
	return ""
}

// ImageExt is the file extension of an image type.
func ImageExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	}
	return ".png"
}

// imageOf is a picture from its bytes, refused when they are not one.
func imageOf(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, errors.New("the provider sent an empty image")
	}
	mt := imageMIME(data)
	if mt == "" {
		return Image{}, errors.New("the provider sent something that is not an image")
	}
	return Image{Mime: mt, Data: data}, nil
}

// imageFromB64 reads a picture a provider sent as base64.
func imageFromB64(s string) (Image, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		if data, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(s)); err != nil {
			return Image{}, errors.New("the provider sent an image that is not base64")
		}
	}
	return imageOf(data)
}

// fetchImage gets a picture a provider answered with as an address: a data:
// URL, or one to download. header goes with the download only where the
// provider's own host serves it; a signed link on a storage host gets none.
func fetchImage(ctx context.Context, url string, header http.Header) (Image, error) {
	url = strings.TrimSpace(url)
	if rest, ok := strings.CutPrefix(url, "data:"); ok {
		i := strings.Index(rest, ",")
		if i < 0 || !strings.Contains(rest[:i], "base64") {
			return Image{}, errors.New("the provider sent an image in a form that cannot be read")
		}
		return imageFromB64(rest[i+1:])
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return Image{}, errors.New("the provider sent no image address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Image{}, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("download the image: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("download the image: http %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxImageBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("download the image: %w", err)
	}
	if len(data) > MaxImageBytes {
		return Image{}, fmt.Errorf("the image is over %d MB", MaxImageBytes>>20)
	}
	return imageOf(data)
}

// fetchImages gets each address in turn.
func fetchImages(ctx context.Context, urls []string, header http.Header) ([]Image, error) {
	out := make([]Image, 0, len(urls))
	for _, u := range urls {
		img, err := fetchImage(ctx, u, header)
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

// imageCall is one JSON request of an image API: the body in, the answer's
// bytes out, a refusal worded by vendorError.
func imageCall(ctx context.Context, name, method, url string, header http.Header, body any, key string) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = jsonBody(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return imageDo(name, req, key)
}

// imageDo sends a request and reads the whole answer.
func imageDo(name string, req *http.Request, key string) ([]byte, error) {
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, vendorError(name, res, key)
	}
	// Pictures come as base64 inside JSON: four of them, a third larger each.
	data, err := io.ReadAll(io.LimitReader(res.Body, 8*MaxImageBytes))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return data, nil
}

// imageForm builds a multipart body: plain fields, then the pictures, each
// under the name files gives it.
func imageForm(fields map[string]string, files func(i int) string, inputs []Image) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	for i, img := range inputs {
		mt := img.Mime
		if mt == "" {
			mt = "image/png"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="image%d%s"`, files(i), i+1, ImageExt(mt)))
		h.Set("Content-Type", mt)
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(img.Data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

// The pace of polling a job a provider runs in the background.
var (
	imagePollFirst = 700 * time.Millisecond
	imagePollMax   = 3 * time.Second
)

// imagePoll asks check until it says done, waiting a little longer each time;
// the caller's context bounds the whole wait.
func imagePoll(ctx context.Context, check func() (done bool, err error)) error {
	wait := imagePollFirst
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("the image was not ready in time: %w", context.Cause(ctx))
		case <-time.After(wait):
		}
		done, err := check()
		if err != nil || done {
			return err
		}
		if wait = wait * 3 / 2; wait > imagePollMax {
			wait = imagePollMax
		}
	}
}

// many runs one request per picture for an API that makes one at a time,
// all at once, and joins what came.
func many(ctx context.Context, count int, one func(ctx context.Context) ([]Image, float64, error)) (ImageResult, error) {
	if count <= 1 {
		imgs, cost, err := one(ctx)
		return ImageResult{Images: imgs, Cost: cost}, err
	}
	type answer struct {
		imgs []Image
		cost float64
		err  error
	}
	answers := make(chan answer, count)
	for range count {
		go func() {
			imgs, cost, err := one(ctx)
			answers <- answer{imgs, cost, err}
		}()
	}
	var out ImageResult
	var first error
	for range count {
		a := <-answers
		if a.err != nil {
			if first == nil {
				first = a.err
			}
			continue
		}
		out.Images = append(out.Images, a.imgs...)
		out.Cost += a.cost
	}
	// Some of them made is an answer; the caller is told how many came.
	if len(out.Images) == 0 {
		return ImageResult{}, first
	}
	return out, nil
}

// aspectPixels is each aspect at about a megapixel, both sides a multiple of
// 16: what an API that takes a width and a height is asked for.
var aspectPixels = map[string][2]int{
	"1:1": {1024, 1024}, "3:2": {1248, 832}, "2:3": {832, 1248}, "4:3": {1152, 864}, "3:4": {864, 1152},
	"16:9": {1344, 768}, "9:16": {768, 1344}, "21:9": {1536, 656},
}

// wide and tall say which way an aspect leans, for an API with three shapes.
func wide(aspect string) bool {
	p, ok := aspectPixels[aspect]
	return ok && p[0] > p[1]
}

func tall(aspect string) bool {
	p, ok := aspectPixels[aspect]
	return ok && p[1] > p[0]
}

// noInputs refuses pictures to work from, for a model that takes none.
func noInputs(name string, req ImageRequest) error {
	if len(req.Inputs) > 0 {
		return fmt.Errorf("%s takes no input images; leave images out, or name a model that edits", name)
	}
	return nil
}
