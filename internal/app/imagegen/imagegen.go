// Package imagegen is the Bot's generate_image tool: a prompt (and optionally
// pictures from the workspace) goes to one of the operator's image models,
// and what comes back is written into the Bot's workspace. Nothing is shown
// to anyone by it: the Bot presents a file when the human should see it.
package imagegen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	"silo.agent/internal/app/models"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/channels"
	"silo.agent/internal/config"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
)

// timeout bounds one request: a large model can take minutes, and a job a
// provider queues is polled until then.
const timeout = 5 * time.Minute

// dir is where a picture goes when the Bot names no path.
const dir = "images"

// Files is the part of the workspace the tool needs: read a picture to work
// from, write the ones that were made.
type Files interface {
	Attachment(ctx context.Context, botID, rel string) (channels.Attachment, error)
	Put(ctx context.Context, botID, path string, data []byte) error
}

// Service is image generation for Bots.
type Service struct {
	cfg    func() config.Config
	models *models.Service
	files  Files
}

func New(cfg func() config.Config, m *models.Service, files Files) *Service {
	return &Service{cfg: cfg, models: m, files: files}
}

// Enabled reports whether the operator picked a default image model, which
// is what turns the tool and its prompt section on.
func (s *Service) Enabled() bool { return s.cfg().ImageModel() != "" }

// allowed is the image models a Bot may name.
func (s *Service) allowed() []string { return s.cfg().KindModels(llm.KindImage) }

// Prompt fills the image section of the system prompt with the default model
// and the allowed ones; "" while there is none. It reads only the operator's
// config, so it is the same text for every Bot.
func (s *Service) Prompt(tmpl string) string {
	def := s.cfg().ImageModel()
	if def == "" {
		return ""
	}
	return strings.NewReplacer("{default}", def, "{models}", strings.Join(s.allowed(), ", ")).Replace(tmpl)
}

// Saved is one picture the tool wrote.
type Saved struct {
	Path  string `json:"path"`
	Mime  string `json:"mime"`
	Bytes int    `json:"bytes"`
}

