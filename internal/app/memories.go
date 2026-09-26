package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
)

const (
	// rememberMax caps one memory; a long note belongs in a workspace file.
	rememberMax = 2000
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

type recalled struct {
	ID        string
	Content   string
	CreatedAt time.Time
	Distance  float64
}

// embed turns texts into vectors with the operator's embedding model.
func (a *App) embed(ctx context.Context, texts []string) ([]pgvector.Vector, string, error) {
	modelID := strings.TrimSpace(a.cfg().EmbedModel)
	if modelID == "" {
		modelID = config.DefaultEmbeddingModel
	}
	client, provider, model, err := a.providerClient(modelID)
	if err != nil {
		return nil, modelID, err
	}
	e, ok := client.(llm.Embedder)
	if !ok {
		return nil, modelID, fmt.Errorf("%s cannot embed; set embedding_model to an OpenAI-compatible model", provider)
	}
	raw, err := e.Embed(ctx, model, texts)
	if err != nil {
		return nil, modelID, fmt.Errorf("embed: %w", err)
	}
	out := make([]pgvector.Vector, len(raw))
	for i, v := range raw {
		out[i] = pgvector.NewVector(v)
	}
	return out, modelID, nil
}

// canEmbed reports whether modelID names a provider that implements
// llm.Embedder, without needing its API key.
func (a *App) canEmbed(modelID string) error {
	provider, _, err := llm.Parse(modelID)
	if err != nil {
		return err
	}
	// Build a throwaway client: only its type matters, so a missing key is
	// filled in and nothing is called.
	settings := llm.Settings{}
	maps.Copy(settings, a.cfg().ProviderSettings(provider))
	if settings.Get("api_key") == "" {
		settings["api_key"] = "probe"
	}
	client, err := llm.New(provider, settings)
	if err != nil {
		return err
	}
	if _, ok := client.(llm.Embedder); !ok {
		return fmt.Errorf("%s cannot embed; pick an OpenAI-compatible provider", provider)
	}
	return nil
}

// nearest returns up to k of the bot's memories closest to vec, closest first,
// no farther than maxDist (0 = no limit).
func (a *App) nearest(botID string, vec pgvector.Vector, k int, maxDist float64) ([]recalled, error) {
	var rows []recalled
	q := a.DB.Model(&db.Memory{}).
		Select("id, content, created_at, embedding <=> ? AS distance", vec).
		Where("bot_id = ?", botID)
	if maxDist > 0 {
		q = q.Where("embedding <=> ? < ?", vec, maxDist)
	}
	// Order(clause.Expr) is silently dropped by GORM; OrderBy is what binds.
	err := q.Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "embedding <=> ?", Vars: []any{vec}}}).
		Limit(k).Scan(&rows).Error
	return rows, err
}

func (a *App) remember(ctx context.Context, botID, runID, content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("content required")
	}
	if len(content) > rememberMax {
		return "", fmt.Errorf("memory is %d characters; cap is %d. Save one short fact per call.", len(content), rememberMax)
	}
	vecs, model, err := a.embed(ctx, []string{content})
	if err != nil {
		return "", err
	}
	near, err := a.nearest(botID, vecs[0], 1, dedupeDistance)
	if err != nil {
		return "", err
	}
	if len(near) == 1 {
		err := a.DB.Model(&db.Memory{}).Where("id = ?", near[0].ID).Updates(map[string]any{
			"content": content, "embedding": vecs[0], "embed_model": model, "run_id": runID,
		}).Error
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("updated memory %s (it already held this fact)", near[0].ID), nil
	}
	m := db.Memory{ID: ids.New(), BotID: botID, Content: content, Embedding: vecs[0], EmbedModel: model, RunID: runID, CreatedAt: time.Now()}
	if err := a.DB.Create(&m).Error; err != nil {
		return "", err
	}
	return "remembered " + m.ID, nil
}

var errNoQuery = errors.New("query required")

// search ranks the bot's memories against query without marking them used.
func (a *App) search(ctx context.Context, botID, query string, k int, maxDist float64) ([]recalled, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errNoQuery
	}
	vecs, _, err := a.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return a.nearest(botID, vecs[0], k, maxDist)
}

