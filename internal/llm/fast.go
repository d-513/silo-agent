package llm

import (
	"context"
	"strings"
)

// Fast mode is a model served faster at a premium price: Anthropic's
// `speed: "fast"`, OpenAI's and OpenRouter's `service_tier: "fast"`, Claude
// Code's fastMode setting. Silo names it on the model id, `claude/opus:fast`,
// so a fast model is an allowlist entry of its own that the operator chose,
// with its cost, and every picker and role can name. Only text models have
// it, and only from a provider whose Descriptor says Fast.
//
// The suffix stops at the client layer: models.Service.Client hands out the
// bare model name (thinking levels, context windows and model lists know no
// suffix), and WithFast marks each request Fast for the provider to map. A
// fast request the provider refuses or rate-limits fails the turn; it is
// never quietly answered at standard speed.

// FastSuffix ends the model id of a model in fast mode.
const FastSuffix = ":fast"

// SplitFast takes the fast suffix off a model id or name.
func SplitFast(model string) (base string, fast bool) {
	model = strings.TrimSpace(model)
	if b, ok := strings.CutSuffix(model, FastSuffix); ok {
		return b, true
	}
	return model, false
}

// WithFast is a client whose every request asks for fast mode.
func WithFast(c Client) Client { return fastClient{c} }

type fastClient struct{ inner Client }

func (c fastClient) Stream(ctx context.Context, req Request) (Stream, error) {
	req.Fast = true
	return c.inner.Stream(ctx, req)
}

func (c fastClient) Complete(ctx context.Context, req Request) (Response, error) {
	req.Fast = true
	return c.inner.Complete(ctx, req)
}

// EndTurn passes a run's closing tool results on, as the client it wraps would.
func (c fastClient) EndTurn(ctx context.Context, req Request) error {
	req.Fast = true
	if te, ok := c.inner.(TurnEnder); ok {
		return te.EndTurn(ctx, req)
	}
	return nil
}
