// Package knowledge is the Bot's document search: the owner picks workspace
// folders, a sweep indexes their text into Postgres (chunks, embeddings and a
// keyword index), and the Bot searches them with search_docs. Files stay on the
// Bot's machine; the worker only walks and extracts.
package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/pgsearch"
	"silo.agent/internal/textx"
)

const (
	knowledgeToolK     = 6
	knowledgeRPCK      = 10
	knowledgeMaxK      = 30
	knowledgeToolMaxK  = 15
	knowledgeCandidate = 30
	// knowledgeMaxDistance is the cosine distance past which a vector-only
	// neighbour is noise (a nearest neighbour always exists, related or not).
	knowledgeMaxDistance = 0.85
	// knowledgeSnippet caps one snippet in the tool result.
	knowledgeSnippet = 1200
	knowledgeIssues  = 20
)

// Service indexes and searches the Bots' folders.
type Service struct {
	db     *gorm.DB
	hub    *hub.Hub
	cfg    func() config.Config
	models *models.Service
	ws     *workspace.Service

	// locks holds each folder's folderSync (sync.go): its syncs take turns
	// and are counted; cancel stops a running one when its
	// folder is removed; pause (unix nanos) holds the sweep back after a
	// provider error.
	locks  sync.Map
	cancel sync.Map
	pause  atomic.Int64
}

func New(gdb *gorm.DB, h *hub.Hub, cfg func() config.Config, m *models.Service, ws *workspace.Service) *Service {
	return &Service{db: gdb, hub: h, cfg: cfg, models: m, ws: ws}
}

// Active reports whether search_docs is offered: knowledge is on and
// the Bot has at least one folder.
func (s *Service) Active(botID string) bool {
	if s.db == nil || !s.cfg().Knowledge.Enabled {
		return false
	}
	var n int64
	s.db.Model(&db.KnowledgeFolder{}).Where("bot_id = ?", botID).Limit(1).Count(&n)
	return n > 0
}

func (s *Service) Folders(botID string) []db.KnowledgeFolder {
	if s.db == nil || !s.cfg().Knowledge.Enabled {
		return nil
	}
	var rows []db.KnowledgeFolder
	s.db.Where("bot_id = ?", botID).Order("path").Find(&rows)
	return rows
}

// Prompt is the system-prompt section listing the indexed folders: only paths,
// since counts change on every sync and would bust the cached prompt tier. ""
// when there are none.
func Prompt(folders []db.KnowledgeFolder) string {
	if len(folders) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The owner indexed these folders so you can search them. For a question about what their files say, call `silo_runtime.search_docs(query, limit=6, path=None)` from Python first: it matches meaning and exact words and returns snippets citing the file plus page or line. Then `read` the cited path before relying on a snippet — the index can lag recent edits. Searching does not replace `grep` for code or for a file you already know.\n")
	for _, f := range folders {
		b.WriteString("- " + f.Path + "\n")
	}
	return b.String()
}

// knowledgePath normalizes a folder the owner picked: workspace-relative, no
// escape, and not scratch (bot, tmp) or the bare workspace or drives root.
func knowledgePath(p string) (string, error) {
	rel := workspace.Rel(p)
	rel = strings.Trim(path.Clean("/"+rel), "/")
	switch {
	case strings.HasPrefix(strings.TrimSpace(p), "..") || strings.Contains(p, "/../"):
		return "", errors.New("the folder must be inside the workspace")
	case rel == "":
		return "", errors.New("pick a folder inside the workspace, not the workspace itself")
	case rel == "bot" || strings.HasPrefix(rel, "bot/"):
		return "", errors.New("bot/ is the Bot's scratch space and is not indexed")
	case rel == "tmp" || strings.HasPrefix(rel, "tmp/"):
		return "", errors.New("tmp/ is wiped on every start and cannot be indexed")
	case rel == "drives":
		return "", errors.New("pick one folder inside a drive, not all drives at once")
	}
	return rel, nil
}

