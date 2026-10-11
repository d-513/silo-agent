package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const typeSafeBase = "https://api.typesafe.ai/v1"

// errDecisionOnly is what a decision-only provider answers to a completion.
var errDecisionOnly = errors.New("TypeSafe serves decision models only; they answer typed questions and write no text")

// typeSafeClient is TypeSafe's System One API: one endpoint that takes a
// state and typed questions (POST {base}/systemone) and a model list.
type typeSafeClient struct {
	base      string
	key       string
	decisions decisionEndpoint
}

func newTypeSafe(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	if base == "" {
		base = typeSafeBase
	}
	return &typeSafeClient{base: base, key: key, decisions: decisionEndpoint{url: base + "/systemone", key: key}}, nil
}

func (c *typeSafeClient) Stream(context.Context, Request) (Stream, error) {
	return nil, errDecisionOnly
}

func (c *typeSafeClient) Complete(context.Context, Request) (Response, error) {
	return Response{}, errDecisionOnly
}

func (c *typeSafeClient) Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	return c.decisions.decide(ctx, req)
}

// typeSafeModel is one entry of GET {base}/models: a model or an alias of
// one, named by `name`.
type typeSafeModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

// ListModels lists the models and aliases the key may call. The API answers
// a bare list; an OpenAI-style {"data": [...]} is read too.
func (c *typeSafeClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: http %d", res.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	var list []typeSafeModel
	if json.Unmarshal(raw, &list) != nil {
		var wrapped struct {
			Data []typeSafeModel `json:"data"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil, fmt.Errorf("list models: %w", err)
		}
		list = wrapped.Data
	}
	out := make([]ModelInfo, 0, len(list))
	for _, m := range list {
		id := m.ID
		if id == "" {
			id = m.Name
		}
		if id == "" {
			continue
		}
		info := ModelInfo{ID: id, Name: m.Description, Kind: KindDecision}
		for _, layout := range []string{time.DateOnly, time.RFC3339} {
			if t, err := time.Parse(layout, m.ReleaseDate); err == nil {
				info.Created = t
				break
			}
		}
		out = append(out, info)
	}
	sortModels(out)
	return out, nil
}
