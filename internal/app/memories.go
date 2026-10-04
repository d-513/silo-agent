package app

import (
	"context"
	"errors"
	"fmt"
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
	Kind      string
	Content   string
	CreatedAt time.Time
	Distance  float64
}

// embedModelID is the operator's embedding model, or the default.
func (a *App) embedModelID() string {
	if m := strings.TrimSpace(a.cfg().EmbedModel); m != "" {
		return m
	}
	return config.DefaultEmbeddingModel
}

// embed turns texts into vectors with the operator's embedding model.
func (a *App) embed(ctx context.Context, texts []string) ([]pgvector.Vector, string, error) {
	modelID := a.embedModelID()
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
	return providerCan[llm.Embedder](a, modelID, "embed")
}

// providerCan reports whether modelID names a provider whose client implements
// the capability interface T; verb names what it cannot do in the error.
func providerCan[T any](a *App, modelID, verb string) error {
	client, provider, err := a.probeClient(modelID)
	if err != nil {
		return err
	}
	if _, ok := client.(T); !ok {
		return fmt.Errorf("%s cannot %s; pick an OpenAI-compatible provider", provider, verb)
	}
	return nil
}

// nearest returns up to k of the bot's memories closest to vec, closest first,
// no farther than maxDist (0 = no limit).
func (a *App) nearest(botID string, vec pgvector.Vector, k int, maxDist float64) ([]recalled, error) {
	var rows []recalled
	q := a.DB.Model(&db.Memory{}).
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

func (a *App) remember(ctx context.Context, botID, runID, content string) (string, error) {
	res, err := a.saveMemory(ctx, newMemory{botID: botID, runID: runID, content: content})
	if err != nil {
		return "", err
	}
	if res.updated {
		return fmt.Sprintf("updated memory %s (it already held this fact)", res.id), nil
	}
	return "remembered " + res.id, nil
}

const (
	memoryFact   = "fact"
	memoryLesson = "lesson"
)

// newMemory is one memory to save: from the Bot's remember (runID) or from the
// memory collector (chatID).
type newMemory struct {
	botID, runID, chatID, kind, content string
}

type savedMemory struct {
	id      string
	updated bool
}

// saveMemory embeds and stores one memory. A near-copy of an existing memory
// replaces it instead of adding a duplicate.
func (a *App) saveMemory(ctx context.Context, m newMemory) (savedMemory, error) {
	content := strings.TrimSpace(m.content)
	if content == "" {
		return savedMemory{}, errors.New("content required")
	}
	if len(content) > rememberMax {
		return savedMemory{}, fmt.Errorf("memory is %d characters; cap is %d. Save one short fact per call.", len(content), rememberMax)
	}
	kind := m.kind
	if kind != memoryLesson {
		kind = memoryFact
	}
	vecs, model, err := a.embed(ctx, []string{content})
	if err != nil {
		return savedMemory{}, err
	}
	near, err := a.nearest(m.botID, vecs[0], 1, dedupeDistance)
	if err != nil {
		return savedMemory{}, err
	}
	if len(near) == 1 {
		up := map[string]any{"content": content, "embedding": vecs[0], "embed_model": model}
		if m.runID != "" {
			up["run_id"] = m.runID
		}
		if m.kind != "" {
			up["kind"] = kind
		}
		err := a.DB.Model(&db.Memory{}).Where("id = ?", near[0].ID).Updates(up).Error
		if err != nil {
			return savedMemory{}, err
		}
		return savedMemory{id: near[0].ID, updated: true}, nil
	}
	row := db.Memory{ID: ids.New(), BotID: m.botID, Kind: kind, Content: content, Embedding: vecs[0], EmbedModel: model, RunID: m.runID, ChatID: m.chatID, CreatedAt: time.Now()}
	if err := a.DB.Create(&row).Error; err != nil {
		return savedMemory{}, err
	}
	return savedMemory{id: row.ID}, nil
}

// rewriteMemory replaces one memory's text (and its embedding).
func (a *App) rewriteMemory(ctx context.Context, botID, id, content string) error {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > rememberMax {
		return fmt.Errorf("memory text must be 1..%d characters", rememberMax)
	}
	vecs, model, err := a.embed(ctx, []string{content})
	if err != nil {
		return err
	}
	return a.DB.Model(&db.Memory{}).Where("bot_id = ? AND id = ?", botID, id).Updates(map[string]any{
		"content": content, "embedding": vecs[0], "embed_model": model,
	}).Error
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
		kind := ""
		if r.Kind == memoryLesson {
			kind = " lesson:"
		}
		fmt.Fprintf(&b, "- [%s] (%s)%s %s\n", r.ID, r.CreatedAt.Format("2006-01-02"), kind, r.Content)
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
	a.DB.Select("id, kind, chat_id, content, created_at, last_used_at").
		Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListMemoriesResponse{}
	for _, r := range rows {
		out.Memories = append(out.Memories, memoryProto(r))
	}
	return connect.NewResponse(out), nil
}

func memoryProto(m db.Memory) *v1.Memory {
	return &v1.Memory{
		Id: m.ID, Kind: m.Kind, ChatId: m.ChatID, Content: m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339), LastUsedAt: rfc3339(m.LastUsedAt),
	}
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
	// nearest selects only what the prompt needs; fetch the rest for the UI.
	full := map[string]db.Memory{}
	var extra []db.Memory
	a.DB.Select("id, chat_id, last_used_at").Where("id IN ?", memIDs).Find(&extra)
	for _, m := range extra {
		full[m.ID] = m
	}
	out := &v1.SearchMemoriesResponse{}
	for _, r := range rows {
		x := full[r.ID]
		m := memoryProto(db.Memory{ID: r.ID, Kind: r.Kind, ChatID: x.ChatID, Content: r.Content, CreatedAt: r.CreatedAt, LastUsedAt: x.LastUsedAt})
		m.Distance = r.Distance
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