func within(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func (s *Service) view(f db.KnowledgeFolder) *v1.KnowledgeFolder {
	out := &v1.KnowledgeFolder{
		Id: f.ID, Path: f.Path, Status: f.Status, Detail: f.Detail,
		Files: int32(f.Files), Skipped: int32(f.Skipped), Chunks: int32(f.Chunks),
		Drive: isDrivePath(f.Path),
	}
	if f.LastSyncAt != nil {
		out.LastSyncAt = f.LastSyncAt.Format(time.RFC3339)
	}
	var bad []db.KnowledgeSource
	s.db.Select("path, detail, status").Where("folder_id = ? AND status <> 'ok'", f.ID).
		Order("path").Limit(knowledgeIssues).Find(&bad)
	for _, s := range bad {
		d := s.Detail
		if d == "" {
			d = s.Status
		}
		out.Issues = append(out.Issues, &v1.KnowledgeIssue{Path: s.Path, Detail: d})
	}
	return out
}

func (s *Service) ownFolder(ctx context.Context, botID, id string, need access.Need) (*db.Bot, *db.KnowledgeFolder, error) {
	b, err := access.Bot(ctx, s.db, botID, need)
	if err != nil {
		return nil, nil, err
	}
	f, err := access.BotRow[db.KnowledgeFolder](s.db, b.ID, id, "folder")
	if err != nil {
		return nil, nil, err
	}
	return b, f, nil
}

// --- RPCs ---

func (s *Service) ListKnowledge(ctx context.Context, req *connect.Request[v1.ListKnowledgeRequest]) (*connect.Response[v1.ListKnowledgeResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	var rows []db.KnowledgeFolder
	s.db.Where("bot_id = ?", b.ID).Order("path").Find(&rows)
	out := &v1.ListKnowledgeResponse{Enabled: s.cfg().Knowledge.Enabled}
	for _, f := range rows {
		out.Folders = append(out.Folders, s.view(f))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) AddKnowledgeFolder(ctx context.Context, req *connect.Request[v1.AddKnowledgeFolderRequest]) (*connect.Response[v1.KnowledgeFolder], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	if !s.cfg().Knowledge.Enabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("knowledge is turned off by the operator (knowledge.enabled)"))
	}
	p, err := knowledgePath(req.Msg.GetPath())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var have []db.KnowledgeFolder
	s.db.Where("bot_id = ?", b.ID).Find(&have)
	for _, f := range have {
		switch {
		case f.Path == p:
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("that folder is already indexed"))
		case within(p, f.Path):
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("%s is already inside the indexed folder %s", p, f.Path))
		case within(f.Path, p):
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("%s holds the indexed folder %s; remove that first", p, f.Path))
		}
	}
	// The folder must be readable now: a typo or an unmounted drive fails here
	// instead of becoming an empty index.
	if _, _, err := s.walk(ctx, b.ID, p, 1); err != nil {
		var ce *connect.Error
		if errors.As(err, &ce) {
			return nil, err
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cannot read %s: %w", p, err))
	}
	f := db.KnowledgeFolder{ID: ids.New(), BotID: b.ID, Path: p, Status: "syncing"}
	fs, _ := s.claim(f.ID) // a new id: nobody else has it
	if err := s.db.Create(&f).Error; err != nil {
		fs.n.Add(-1)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("that folder is already indexed"))
	}
	s.startSync(f.ID, fs)
	return connect.NewResponse(s.view(f)), nil
}

func (s *Service) RemoveKnowledgeFolder(ctx context.Context, req *connect.Request[v1.RemoveKnowledgeFolderRequest]) (*connect.Response[v1.RemoveKnowledgeFolderResponse], error) {
	_, f, err := s.ownFolder(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Manage)
	if err != nil {
		return nil, err
	}
	s.dropFolder(f.ID)
	return connect.NewResponse(&v1.RemoveKnowledgeFolderResponse{}), nil
}

