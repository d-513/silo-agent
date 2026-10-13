// Package filter is the output filter: what an action's output rule does to a
// tool result before the Bot reads it. It is the mirror of the approval gate.
// The gate judges what a Bot wants to do against its owner's auto-approval
// policy; the filter judges what came back against the owner's output policy,
// because a web page, a mail or a connector's answer is written by someone
// else and may be written to steer the Bot.
//
// The model is the operator's filter model and may be of either kind. Its
// answer is one of three: clean (the result passes untouched), flagged (it
// passes inside a frame that calls it untrusted) or withheld (the Bot gets a
// placeholder). A model that fails, is unsure or is given more than it can
// read never holds a run up: the result passes, marked unchecked.
//
// Nothing a model writes here reaches the Bot. The frames are fixed text
// (frame.go): a reason in the judge's words would be the attacker's words at
// one remove.
package filter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/app/approval"
	"silo.agent/internal/app/models"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/security"
)

// Verdict is what the filter made of one result.
type Verdict string

const (
	Clean Verdict = "clean"
	// Flagged passes, framed as untrusted.
	Flagged Verdict = "flagged"
	// Withheld never reaches the Bot.
	Withheld Verdict = "withheld"
	// Unchecked passes with a warning: no model gave a verdict.
	Unchecked Verdict = "unchecked"
)

// rank orders verdicts by how much they do to a result. A result judged in
// parts gets the worst of them.
var rank = map[Verdict]int{Clean: 0, Unchecked: 1, Flagged: 2, Withheld: 3}

func worse(a, b Verdict) Verdict {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

const (
	// timeout bounds the whole check of one result; past it the result
	// passes unchecked.
	timeout = 30 * time.Second
	// chunkRunes is how much of a result one model call reads, and overlap
	// how much of the previous part the next repeats, so text that sits on
	// a cut is read whole by one of them.
	chunkRunes = 8000
	overlap    = 400
	// parallel is how many parts of one result are judged at once.
	parallel = 4
	// cacheFor is how long a verdict on the same text for the same call
	// stands, and cacheMax how many are kept.
	cacheFor = 10 * time.Minute
	cacheMax = 2000
)

// Request is everything the filter model is shown about one part of one
// result, and all of it.
type Request struct {
	Policy string
	// Source is where the call came from and what the human last asked
	// there, exactly as the approval gate is told.
	Source approval.Source
	// Key is connector.action; Action describes the call with its arguments
	// redacted like an approval slip's.
	Key    string
	Action security.Prompt
	// Content is the part to judge; Part and Parts place it in the result.
	Content string
	Part    int
	Parts   int
}

// Judge is one way of getting a verdict on a Request. There is one per kind
// of model; another checker is another Judge.
type Judge interface {
	Judge(ctx context.Context, modelID, botID string, r Request) (Verdict, error)
}

// Call names the tool call whose result is being filtered.
type Call struct {
	Bot       *db.Bot
	ChatID    string
	Connector string
	Action    string
	ArgsJSON  string
}

// Service checks tool results for actions whose output rule is Check.
type Service struct {
	cfg    func() config.Config
	source func(chatID string) approval.Source
	judges map[string]Judge

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	verdict Verdict
	at      time.Time
}

func New(cfg func() config.Config, m *models.Service, a *approval.Service) *Service {
	return &Service{
		cfg: cfg, source: a.Source,
		judges: map[string]Judge{
			llm.KindText:     textJudge{cfg: cfg, models: m},
			llm.KindDecision: decisionJudge{models: m},
		},
		cache: map[string]cached{},
	}
}

// Policy is the output policy a Bot's results are judged by: its owner's, or
// the built-in one when the owner wrote none.
func Policy(bot *db.Bot) string {
	if p := strings.TrimSpace(bot.OutputPolicy); p != "" {
		return p
	}
	return strings.TrimSpace(prompts.FilterPolicy)
}

// Check judges one result. It never fails: whatever keeps a model from giving
// a verdict makes the result Unchecked.
func (s *Service) Check(ctx context.Context, c Call, content string) Verdict {
	cfg := s.cfg()
	parts := split(reveal(content), cfg.Filter.Chars())
	if parts == nil {
		return Unchecked
	}
	modelID := cfg.FilterModel()
	judge := s.judges[cfg.ModelKind(modelID)]
	if judge == nil {
		return Unchecked
	}
	base := Request{
		Policy: Policy(c.Bot), Source: s.source(c.ChatID),
		Key:    security.Key(c.Connector, c.Action),
		Action: security.Describe(c.Connector, c.Action, security.Redact(c.Connector, c.Action, c.ArgsJSON)),
		Parts:  len(parts),
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	verdicts := make([]Verdict, len(parts))
	gate := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, part := range parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			req := base
			req.Content, req.Part = part, i+1
			verdicts[i] = s.one(ctx, judge, modelID, c.Bot.ID, req)
		}()
	}
	wg.Wait()
	out := Clean
	for _, v := range verdicts {
		out = worse(out, v)
	}
	return out
}

