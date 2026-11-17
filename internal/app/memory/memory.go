// Package memory is what a Bot keeps across conversations: facts, the human's
// preferences, lessons about the work and what it was last working on, saved
// with remember, searched with
// recall, shown to its owner on the Memories page and tended in the
// background by the keeper (keeper.go). Rows are in Postgres, per Bot, each
// with a pgvector embedding and a keyword index.
package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	"silo.agent/internal/pgsearch"
	"silo.agent/internal/textx"
)

// What a memory is about.
const (
	KindFact       = "fact"
	KindPreference = "preference"
	KindLesson     = "lesson"
	// KindWork is what the Bot was working on and where: one entry per piece
	// of work, replaced as the work goes on.
	KindWork = "work"
)

// Kinds are the kinds a memory may be, in the order they are told.
var Kinds = []string{KindPreference, KindLesson, KindFact, KindWork}

// Where a memory stands.
const (
	// StatusActive is a memory the Bot is told.
	StatusActive = "active"
	// StatusTentative is one that was worked out, not said: it waits out of
	// the prompt and out of recall until it is seen again, or its owner keeps it.
	StatusTentative = "tentative"
	// StatusRetired is one that was replaced or forgotten. It is kept for
	// retiredKeep so the owner can put it back, and reaches nobody meanwhile.
	StatusRetired = "retired"
)

const (
	// RememberMax caps one memory; a long note belongs in a workspace file.
	RememberMax = 2000
	recallK     = 5
	recallMaxK  = 20
	// searchK / searchMaxK bound a human search from the Memories tab.
	searchK    = 20
	searchMaxK = 50
	// searchCandidates is how many rows each half of a search hands the fusion.
	searchCandidates = 30
	// dedupeDistance is the cosine distance under which a new memory is the
	// one already there, said again.
	dedupeDistance = 0.05
	// nearDistance is how close an existing memory must be for remember to
	// show it, so the one who saves can retire what the new one replaces.
	nearDistance = 0.35
	nearShown    = 3
	// PromoteAt is the number of separate sightings at which a tentative
	// memory becomes active.
	PromoteAt = 2
	// evidenceGap is how far apart two sightings must be to count as two: one
	// long conversation read in parts is one sighting.
	evidenceGap = time.Hour
	// retiredKeep is how long a retired memory can still be put back, and
	// tentativeKeep how long a guess nothing ever supported is kept.
	retiredKeep   = 30 * 24 * time.Hour
	tentativeKeep = 90 * 24 * time.Hour
)

// Service stores and ranks a Bot's memories.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	models *models.Service
	jobs   Jobs
	// saving makes Save's look for a near-copy and its write one step, so a
	// remember and the keeper saving the same fact add one row.
	saving sync.Mutex
	// digestFailed (bot id → time.Time) holds a Bot's digest back after a
	// failure that was not the provider's.
	digestFailed sync.Map
	// reading (bot id → *reading) is the conversation the keeper is reading
	// for a Bot now, and what it has done with it.
	reading sync.Map
	// tidied (bot id → time.Time) is when the keeper last tidied a Bot's
	// memory, and batches (bot id → batch) the review batches its run took.
	tidied  sync.Map
	batches sync.Map
	// Room says how many bytes a review batch may fill, from what the
	// keeper's model can hold; nil keeps batches small.
	Room func() int
}

// New makes the service. jobs runs the keeper; nil leaves memory untended.
func New(gdb *gorm.DB, cfg func() config.Config, m *models.Service, jobs Jobs) *Service {
	s := &Service{db: gdb, cfg: cfg, models: m, jobs: jobs}
	if jobs != nil {
		jobs.Pin(s.keeper())
	}
	return s
}

// Recalled is one memory as a search or a listing returns it.
type Recalled struct {
	ID        string
	Content   string
	CreatedAt time.Time
	Kind      string
	Status    string
	Evidence  int
	UseCount  int
	// Distance is its cosine distance from the query, when there was one.
	Distance float64
}

// live narrows a query to the memories somebody can be told: the active
// ones, and with tentative the guesses still waiting.
func live(q *gorm.DB, tentative bool) *gorm.DB {
	if tentative {
		return q.Where("status <> ?", StatusRetired)
	}
	return q.Where("status = ?", StatusActive)
}

const recalledCols = "id, content, created_at, kind, status, evidence, use_count"