// Tool is the Bot's generate_image (silo_runtime.generate_image, and the chat
// tool where an operator offers it). The caller already authorized
// model.image.
func (s *Service) Tool(ctx context.Context, botID string, args map[string]any, structured bool) (string, error) {
	cfg := s.cfg()
	modelID := cfg.ImageModel()
	if modelID == "" {
		return "", errors.New("the operator has not picked an image model")
	}
	if m, _ := args["model"].(string); strings.TrimSpace(m) != "" {
		modelID = strings.TrimSpace(m)
		if !llm.Allowed(modelID, s.allowed()) {
			return "", fmt.Errorf("model %q is not an allowed image model; allowed: %s", modelID, strings.Join(s.allowed(), ", "))
		}
	}
	req := llm.ImageRequest{Count: count(args["count"])}
	req.Prompt, _ = args["prompt"].(string)
	req.Aspect, _ = args["aspect"].(string)
	inputs, err := paths(args["images"])
	if err != nil {
		return "", err
	}
	if len(inputs) > llm.MaxImageInputs {
		return "", fmt.Errorf("%d input images; the cap is %d", len(inputs), llm.MaxImageInputs)
	}
	if err := req.Validate(); err != nil {
		return "", err
	}
	dest, _ := args["path"].(string)
	base, err := target(dest, req.Prompt)
	if err != nil {
		return "", err
	}
	for _, p := range inputs {
		file, err := s.files.Attachment(ctx, botID, p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		if !strings.HasPrefix(file.Mime, "image/") {
			return "", fmt.Errorf("%s is not an image", p)
		}
		req.Inputs = append(req.Inputs, llm.Image{Mime: file.Mime, Data: file.Data})
	}
	res, err := s.make(ctx, botID, modelID, req)
	if err != nil {
		return "", err
	}
	saved := make([]Saved, 0, len(res.Images))
	for i, img := range res.Images {
		p := base + llm.ImageExt(img.Mime)
		if len(res.Images) > 1 {
			p = fmt.Sprintf("%s-%d%s", base, i+1, llm.ImageExt(img.Mime))
		}
		if err := s.files.Put(ctx, botID, p, img.Data); err != nil {
			return "", fmt.Errorf("save %s: %w", p, err)
		}
		saved = append(saved, Saved{Path: p, Mime: img.Mime, Bytes: len(img.Data)})
	}
	if structured {
		out := map[string]any{"model": modelID, "images": saved}
		if res.Cost > 0 {
			out["cost"] = res.Cost
		}
		b, err := json.Marshal(out)
		return string(b), err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved %d image(s) made by %s:\n", len(saved), modelID)
	for _, f := range saved {
		fmt.Fprintf(&b, "- %s (%s, %d KB)\n", f.Path, f.Mime, (f.Bytes+1023)/1024)
	}
	b.WriteString("Nobody has seen them yet: present a file to look at it or to show it.")
	return b.String(), nil
}

// make asks the model and records the call.
func (s *Service) make(ctx context.Context, botID, modelID string, req llm.ImageRequest) (llm.ImageResult, error) {
	client, provider, model, err := s.models.Client(modelID)
	if err != nil {
		return llm.ImageResult{}, err
	}
	maker, ok := client.(llm.ImageMaker)
	if !ok {
		return llm.ImageResult{}, fmt.Errorf("%s has no image models", provider)
	}
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, fmt.Errorf("no answer in %s", timeout))
	defer cancel()
	start := time.Now()
	res, err := maker.MakeImages(ctx, model, req)
	if err == nil && len(res.Images) == 0 {
		err = errors.New("the model answered with no image")
	}
	asked := req.Prompt
	if n := len(req.Inputs); n > 0 {
		asked += fmt.Sprintf("\n\n[%d input image(s)]", n)
	}
	rec := llm.Record{
		Provider: provider, Model: model,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: asked}},
		Error:    err, Duration: time.Since(start),
	}
	if err == nil {
		size := 0
		for _, img := range res.Images {
			size += len(img.Data)
		}
		rec.Text = fmt.Sprintf("[%d image(s), %d bytes]", len(res.Images), size)
	}
	s.models.Record(ctx, botID, "image", rec)
	if err != nil {
		return llm.ImageResult{}, fmt.Errorf("generate image: %w", err)
	}
	return res, nil
}

// count reads the count argument: a number from the model or from Python.
func count(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// paths reads the images argument: a list of workspace paths, or one path.
func paths(v any) ([]string, error) {
	var out []string
	add := func(s string) {
		if s = workspace.Rel(s); s != "" {
			out = append(out, s)
		}
	}
	switch list := v.(type) {
	case nil:
	case string:
		add(list)
	case []any:
		for _, e := range list {
			s, ok := e.(string)
			if !ok {
				return nil, errors.New("images: a list of workspace paths")
			}
			add(s)
		}
	default:
		return nil, errors.New("images: a list of workspace paths")
	}
	return out, nil
}

// target is where the pictures go, without the extension (the model decides
// the format): the path the Bot gave, or images/<words of the prompt>-<id>.
func target(dest, prompt string) (string, error) {
	dest = workspace.Rel(dest)
	if dest == "" {
		return path.Join(dir, slug(prompt)+"-"+ids.New()[:6]), nil
	}
	for _, part := range strings.Split(dest, "/") {
		if part == ".." {
			return "", errors.New("path must stay inside the workspace")
		}
	}
	if strings.HasSuffix(dest, "/") {
		return path.Join(dest, slug(prompt)+"-"+ids.New()[:6]), nil
	}
	dest = path.Clean(dest)
	switch strings.ToLower(path.Ext(dest)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg":
		dest = strings.TrimSuffix(dest, path.Ext(dest))
	}
	if dest == "" || dest == "." || strings.HasSuffix(dest, "/") {
		return "", errors.New("path needs a file name")
	}
	return dest, nil
}

// slug is the first words of a prompt as a file name.
func slug(prompt string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(prompt) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	if b.Len() == 0 {
		return "image"
	}
	return b.String()
}