// one judges a single part, from the cache when the same text was just judged
// for the same call.
func (s *Service) one(ctx context.Context, judge Judge, modelID, botID string, r Request) Verdict {
	key := cacheKey(modelID, r)
	if v, ok := s.recall(key); ok {
		return v
	}
	if ctx.Err() != nil {
		return Unchecked
	}
	v, err := judge.Judge(ctx, modelID, botID, r)
	if err != nil {
		log.Printf("output filter model %s: %v", modelID, err)
		return Unchecked
	}
	if _, known := rank[v]; !known {
		return Unchecked
	}
	if v != Unchecked {
		s.keep(key, v)
	}
	return v
}

func (s *Service) recall(key string) (Verdict, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[key]
	if !ok || time.Since(c.at) > cacheFor {
		return "", false
	}
	return c.verdict, true
}

func (s *Service) keep(key string, v Verdict) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= cacheMax {
		for k, c := range s.cache {
			if time.Since(c.at) > cacheFor {
				delete(s.cache, k)
			}
		}
		if len(s.cache) >= cacheMax {
			clear(s.cache)
		}
	}
	s.cache[key] = cached{verdict: v, at: time.Now()}
}

// cacheKey covers everything the verdict depends on.
func cacheKey(modelID string, r Request) string {
	h := sha256.New()
	for _, p := range []string{modelID, r.Policy, r.Source.From(), r.Source.Human, r.Key, r.Action.Summary, r.Content} {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	for _, f := range r.Action.Fields {
		h.Write([]byte(f.Label + "\x00" + f.Value + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// split cuts a result into the parts a model reads. It answers nil for a
// result longer than max characters: one too long to check.
func split(content string, max int) []string {
	r := []rune(content)
	if len(r) > max {
		return nil
	}
	if len(r) <= chunkRunes {
		return []string{content}
	}
	var parts []string
	for start := 0; start < len(r); start += chunkRunes - overlap {
		end := min(start+chunkRunes, len(r))
		parts = append(parts, string(r[start:end]))
		if end == len(r) {
			break
		}
	}
	return parts
}

// Unicode tag characters (U+E0020 to U+E007E) mirror ASCII and draw nothing:
// text written in them is invisible to a person and plain to a model, which
// is what makes them a way to smuggle instructions. Their one honest use is
// inside a flag emoji, right after U+1F3F4.
const (
	tagFirst = 0xE0020
	tagLast  = 0xE007E
	tagEnd   = 0xE007F
	flagBase = 0x1F3F4
)

// reveal is a result as the judge reads it: text hidden in tag characters is
// spelled out and named as hidden, so the judge rules on what a model would
// read there and not on what a person sees. The Bot's copy is not changed.
func reveal(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r >= tagFirst && r <= tagEnd }) {
		return s
	}
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r < tagFirst || r > tagEnd {
			b.WriteRune(r)
			continue
		}
		j := i
		var hidden strings.Builder
		for j < len(rs) && rs[j] >= tagFirst && rs[j] <= tagEnd {
			if rs[j] != tagEnd {
				hidden.WriteRune(rs[j] - 0xE0000)
			}
			j++
		}
		// A flag's region code: a few tag letters after the black flag.
		if i > 0 && rs[i-1] == flagBase && hidden.Len() <= 8 {
			b.WriteString(string(rs[i:j]))
		} else {
			b.WriteString(" [text hidden from human readers in invisible Unicode tag characters: " + hidden.String() + "] ")
		}
		i = j - 1
	}
	return b.String()
}