// nearest returns up to k of the bot's live memories closest to vec, closest
// first, no farther than maxDist (0 = no limit).
func (s *Service) nearest(botID string, vec pgvector.Vector, k int, maxDist float64, tentative bool) ([]Recalled, error) {
	var rows []Recalled
	err := s.db.Transaction(func(tx *gorm.DB) error {
		pgsearch.IterativeScan(tx)
		q := live(tx.Model(&db.Memory{}), tentative).
			Select(recalledCols+", embedding <=> ? AS distance", vec).
			Where("bot_id = ?", botID)
		if maxDist > 0 {
			q = q.Where("embedding <=> ? < ?", vec, maxDist)
		}
		// Order(clause.Expr) is silently dropped by GORM; OrderBy is what binds.
		return q.Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "embedding <=> ?", Vars: []any{vec}}}).
			Limit(k).Scan(&rows).Error
	})
	// relaxed_order may return the k nearest slightly out of order.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Distance < rows[j].Distance })
	return rows, err
}

// Entry is one memory to save.
type Entry struct {
	BotID, RunID, Content string
	// ChatID is the conversation it was read from, when the keeper read one.
	ChatID string
	// Kind is what it is about; "" is a fact.
	Kind string
	// Inferred marks what the human did not say: it is saved tentative.
	Inferred bool
	// Replaces are memories this one makes out of date: they are retired.
	Replaces []string
	// Keeper marks a save by the keeper's run: it is filed under the
	// conversation the keeper is reading.
	Keeper bool
}

// Saved says what an Entry became.
type Saved struct {
	ID string
	// Updated is set when it was already there and was said again.
	Updated bool
	// Status is where the memory stands now.
	Status string
	// Retired are the memories it replaced.
	Retired []string
	// Near are other memories close to it, for the one who saved to judge.
	Near []Recalled
}

// errNoInfer refuses a guess while the operator keeps memory to what was said.
var errNoInfer = errors.New("not saved: this installation keeps only what the human said (memory.infer is off)")

// Save embeds and stores one memory. One already there (a near-copy) is
// updated instead: said again by the human it becomes active, seen again by
// inference it gains a sighting and becomes active at PromoteAt.
func (s *Service) Save(ctx context.Context, m Entry) (Saved, error) {
	content := strings.TrimSpace(m.Content)
	if content == "" {
		return Saved{}, errors.New("content required")
	}
	if n := utf8.RuneCountInString(content); n > RememberMax {
		return Saved{}, fmt.Errorf("memory is %d characters; cap is %d. Save one short fact per call.", n, RememberMax)
	}
	kind := strings.ToLower(strings.TrimSpace(m.Kind))
	if kind == "" {
		kind = KindFact
	}
	if !slices.Contains(Kinds, kind) {
		return Saved{}, fmt.Errorf("kind must be one of %s", strings.Join(Kinds, ", "))
	}
	if kind == KindWork {
		// What was worked on is what happened, not a guess about the human.
		m.Inferred = false
	}
	if m.Inferred && !s.cfg().Memory.Infer {
		return Saved{}, errNoInfer
	}
	vecs, model, err := s.models.Embed(ctx, m.BotID, []string{content})
	if err != nil {
		return Saved{}, err
	}
	rd := s.readingFor(m)
	chatID := m.ChatID
	if rd != nil {
		chatID = rd.chatID
	}
	now := time.Now()

	s.saving.Lock()
	defer s.saving.Unlock()
	near, err := s.nearest(m.BotID, vecs[0], nearShown+1+len(m.Replaces), nearDistance, true)
	if err != nil {
		return Saved{}, err
	}
	var out Saved
	if len(near) > 0 && near[0].Distance < dedupeDistance && !slices.Contains(m.Replaces, near[0].ID) {
		out, err = s.again(near[0], m, kind, content, vecs[0], model, now)
		near = near[1:]
	} else {
		row := db.Memory{
			ID: ids.New(), BotID: m.BotID, Content: content, Embedding: vecs[0], EmbedModel: model,
			RunID: m.RunID, ChatID: chatID, CreatedAt: now, Kind: kind, Status: StatusActive,
			Inferred: m.Inferred, Evidence: 1, EvidenceAt: &now,
		}
		if m.Inferred {
			row.Status = StatusTentative
		}
		err = s.db.Create(&row).Error
		out = Saved{ID: row.ID, Status: row.Status}
	}
	if err != nil {
		return Saved{}, err
	}
	out.Retired = s.retire(m.BotID, m.Replaces, out.ID)
	for _, r := range near {
		if r.ID != out.ID && !slices.Contains(out.Retired, r.ID) && len(out.Near) < nearShown {
			out.Near = append(out.Near, r)
		}
	}
	rd.count(out)
	return out, nil
}

