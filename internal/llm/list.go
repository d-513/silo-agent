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
	// Kind is one of Kinds.
	Kind string
	// Voices are a speech model's voice ids, where the provider lists them.
	Voices []string
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
	SupportedVoices     []string `json:"supported_voices"`
	Architecture        struct {
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
}

// fetchModels is the one GET {base}/models every OpenAI-compatible endpoint
// answers; query narrows it where the endpoint knows how (OpenRouter's
// output_modalities). The error names the status only: an upstream error body
// can echo the key back.
func fetchModels(ctx context.Context, base, key string, query ...string) ([]listedModel, error) {
	url := strings.TrimRight(base, "/") + "/models"
	if len(query) > 0 {
		url += "?" + strings.Join(query, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

// modelInfos is a listing as the engine's own entries, all of one kind. An
// entry with no id is dropped.
func modelInfos(list []listedModel, kind string) []ModelInfo {
	out := make([]ModelInfo, 0, len(list))
	for _, m := range list {
		if m.ID == "" {
			continue
		}
		info := ModelInfo{ID: m.ID, Name: m.Name, ContextWindow: m.ContextLength, Kind: kind, Voices: m.SupportedVoices}
		if m.Created > 0 {
			info.Created = time.Unix(m.Created, 0)
		}
		out = append(out, info)
	}
	return out
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
