package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"silo.agent/internal/app/models"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/textx"
)

// Long-term memory reaches the model through its prompt, never through a
// search made while the human waits. While a Bot's memories are few they are
// all there. Once they outgrow InjectMax a digest stands in for them: a
// summary the sweep writes in the background and rewrites when the memories
// have changed. The exact facts stay a `recall` away.
const (
	// InjectMax is the total size of memory text, in characters, up to which
	// every memory is in the prompt.
	InjectMax = 6000
	// digestMax is the size the digest is asked to stay under.
	digestMax = 3000
	// digestInput caps what one digest call reads; a larger store is
	// summarized from its newest memories.
	digestInput = 60000
	// digestQuiet is how long a digest stands before a changed store
	// replaces it: every rewrite re-bills the conversations that follow.
	digestQuiet = 30 * time.Minute
	// digestPerTick caps the digests one sweep writes.
	digestPerTick = 3
)

// Prompt is the system prompt's long-term memory section for one Bot. It
// reads the database only and changes only when the memories (or their
// digest) do, so the prompt before it stays cached.
func (s *Service) Prompt(botID string) string {
	if !s.cfg().Memory.InPrompt {
		return ""
	}
	var st struct {
		N    int64
		Size int64
	}
	s.db.Model(&db.Memory{}).Select("count(*) AS n, COALESCE(SUM(LENGTH(content)), 0) AS size").
		Where("bot_id = ?", botID).Scan(&st)
	if st.N == 0 {
		return ""
	}
	if st.Size <= InjectMax {
		return "Everything in long-term memory, oldest first. Use an id with `forget` when one is wrong.\n" +
			formatRecalled(s.listed(botID, 0))
	}
	var d db.MemoryDigest
	s.db.Where("bot_id = ?", botID).Limit(1).Find(&d)
	if strings.TrimSpace(d.Text) != "" {
		return "Too many to list. This is a summary of them as of " + d.UpdatedAt.UTC().Format("2006-01-02") +
			"; `recall` searches all of them by meaning — use it before relying on a detail that is not spelled out here.\n\n" +
			strings.TrimSpace(d.Text)
	}
	return "Too many to list. These are the newest; `recall` searches all of them by meaning.\n" +
		formatRecalled(s.listed(botID, InjectMax))
}

// listed is the Bot's memories oldest first. With a budget it is the newest
// ones whose text fits it.
func (s *Service) listed(botID string, budget int) []Recalled {
	var rows []Recalled
	q := s.db.Model(&db.Memory{}).Select("id, kind, content, created_at").Where("bot_id = ?", botID)
	if budget <= 0 {
		q.Order("created_at, id").Scan(&rows)
		return rows
	}
	q.Order("created_at DESC, id DESC").Limit(200).Scan(&rows)
	used, n := 0, 0
	for n < len(rows) && used+len(rows[n].Content) <= budget {
		used += len(rows[n].Content)
		n++
	}
	rows = rows[:n]
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// storeKey identifies the exact contents of a Bot's memory.
func (s *Service) storeKey(botID string) string {
	var rows []struct {
		ID      string
		Content string
	}
	s.db.Model(&db.Memory{}).Select("id, content").Where("bot_id = ?", botID).Order("id").Scan(&rows)
	h := sha256.New()
	for _, r := range rows {
		fmt.Fprintf(h, "%s\x00%s\x00", r.ID, r.Content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SweepDigests writes the digest of every Bot whose memories outgrew the
// prompt and changed since its last one. It returns how many it wrote.
func (s *Service) SweepDigests(ctx context.Context, now time.Time) int {
	if s.db == nil || !s.cfg().Memory.InPrompt {
		return 0
	}
	var bots []string
	s.db.Model(&db.Memory{}).Select("bot_id").Group("bot_id").
		Having("SUM(LENGTH(content)) > ?", InjectMax).Order("bot_id").Scan(&bots)
	wrote := 0
	for _, botID := range bots {
		if wrote == digestPerTick {
			break
		}
		var d db.MemoryDigest
		s.db.Where("bot_id = ?", botID).Limit(1).Find(&d)
		if d.Text != "" && now.Sub(d.UpdatedAt) < digestQuiet {
			continue
		}
		key := s.storeKey(botID)
		if d.Text != "" && d.Key == key {
			continue
		}
		text, err := s.digest(ctx, botID)
		if err != nil {
			log.Printf("memory digest: bot %s: %v", botID, err)
			if _, provider := err.(models.ProviderError); provider {
				return wrote
			}
			continue
		}
		row := db.MemoryDigest{BotID: botID, Key: key, Text: text, UpdatedAt: now}
		s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "bot_id"}}, UpdateAll: true}).Create(&row)
		wrote++
	}
	return wrote
}

// digest asks the memory model for the briefing of a Bot's memories.
func (s *Service) digest(ctx context.Context, botID string) (string, error) {
	rows := s.listed(botID, digestInput)
	if len(rows) == 0 {
		return "", fmt.Errorf("no memories")
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "- (%s, %s) %s\n", r.CreatedAt.UTC().Format("2006-01-02"), r.Kind, r.Content)
	}
	client, _, model, err := s.models.Observed(s.cfg().MemoryModel(), botID, "memory")
	if err != nil {
		return "", models.ProviderError{Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	res, err := client.Complete(ctx, llm.Request{
		Model:    model,
		System:   []llm.SystemBlock{{Text: strings.ReplaceAll(prompts.MemoryDigest, "{max}", fmt.Sprint(digestMax))}},
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "The memories:\n\n" + b.String()}},
	})
	if err != nil {
		return "", models.ProviderError{Err: err}
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return "", fmt.Errorf("the model returned an empty digest")
	}
	// A model that ignores the cap must not grow the prompt without bound.
	return textx.CapRunes(text, 2*digestMax), nil
}

// DropDigest removes a Bot's digest (with the Bot).
func (s *Service) DropDigest(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.MemoryDigest{})
}