// again is Save meeting a memory that already holds this: the human saying
// it settles it; an inference seeing it is one more sighting.
func (s *Service) again(old Recalled, m Entry, kind, content string, vec pgvector.Vector, model string, now time.Time) (Saved, error) {
	var row db.Memory
	if err := s.db.Select("id, status, evidence, evidence_at, inferred").Where("id = ?", old.ID).Limit(1).Find(&row).Error; err != nil {
		return Saved{}, err
	}
	up := map[string]any{}
	if m.RunID != "" {
		up["run_id"] = m.RunID
	}
	status := row.Status
	if seenAgain(row.EvidenceAt, now) {
		row.Evidence++
		up["evidence"], up["evidence_at"] = row.Evidence, now
	}
	if m.Inferred {
		if row.Evidence >= PromoteAt {
			status = StatusActive
		}
	} else {
		// What the human said replaces what was worked out, words and all.
		status = StatusActive
		up["content"], up["embedding"], up["embed_model"] = content, vec, model
		up["kind"], up["inferred"], up["reviewed_at"] = kind, false, nil
	}
	up["status"] = status
	if err := s.db.Model(&db.Memory{}).Where("id = ?", old.ID).Updates(up).Error; err != nil {
		return Saved{}, err
	}
	return Saved{ID: old.ID, Updated: true, Status: status}, nil
}

// seenAgain reports whether a sighting now is a separate one from the last.
func seenAgain(last *time.Time, now time.Time) bool {
	return last == nil || now.Sub(*last) >= evidenceGap
}

// retire takes memories out of what anybody is told, as replaced by by ("" =
// simply forgotten), and answers the ones it retired. Nothing is deleted:
// the owner can put one back until the sweep purges it.
func (s *Service) retire(botID string, idsIn []string, by string) []string {
	var out []string
	now := time.Now()
	for _, id := range idsIn {
		id = strings.TrimSpace(id)
		if id == "" || id == by {
			continue
		}
		res := s.db.Model(&db.Memory{}).Where("bot_id = ? AND id = ? AND status <> ?", botID, id, StatusRetired).
			Updates(map[string]any{"status": StatusRetired, "superseded_by": by, "retired_at": now})
		if res.Error == nil && res.RowsAffected == 1 {
			out = append(out, id)
		}
	}
	if len(out) > 0 {
		// A digest may still tell what was just retired: until the sweep
		// writes a new one, the prompt lists the newest memories instead.
		s.DropDigest(botID)
	}
	return out
}