// recall is a search by the Bot: what it returns counts as used.
func (a *App) recall(ctx context.Context, botID, query string, k int, maxDist float64) ([]recalled, error) {
	if k <= 0 {
		k = recallK
	}
	rows, err := a.search(ctx, botID, query, min(k, recallMaxK), maxDist)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	used := make([]string, len(rows))
	for i, r := range rows {
		used[i] = r.ID
	}
	a.DB.Model(&db.Memory{}).Where("id IN ?", used).Update("last_used_at", time.Now())
	return rows, nil
}

func (a *App) forget(botID, id string) (string, error) {
	res := a.DB.Where("bot_id = ? AND id = ?", botID, strings.TrimSpace(id)).Delete(&db.Memory{})
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		return "", fmt.Errorf("no memory %s", id)
	}
	return "forgot " + id, nil
}

func formatRecalled(rows []recalled) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "- [%s] (%s) %s\n", r.ID, r.CreatedAt.Format("2006-01-02"), r.Content)
	}
	return b.String()
}

func (a *App) recallTool(ctx context.Context, botID string, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	k := 0
	if f, ok := args["limit"].(float64); ok {
		k = int(f)
	}
	rows, err := a.recall(ctx, botID, query, k, 0)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "No memories match.", nil
	}
	return formatRecalled(rows), nil
}

// autoRecall is the per-run note of memories close to the opening message. It
// is best-effort: an embedding failure only costs the note.
func (a *App) autoRecall(ctx context.Context, botID, userText string) string {
	if !a.cfg().Memory.AutoRecall || strings.TrimSpace(userText) == "" {
		return ""
	}
	var n int64
	if a.DB.Model(&db.Memory{}).Where("bot_id = ?", botID).Count(&n); n == 0 {
		return ""
	}
	rows, err := a.recall(ctx, botID, userText, autoRecallK, autoRecallMaxDistance)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return "Long-term memories close to this message (use `recall` for more, `forget` for wrong ones):\n" + formatRecalled(rows)
}

func (a *App) ListMemories(ctx context.Context, req *connect.Request[v1.ListMemoriesRequest]) (*connect.Response[v1.ListMemoriesResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.Memory
	a.DB.Select("id, content, created_at, last_used_at").
		Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListMemoriesResponse{}
	for _, r := range rows {
		m := &v1.Memory{Id: r.ID, Content: r.Content, CreatedAt: r.CreatedAt.Format(time.RFC3339)}
		if r.LastUsedAt != nil {
			m.LastUsedAt = r.LastUsedAt.Format(time.RFC3339)
		}
		out.Memories = append(out.Memories, m)
	}
	return connect.NewResponse(out), nil
}

func (a *App) SearchMemories(ctx context.Context, req *connect.Request[v1.SearchMemoriesRequest]) (*connect.Response[v1.SearchMemoriesResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := a.ownBot(ctx, botID); err != nil {
		return nil, err
	}
	k := int(req.Msg.GetLimit())
	if k <= 0 {
		k = searchK
	}
	rows, err := a.search(ctx, botID, req.Msg.GetQuery(), min(k, searchMaxK), 0)
	if errors.Is(err, errNoQuery) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	memIDs := make([]string, len(rows))
	for i, r := range rows {
		memIDs[i] = r.ID
	}
	// nearest selects only what the prompt needs; fetch last_used_at for the UI.
	used := map[string]*time.Time{}
	var full []db.Memory
	a.DB.Select("id, last_used_at").Where("id IN ?", memIDs).Find(&full)
	for _, m := range full {
		used[m.ID] = m.LastUsedAt
	}
	out := &v1.SearchMemoriesResponse{}
	for _, r := range rows {
		m := &v1.Memory{Id: r.ID, Content: r.Content, CreatedAt: r.CreatedAt.Format(time.RFC3339), Distance: r.Distance}
		if t := used[r.ID]; t != nil {
			m.LastUsedAt = t.Format(time.RFC3339)
		}
		out.Memories = append(out.Memories, m)
	}
	return connect.NewResponse(out), nil
}

func (a *App) DeleteMemory(ctx context.Context, req *connect.Request[v1.DeleteMemoryRequest]) (*connect.Response[v1.DeleteMemoryResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	if _, err := a.forget(req.Msg.GetBotId(), req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&v1.DeleteMemoryResponse{}), nil
}
