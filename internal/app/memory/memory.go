// Package memory is a Bot's long-term memory: facts and lessons it saves with
// remember, ranked back by embedding with recall, and shown to its owner on the
// Memories page. Rows are pgvector embeddings in Postgres, per Bot.
package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/models"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

const (
	// RememberMax caps one memory; a long note belongs in a workspace file.
	RememberMax = 2000
	recallK     = 5
	recallMaxK  = 20
	// searchK / searchMaxK bound a human search from the Memories tab.
	searchK    = 20
	searchMaxK = 50
	// dedupeDistance is the cosine distance under which a new memory replaces
	// its nearest neighbour instead of adding a near-copy.
	dedupeDistance = 0.05
	// autoRecallK and autoRecallMaxDistance bound what a run gets for free:
	// only close matches, so an unrelated message injects nothing.
	autoRecallK           = 3
	autoRecallMaxDistance = 0.65
)

// Service stores and ranks a Bot's memories.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	models *models.Service
}

func New(gdb *gorm.DB, cfg func() config.Config, m *models.Service) *Service {
	return &Service{db: gdb, cfg: cfg, models: m}
}

// Recalled is one memory with its distance from the query.
type Recalled struct {
	ID        string
	Kind      string
	Content   string
	CreatedAt time.Time
	Distance  float64
}

// nearest returns up to k of the bot's memories closest to vec, closest first,
// no farther than maxDist (0 = no limit).
func (s *Service) nearest(botID string, vec pgvector.Vector, k int, maxDist float64) ([]Recalled, error) {
	var rows []Recalled
	q := s.db.Model(&db.Memory{}).
		Select("id, kind, content, created_at, embedding <=> ? AS distance", vec).
		Where("bot_id = ?", botID)
	if maxDist > 0 {
		q = q.Where("embedding <=> ? < ?", vec, maxDist)
	}
	// Order(clause.Expr) is silently dropped by GORM; OrderBy is what binds.
	err := q.Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "embedding <=> ?", Vars: []any{vec}}}).
		Limit(k).Scan(&rows).Error
	return rows, err
}

// Remember saves one fact the Bot chose to keep.
func (s *Service) Remember(ctx context.Context, botID, runID, content string) (string, error) {
	res, err := s.Save(ctx, Entry{BotID: botID, RunID: runID, Content: content})
	if err != nil {
		return "", err
	}
	if res.Updated {
		return fmt.Sprintf("updated memory %s (it already held this fact)", res.ID), nil
	}
	return "remembered " + res.ID, nil
}

const (
	Fact   = "fact"
	Lesson = "lesson"
)

// Entry is one memory to save: from the Bot's remember (runID) or from the
// memory collector (chatID).
type Entry struct {
	BotID, RunID, ChatID, Kind, Content string
}

// Saved says which row an Entry became and whether it replaced a near-copy.
type Saved struct {
	ID      string
	Updated bool
}

// Save embeds and stores one memory. A near-copy of an existing memory
// replaces it instead of adding a duplicate.
func (s *Service) Save(ctx context.Context, m Entry) (Saved, error) {
	content := strings.TrimSpace(m.Content)
	if content == "" {
		return Saved{}, errors.New("content required")
	}
	if len(content) > RememberMax {
		return Saved{}, fmt.Errorf("memory is %d characters; cap is %d. Save one short fact per call.", len(content), RememberMax)
	}
	kind := m.Kind
	if kind != Lesson {
		kind = Fact
	}
	vecs, model, err := s.models.Embed(ctx, []string{content})
	if err != nil {
		return Saved{}, err
	}
	near, err := s.nearest(m.BotID, vecs[0], 1, dedupeDistance)
	if err != nil {
		return Saved{}, err
	}
	if len(near) == 1 {
		up := map[string]any{"content": content, "embedding": vecs[0], "embed_model": model}
		if m.RunID != "" {
			up["run_id"] = m.RunID
		}
		if m.Kind != "" {
			up["kind"] = kind
		}
		err := s.db.Model(&db.Memory{}).Where("id = ?", near[0].ID).Updates(up).Error
		if err != nil {
			return Saved{}, err
		}
		return Saved{ID: near[0].ID, Updated: true}, nil
	}
	row := db.Memory{ID: ids.New(), BotID: m.BotID, Kind: kind, Content: content, Embedding: vecs[0], EmbedModel: model, RunID: m.RunID, ChatID: m.ChatID, CreatedAt: time.Now()}
	if err := s.db.Create(&row).Error; err != nil {
		return Saved{}, err
	}
	return Saved{ID: row.ID}, nil
}

// Rewrite replaces one memory's text (and its embedding).
func (s *Service) Rewrite(ctx context.Context, botID, id, content string) error {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > RememberMax {
		return fmt.Errorf("memory text must be 1..%d characters", RememberMax)
	}
	vecs, model, err := s.models.Embed(ctx, []string{content})
	if err != nil {
		return err
	}
	return s.db.Model(&db.Memory{}).Where("bot_id = ? AND id = ?", botID, id).Updates(map[string]any{
		"content": content, "embedding": vecs[0], "embed_model": model,
	}).Error
}