// Remember is the remember tool: it saves one memory and says what became of it.
func (s *Service) Remember(ctx context.Context, m Entry) (string, error) {
	res, err := s.Save(ctx, m)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	switch {
	case res.Updated && res.Status == StatusTentative:
		fmt.Fprintf(&b, "noted again: %s is still tentative", res.ID)
	case res.Updated:
		fmt.Fprintf(&b, "updated memory %s (it already held this)", res.ID)
	case res.Status == StatusTentative:
		fmt.Fprintf(&b, "noted %s as tentative: it is kept out of the prompt until it is seen again", res.ID)
	default:
		b.WriteString("remembered " + res.ID)
	}
	if len(res.Retired) > 0 {
		fmt.Fprintf(&b, "; retired %s", strings.Join(res.Retired, ", "))
	}
	if len(res.Near) > 0 {
		b.WriteString("\nClose to it already (if the new one makes any of these out of date, `forget` it):\n")
		b.WriteString(formatDetailed(res.Near))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Search ranks the bot's memories against query, by meaning and by its
// words, without marking them used. tentative includes the guesses still
// waiting, which only the keeper and the owner see.
func (s *Service) Search(ctx context.Context, botID, query string, k int, tentative bool) ([]Recalled, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, models.ErrNoQuery
	}
	vecs, _, err := s.models.Embed(ctx, botID, []string{query})
	if err != nil {
		return nil, err
	}
	var args pgsearch.Args
	where := "m.bot_id = " + args.Add(botID)
	if tentative {
		where += " AND m.status <> " + args.Add(StatusRetired)
	} else {
		where += " AND m.status = " + args.Add(StatusActive)
	}
	stmt := pgsearch.Fused(&args, pgsearch.Source{
		From: "memories m", ID: "m.id", Embedding: "m.embedding", TSV: "m.tsv",
		Where: where, Candidates: searchCandidates,
	}, vecs[0], query) + `
SELECT m.id, m.content, m.created_at, m.kind, m.status, m.evidence, m.use_count, m.embedding <=> ` + args.Add(vecs[0]) + `
FROM fused f JOIN memories m ON m.id = f.id
ORDER BY f.score DESC, m.created_at DESC LIMIT ` + args.Add(k)
	var out []Recalled
	err = pgsearch.Run(ctx, s.db, stmt, &args, func(rows *sql.Rows) error {
		var r Recalled
		if err := rows.Scan(&r.ID, &r.Content, &r.CreatedAt, &r.Kind, &r.Status, &r.Evidence, &r.UseCount, &r.Distance); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

// Closest is the bot's live memories nearest a long text by meaning alone:
// what the keeper is shown beside a conversation.
func (s *Service) Closest(ctx context.Context, botID, text string, k int) ([]Recalled, error) {
	vecs, _, err := s.models.Embed(ctx, botID, []string{text})
	if err != nil {
		return nil, err
	}
	return s.nearest(botID, vecs[0], k, 0, true)
}

// Recall is a search by the Bot: what it returns counts as used.
func (s *Service) Recall(ctx context.Context, botID, query string, k int, tentative bool) ([]Recalled, error) {
	if k <= 0 {
		k = recallK
	}
	rows, err := s.Search(ctx, botID, query, min(k, recallMaxK), tentative)
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

// Forget is the forget tool: it retires one memory.
func (s *Service) Forget(botID, id string, keeper bool) (string, error) {
	id = strings.TrimSpace(id)
	if len(s.retire(botID, []string{id}, "")) == 0 {
		return "", fmt.Errorf("no memory %s", id)
	}
	if keeper {
		if rd, ok := s.reading.Load(botID); ok {
			rd.(*reading).forgot()
		}
	}
	return "forgot " + id, nil
}

// Delete removes one memory for good: the owner's delete.
func (s *Service) Delete(botID, id string) error {
	res := s.db.Where("bot_id = ? AND id = ?", botID, strings.TrimSpace(id)).Delete(&db.Memory{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("no memory %s", id)
	}
	// The owner deletes a memory because it is wrong or private, and a digest
	// may still tell it.
	s.DropDigest(botID)
	return nil
}

// Reinforce is the keeper saying a conversation leaned on these memories, or
// showed them true again. An active one counts a use; a tentative one gains a
// sighting when this is a separate one, and becomes active at PromoteAt.
func (s *Service) Reinforce(botID, runID string, idsIn []string) (string, error) {
	var rows []db.Memory
	s.db.Select("id, status, evidence, evidence_at").
		Where("bot_id = ? AND id IN ? AND status <> ?", botID, idsIn, StatusRetired).Find(&rows)
	if len(rows) == 0 {
		return "", errors.New("none of those ids is a memory of this Bot")
	}
	now := time.Now()
	used, seen, promoted := 0, 0, 0
	for _, r := range rows {
		up := map[string]any{"last_used_at": now}
		switch {
		case r.Status == StatusActive:
			up["use_count"] = gorm.Expr("use_count + 1")
			used++
		case seenAgain(r.EvidenceAt, now):
			up["evidence"], up["evidence_at"], up["run_id"] = r.Evidence+1, now, runID
			seen++
			if r.Evidence+1 >= PromoteAt {
				up["status"] = StatusActive
				promoted++
			}
		}
		s.db.Model(&db.Memory{}).Where("id = ?", r.ID).Updates(up)
	}
	return fmt.Sprintf("marked: %d used, %d tentative seen again (%d now active)", used, seen, promoted), nil
}

// line is a memory as the Bot reads it. UTC dates: the list sits in the
// prompt ahead of the conversation, so it must read the same wherever the
// Bot or the server is.
func line(r Recalled) string {
	return fmt.Sprintf("- [%s] (%s) %s\n", r.ID, r.CreatedAt.UTC().Format("2006-01-02"), r.Content)
}

func formatRecalled(rows []Recalled) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(line(r))
	}
	return b.String()
}

// formatDetailed is memories as whoever tends them reads them: with what
// kind each is, where it stands and how often it was seen and used.
func formatDetailed(rows []Recalled) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "- [%s] (%s) %s\n", r.ID, describe(r), r.Content)
	}
	return b.String()
}

func describe(r Recalled) string {
	parts := []string{r.CreatedAt.UTC().Format("2006-01-02"), r.Kind}
	if r.Status == StatusTentative {
		parts = append(parts, fmt.Sprintf("tentative, seen %d×", r.Evidence))
	}
	if r.UseCount > 0 {
		parts = append(parts, fmt.Sprintf("used %d×", r.UseCount))
	}
	if r.Kind == KindWork && time.Since(r.CreatedAt) > workStale {
		parts = append(parts, fmt.Sprintf("untouched for over %d days", int(workStale.Hours()/24)))
	}
	return strings.Join(parts, ", ")
}

// RecallTool is the recall chat tool: the matches as prose.
func (s *Service) RecallTool(ctx context.Context, botID, query string, k int, keeper bool) (string, error) {
	rows, err := s.Recall(ctx, botID, query, k, keeper)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "No memories match.", nil
	}
	if keeper {
		return formatDetailed(rows), nil
	}
	return formatRecalled(rows), nil
}

