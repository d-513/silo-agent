package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"silo.agent/internal/app/models"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/textx"
)

// Memories reach the model through its prompt, never through a search made
// while the human waits. Preferences and lessons are always there, word for
// word: they are few and they shape every answer. Facts are all there while
// they are few; once they outgrow InjectMax a digest stands in for them, a
// summary the sweep writes in the background and rewrites when the facts have
// changed. The exact facts stay a `recall` away. Last comes what the Bot was
// recently working on, the newest WorkShown entries: it changes most often,
// so nothing stands behind it. Only active memories are told: a tentative
// one waits to be seen again, a retired one is gone.
const (
	// InjectMax is the total size of fact text, in characters, up to which
	// every fact is in the prompt.
	InjectMax = 6000
	// GuideMax is the size up to which each of preferences and lessons is
	// listed; past it the newest stay (the keeper merges them before that).
	GuideMax = 3000
	// WorkShown is how many work entries are told, the newest; the older ones
	// are found with recall.
	WorkShown = 6
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

// told narrows a query to the memories of one kind a Bot is told.
func (s *Service) told(botID, kind string) *gorm.DB {
	return s.db.Model(&db.Memory{}).Where("bot_id = ? AND status = ? AND kind = ?", botID, StatusActive, kind)
}

// Prompt is the system prompt's memories section for one Bot. It reads the
// database only and changes only when the memories (or their digest) do, so
// the prompt before it stays cached.
func (s *Service) Prompt(botID string) string {
	if !s.cfg().Memory.InPrompt {
		return ""
	}
	var parts []string
	for _, g := range []struct{ kind, head string }{
		{KindPreference, "Preferences: how the human wants things done. Follow them unless told otherwise now."},
		{KindLesson, "Lessons: what has worked here before, and what has not."},
	} {
		if rows := s.listed(botID, g.kind, GuideMax, 0); len(rows) > 0 {
			parts = append(parts, g.head+"\n"+formatRecalled(rows))
		}
	}
	if facts := s.facts(botID); facts != "" {
		parts = append(parts, facts)
	}
	if rows := s.listed(botID, KindWork, GuideMax, WorkShown); len(rows) > 0 {
		parts = append(parts, "Recent work: what this Bot was last working on and where, each as it stood on its date. Check the place before relying on one; `recall` finds older work.\n"+formatRecalled(rows))
	}
	return strings.Join(parts, "\n")
}

// facts is the part of the section that tells the Bot's facts: all of them,
// or their digest.
func (s *Service) facts(botID string) string {
	var st struct {
		N    int64
		Size int64
	}
	s.told(botID, KindFact).Select("count(*) AS n, COALESCE(SUM(LENGTH(content)), 0) AS size").Scan(&st)
	if st.N == 0 {
		return ""
	}
	if st.Size <= InjectMax {
		return "Facts, oldest first. Use an id with `forget` when one is wrong.\n" +
			formatRecalled(s.listed(botID, KindFact, 0, 0))
	}
	var d db.MemoryDigest
	s.db.Where("bot_id = ?", botID).Limit(1).Find(&d)
	if strings.TrimSpace(d.Text) != "" {
		return "Facts: too many to list. This is a summary of them as of " + d.UpdatedAt.UTC().Format("2006-01-02") +
			"; `recall` searches all of them by meaning and by their words — use it before relying on a detail that is not spelled out here.\n\n" +
			strings.TrimSpace(d.Text)
	}
	return "Facts: too many to list. These are the newest; `recall` searches all of them by meaning and by their words.\n" +
		formatRecalled(s.listed(botID, KindFact, InjectMax, 0))
}

// listed is the Bot's active memories of one kind, oldest first. With a
// budget it is the newest ones whose text fits it, at most `most` of them
// (0 = as many as fit).
func (s *Service) listed(botID, kind string, budget, most int) []Recalled {
	var rows []Recalled
	q := s.told(botID, kind).Select(recalledCols)
	if budget <= 0 {
		q.Order("created_at, id").Scan(&rows)
		return rows
	}
	q.Order("created_at DESC, id DESC").Limit(200).Scan(&rows)
	used, n := 0, 0
	for n < len(rows) && (most <= 0 || n < most) && used+len(rows[n].Content) <= budget {
		used += len(rows[n].Content)
		n++
	}
	rows = rows[:n]
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// storeKey identifies the exact facts a Bot is told: the SHA-256 of
// each one's id and text, NUL-terminated, in id order (storeKeyOf). It is
// computed in the database, so a sweep does not load every memory to learn
// that nothing changed.
func (s *Service) storeKey(botID string) string {
	var key string
	err := s.db.Raw(`SELECT encode(sha256(COALESCE(string_agg(
			convert_to(id, 'UTF8') || '\x00'::bytea || convert_to(content, 'UTF8') || '\x00'::bytea,
			''::bytea ORDER BY id), ''::bytea)), 'hex')
		FROM memories WHERE bot_id = ? AND status = ? AND kind = ?`, botID, StatusActive, KindFact).Scan(&key).Error
	if err != nil || key == "" {
		var rows []struct {
			ID      string
			Content string
		}
		s.told(botID, KindFact).Select("id, content").Order("id").Scan(&rows)
		pairs := make([][2]string, len(rows))
		for i, r := range rows {
			pairs[i] = [2]string{r.ID, r.Content}
		}
		return storeKeyOf(pairs)
	}
	return key
}

// storeKeyOf is storeKey over (id, content) pairs in id order.
func storeKeyOf(rows [][2]string) string {
	h := sha256.New()
	for _, r := range rows {
		fmt.Fprintf(h, "%s\x00%s\x00", r[0], r[1])
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
	s.db.Model(&db.Memory{}).Select("bot_id").Where("status = ? AND kind = ?", StatusActive, KindFact).Group("bot_id").
		Having("SUM(LENGTH(content)) > ?", InjectMax).Order("bot_id").Scan(&bots)
	wrote := 0
	for _, botID := range bots {
		if wrote == digestPerTick {
			break
		}
		if v, ok := s.digestFailed.Load(botID); ok && now.Before(v.(time.Time)) {
			continue // a digest that failed is not retried, and billed, every tick
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
			s.digestFailed.Store(botID, now.Add(digestQuiet))
			continue
		}
		s.digestFailed.Delete(botID)
		row := db.MemoryDigest{BotID: botID, Key: key, Text: text, UpdatedAt: now}
		s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "bot_id"}}, UpdateAll: true}).Create(&row)
		wrote++
	}
	return wrote
}

// digest asks the memory model for the briefing of a Bot's memories.
func (s *Service) digest(ctx context.Context, botID string) (string, error) {
	rows := s.listed(botID, KindFact, digestInput, 0)
	if len(rows) == 0 {
		return "", fmt.Errorf("no memories")
	}
	var b strings.Builder
	for _, r := range rows {
		used := ""
		if r.UseCount > 0 {
			used = fmt.Sprintf(", used %d×", r.UseCount)
		}
		fmt.Fprintf(&b, "- (%s%s) %s\n", r.CreatedAt.UTC().Format("2006-01-02"), used, r.Content)
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
