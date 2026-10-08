package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ModelInfo is one model a provider offers. ID is the bare model name (no
// provider prefix); Name and ContextWindow are filled when the provider
// reports them.
type ModelInfo struct {
	ID            string
	Name          string
	ContextWindow int
	// Created is when the provider released the model; zero when unknown.
	Created time.Time
}

// ModelLister is the optional interface a provider implements when it can
// list the models its key may call. The OpenAI-compatible providers all share
// GET {base}/models; Anthropic has its own Models API.
type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// listTimeout bounds one whole listing (Anthropic's is paged).
const listTimeout = 20 * time.Second

// listedModel is one entry of an OpenAI-style GET /models. OpenAI itself
// sends only id and created; OpenRouter adds the rest.
type listedModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Created             int64    `json:"created"`
	ContextLength       int      `json:"context_length"`
	SupportedParameters []string `json:"supported_parameters"`
}

// fetchModels is the one GET {base}/models every OpenAI-compatible endpoint
// answers. The error names the status only: an upstream error body can echo
// the key back.
func fetchModels(ctx context.Context, base, key string) ([]listedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: http %d", res.StatusCode)
	}
	var body struct {
		Data []listedModel `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	return body.Data, nil
}

// sortModels orders a listing newest first, then by id, so the order is the
// same on every call whatever the provider sent.
func sortModels(list []ModelInfo) {
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].Created.Equal(list[j].Created) {
			return list[i].Created.After(list[j].Created)
		}
		return list[i].ID < list[j].ID
	})
}