// --- the Memories page ---

const protoCols = "id, chat_id, content, created_at, last_used_at, kind, status, inferred, evidence, use_count, superseded_by, retired_at"

func (s *Service) ListMemories(ctx context.Context, req *connect.Request[v1.ListMemoriesRequest]) (*connect.Response[v1.ListMemoriesResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use); err != nil {
		return nil, err
	}
	var rows []db.Memory
	s.db.Select(protoCols).Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListMemoriesResponse{}
	for _, r := range rows {
		out.Memories = append(out.Memories, Proto(r))
	}
	return connect.NewResponse(out), nil
}

// Proto is the wire form of a memory.
func Proto(m db.Memory) *v1.Memory {
	return &v1.Memory{
		Id: m.ID, ChatId: m.ChatID, Content: m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339), LastUsedAt: textx.RFC3339(m.LastUsedAt),
		Kind: m.Kind, Status: m.Status, Inferred: m.Inferred, Evidence: int32(m.Evidence),
		UseCount: int32(m.UseCount), SupersededBy: m.SupersededBy, RetiredAt: textx.RFC3339(m.RetiredAt),
	}
}

// Protos are the memories with these ids, oldest first, in wire form.
func (s *Service) Protos(idsIn []string) []*v1.Memory {
	if len(idsIn) == 0 {
		return nil
	}
	var rows []db.Memory
	s.db.Select(protoCols).Where("id IN ?", idsIn).Order("created_at").Find(&rows)
	out := make([]*v1.Memory, len(rows))
	for i, r := range rows {
		out[i] = Proto(r)
	}
	return out
}

func (s *Service) SearchMemories(ctx context.Context, req *connect.Request[v1.SearchMemoriesRequest]) (*connect.Response[v1.SearchMemoriesResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.Bot(ctx, s.db, botID, access.Use); err != nil {
		return nil, err
	}
	k := int(req.Msg.GetLimit())
	if k <= 0 {
		k = searchK
	}
	rows, err := s.Search(ctx, botID, req.Msg.GetQuery(), min(k, searchMaxK), true)
	if errors.Is(err, models.ErrNoQuery) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	dist := map[string]float64{}
	found := make([]string, len(rows))
	for i, r := range rows {
		found[i], dist[r.ID] = r.ID, r.Distance
	}
	full := map[string]*v1.Memory{}
	for _, m := range s.Protos(found) {
		m.Distance = dist[m.Id]
		full[m.Id] = m
	}
	out := &v1.SearchMemoriesResponse{}
	for _, id := range found {
		if m := full[id]; m != nil {
			out.Memories = append(out.Memories, m)
		}
	}
	return connect.NewResponse(out), nil
}

func (s *Service) DeleteMemory(ctx context.Context, req *connect.Request[v1.DeleteMemoryRequest]) (*connect.Response[v1.DeleteMemoryResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	if err := s.Delete(req.Msg.GetBotId(), req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&v1.DeleteMemoryResponse{}), nil
}

// KeepMemory is the owner's word on a memory that is not active: a tentative
// one is kept as true, a retired one is put back.
func (s *Service) KeepMemory(ctx context.Context, req *connect.Request[v1.KeepMemoryRequest]) (*connect.Response[v1.Memory], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.Bot(ctx, s.db, botID, access.Manage); err != nil {
		return nil, err
	}
	res := s.db.Model(&db.Memory{}).Where("bot_id = ? AND id = ?", botID, req.Msg.GetId()).
		Updates(map[string]any{"status": StatusActive, "superseded_by": "", "retired_at": nil, "reviewed_at": nil})
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no memory %s", req.Msg.GetId()))
	}
	s.DropDigest(botID)
	out := s.Protos([]string{req.Msg.GetId()})
	return connect.NewResponse(out[0]), nil
}