// Search ranks the bot's memories against query without marking them used.
func (s *Service) Search(ctx context.Context, botID, query string, k int, maxDist float64) ([]Recalled, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, models.ErrNoQuery
	}
	vecs, _, err := s.models.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return s.nearest(botID, vecs[0], k, maxDist)
}

// Recall is a search by the Bot: what it returns counts as used.
func (s *Service) Recall(ctx context.Context, botID, query string, k int, maxDist float64) ([]Recalled, error) {
	if k <= 0 {
		k = recallK
	}
	rows, err := s.Search(ctx, botID, query, min(k, recallMaxK), maxDist)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	used := make([]string, len(rows))
	for i, r := range rows {
		used[i] = r.ID
	}
	s.db.Model(&db.Memory{}).Where("id IN ?", used).Update("last_used_at", time.Now())
	return rows, nil
}

// Forget deletes one memory.
func (s *Service) Forget(botID, id string) (string, error) {
	res := s.db.Where("bot_id = ? AND id = ?", botID, strings.TrimSpace(id)).Delete(&db.Memory{})
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		return "", fmt.Errorf("no memory %s", id)
	}
	return "forgot " + id, nil
}

func formatRecalled(rows []Recalled) string {
	var b strings.Builder
	for _, r := range rows {
		kind := ""
		if r.Kind == Lesson {
			kind = " lesson:"
		}
		fmt.Fprintf(&b, "- [%s] (%s)%s %s\n", r.ID, r.CreatedAt.Format("2006-01-02"), kind, r.Content)
	}
	return b.String()
}

// RecallTool is the recall chat tool: the matches as prose.
func (s *Service) RecallTool(ctx context.Context, botID, query string, k int) (string, error) {
	rows, err := s.Recall(ctx, botID, query, k, 0)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "No memories match.", nil
	}
	return formatRecalled(rows), nil
}

// AutoRecall is the per-run note of memories close to the opening message. It
// is best-effort: an embedding failure only costs the note.
func (s *Service) AutoRecall(ctx context.Context, botID, userText string) string {
	if !s.cfg().Memory.AutoRecall || strings.TrimSpace(userText) == "" {
		return ""
	}
	var n int64
	if s.db.Model(&db.Memory{}).Where("bot_id = ?", botID).Count(&n); n == 0 {
		return ""
	}
	rows, err := s.Recall(ctx, botID, userText, autoRecallK, autoRecallMaxDistance)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return "Long-term memories close to this message (use `recall` for more, `forget` for wrong ones):\n" + formatRecalled(rows)
}

func (s *Service) ListMemories(ctx context.Context, req *connect.Request[v1.ListMemoriesRequest]) (*connect.Response[v1.ListMemoriesResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.Memory
	s.db.Select("id, kind, chat_id, content, created_at, last_used_at").
		Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListMemoriesResponse{}
	for _, r := range rows {
		out.Memories = append(out.Memories, Proto(r))
	}
	return connect.NewResponse(out), nil
}

// Proto is the wire form of a memory.
func Proto(m db.Memory) *v1.Memory {
	return &v1.Memory{
		Id: m.ID, Kind: m.Kind, ChatId: m.ChatID, Content: m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339), LastUsedAt: textx.RFC3339(m.LastUsedAt),
	}
}

func (s *Service) SearchMemories(ctx context.Context, req *connect.Request[v1.SearchMemoriesRequest]) (*connect.Response[v1.SearchMemoriesResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
		return nil, err
	}
	k := int(req.Msg.GetLimit())
	if k <= 0 {
		k = searchK
	}
	rows, err := s.Search(ctx, botID, req.Msg.GetQuery(), min(k, searchMaxK), 0)
	if errors.Is(err, models.ErrNoQuery) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	memIDs := make([]string, len(rows))
	for i, r := range rows {
		memIDs[i] = r.ID
	}
	// nearest selects only what the prompt needs; fetch the rest for the UI.
	full := map[string]db.Memory{}
	var extra []db.Memory
	s.db.Select("id, chat_id, last_used_at").Where("id IN ?", memIDs).Find(&extra)
	for _, m := range extra {
		full[m.ID] = m
	}
	out := &v1.SearchMemoriesResponse{}
	for _, r := range rows {
		x := full[r.ID]
		m := Proto(db.Memory{ID: r.ID, Kind: r.Kind, ChatID: x.ChatID, Content: r.Content, CreatedAt: r.CreatedAt, LastUsedAt: x.LastUsedAt})
		m.Distance = r.Distance
		out.Memories = append(out.Memories, m)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) DeleteMemory(ctx context.Context, req *connect.Request[v1.DeleteMemoryRequest]) (*connect.Response[v1.DeleteMemoryResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	if _, err := s.Forget(req.Msg.GetBotId(), req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&v1.DeleteMemoryResponse{}), nil
}