func (s *Service) SyncKnowledge(ctx context.Context, req *connect.Request[v1.SyncKnowledgeRequest]) (*connect.Response[v1.KnowledgeFolder], error) {
	b, f, err := s.ownFolder(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	if !s.hub.Connected(b.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the Bot's machine is not running"))
	}
	f.Status = "syncing"
	fs, ok := s.claim(f.ID)
	if !ok {
		return connect.NewResponse(s.view(*f)), nil // one is running or waiting
	}
	s.db.Model(&db.KnowledgeFolder{}).Where("id = ?", f.ID).Update("status", "syncing")
	s.startSync(f.ID, fs)
	return connect.NewResponse(s.view(*f)), nil
}

func (s *Service) SearchKnowledge(ctx context.Context, req *connect.Request[v1.SearchKnowledgeRequest]) (*connect.Response[v1.SearchKnowledgeResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	k := int(req.Msg.GetLimit())
	if k <= 0 {
		k = knowledgeRPCK
	}
	hits, err := s.search(ctx, b.ID, req.Msg.GetQuery(), min(k, knowledgeMaxK), req.Msg.GetPath())
	if errors.Is(err, models.ErrNoQuery) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	out := &v1.SearchKnowledgeResponse{}
	for _, h := range hits {
		out.Hits = append(out.Hits, &v1.KnowledgeHit{
			Path: h.Path, Locator: h.Locator, Snippet: h.Content, Score: h.Score, IndexedAt: h.IndexedAt.Format(time.RFC3339),
		})
	}
	return connect.NewResponse(out), nil
}

// --- search ---

type knowledgeHit struct {
	Path      string
	Locator   string
	Content   string
	Score     float64
	IndexedAt time.Time
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// searchKnowledge is hybrid retrieval: the closest chunks by cosine and the
// best keyword matches (every word first, then any word), fused by reciprocal
// rank. A vector-only neighbour farther than knowledgeMaxDistance is dropped.
func (s *Service) search(ctx context.Context, botID, query string, k int, prefix string) ([]knowledgeHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, models.ErrNoQuery
	}
	vecs, _, err := s.models.Embed(ctx, botID, []string{query})
	if err != nil {
		return nil, err
	}
	prefix = strings.Trim(workspace.Rel(prefix), "/")

	var args pgsearch.Args
	where := "c.bot_id = " + args.Add(botID)
	if prefix != "" {
		where += " AND (s.path = " + args.Add(prefix) + " OR s.path LIKE " + args.Add(likeEscape(prefix)+"/%") + ` ESCAPE '\')`
	}
	stmt := pgsearch.Fused(&args, pgsearch.Source{
		From: "knowledge_chunks c JOIN knowledge_sources s ON s.id = c.source_id",
		ID:   "c.id", Embedding: "c.embedding", TSV: "c.tsv",
		Where: where, Candidates: knowledgeCandidate, MaxDistance: knowledgeMaxDistance,
	}, vecs[0], query) + `
SELECT s.path, c.locator, c.content, f.score, s.indexed_at
FROM fused f JOIN knowledge_chunks c ON c.id = f.id JOIN knowledge_sources s ON s.id = c.source_id
ORDER BY f.score DESC, s.path, c.ord LIMIT ` + args.Add(k)

	var out []knowledgeHit
	err = pgsearch.Run(ctx, s.db, stmt, &args, func(rows *sql.Rows) error {
		var h knowledgeHit
		var at sql.NullTime
		if err := rows.Scan(&h.Path, &h.Locator, &h.Content, &h.Score, &at); err != nil {
			return err
		}
		h.IndexedAt = at.Time
		out = append(out, h)
		return nil
	})
	return out, err
}

// --- the Bot's tool ---

// Tool is search_docs (chat tool and silo_runtime.search_docs). The
// caller already authorized bot.search_docs.
func (s *Service) Tool(ctx context.Context, botID, query, prefix string, k int, structured bool) (string, error) {
	if k <= 0 {
		k = knowledgeToolK
	}
	if !s.cfg().Knowledge.Enabled {
		return "", errors.New("document search is turned off by the operator")
	}
	if !s.Active(botID) {
		return "", errors.New("no folders are indexed; the owner adds them on the Knowledge page")
	}
	hits, err := s.search(ctx, botID, query, min(k, knowledgeToolMaxK), prefix)
	if err != nil {
		return "", err
	}
	if structured {
		type result struct {
			Path      string  `json:"path"`
			Locator   string  `json:"locator"`
			Snippet   string  `json:"snippet"`
			Score     float64 `json:"score"`
			IndexedAt string  `json:"indexed_at"`
		}
		out := struct {
			Results []result `json:"results"`
		}{Results: []result{}}
		for _, h := range hits {
			out.Results = append(out.Results, result{h.Path, h.Locator, textx.CapRunes(h.Content, knowledgeSnippet), h.Score, h.IndexedAt.Format(time.RFC3339)})
		}
		b, err := json.Marshal(out)
		return string(b), err
	}
	if len(hits) == 0 {
		return "No matches in the indexed folders.", nil
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] %s · %s (indexed %s)\n%s\n\n", i+1, h.Path, h.Locator, h.IndexedAt.Format("2006-01-02"), textx.CapRunes(h.Content, knowledgeSnippet))
	}
	b.WriteString("Snippets come from an index that can lag edits: `read` the path to confirm before relying on one.")
	return b.String(), nil
}
