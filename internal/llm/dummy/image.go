package dummy

import (
	"bytes"
	"context"
	"errors"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"strings"

	"silo.agent/internal/llm"
)

// ImageModel is the conventional image model of the dummy provider.
const ImageModel = "paint"

var painted []llm.ImageRequest

// Painted is every image request the dummy has served, oldest first.
func Painted() []llm.ImageRequest {
	mu.Lock()
	defer mu.Unlock()
	return append([]llm.ImageRequest(nil), painted...)
}

// MakeImages answers with small PNGs whose colour comes from the prompt, so
// the same prompt paints the same bytes. A prompt holding Test_Image_Error is
// refused.
func (c *client) MakeImages(_ context.Context, _ string, req llm.ImageRequest) (llm.ImageResult, error) {
	if strings.Contains(req.Prompt, "Test_Image_Error") {
		return llm.ImageResult{}, errors.New("dummy: http 400: the dummy refuses to paint that")
	}
	mu.Lock()
	painted = append(painted, req)
	mu.Unlock()
	out := llm.ImageResult{Cost: 0.01 * float64(max(req.Count, 1))}
	for i := range max(req.Count, 1) {
		out.Images = append(out.Images, llm.Image{Mime: "image/png", Data: Swatch(req.Prompt, i)})
	}
	return out, nil
}

// Swatch is the PNG the dummy paints for a prompt's n-th picture.
func Swatch(prompt string, n int) []byte {
	h := fnv.New32a()
	h.Write([]byte(prompt))
	sum := h.Sum32() + uint32(n)*2654435761
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	fill := color.RGBA{uint8(sum), uint8(sum >> 8), uint8(sum >> 16), 255}
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
