package memory

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/prompts"
)

// The keeper is the Bot's memory tended in the background: a pinned
// automation ("Memory") whose runs go through the same loop, rules and log as
// any run, narrowed to the memory tools, the memory model and a prompt of its
// own (run.Profile), and starting no machine. It does two jobs:
//
//   - reading: the App hands it a stretch of a conversation that went quiet
//     (Read), and it saves what a later conversation would miss;
//   - tidying: fired with its own prompt (by Tend, the owner's schedule or
//     Run now), it looks the store over in batches (Review) and merges,
//     replaces and drops.
const (
	// KeeperKind is the pinned automation's kind.
	KeeperKind = "memory"

	// A review batch is as many memories as fit the room the keeper's model
	// has for it (Service.Room, in bytes: the store is mostly looked over in
	// one batch), at most reviewSeeds, each with up to reviewNear neighbours
	// no farther than reviewDistance; a run takes at most reviewBatches.
	// ReviewRoomMin is the room when nobody says, ReviewRoomMax the most.
	reviewSeeds    = 400
	reviewNear     = 3
	reviewDistance = 0.4
	reviewBatches  = 3
	ReviewRoomMin  = 10_000
	ReviewRoomMax  = 200_000
	// tentativeStale is how long a guess waits before it is looked over again.
	tentativeStale = 30 * 24 * time.Hour
	// workStale is how long a work entry stands untouched before it is looked
	// over again, to be folded into a fact or let go.
	workStale = 30 * 24 * time.Hour
	// tidyMin is how many memories must wait to be looked over before the
	// keeper tidies unasked, and tidyEvery the least time between two tidies.
	tidyMin   = 20
	tidyEvery = 6 * time.Hour
	tidyWait  = 15 * time.Minute
)

// KeeperTools are the tools the keeper's run is offered.
var KeeperTools = []string{"remember", "recall", "forget", "reinforce", "review_memories"}

// Jobs is the part of the automations the keeper runs on.
type Jobs interface {
	Pin(automation.Pinned)
	Do(ctx context.Context, botID, kind, text string) (runID, status string, err error)
}

// ErrBusy is what Read answers while the keeper is at work for the Bot.
var ErrBusy = errors.New("memory is being tended for this Bot; try again in a moment")

func (s *Service) keeper() automation.Pinned {
	return automation.Pinned{
		Kind: KeeperKind, Name: "Memory", Prompt: prompts.MemoryTidy,
		Profile: &run.Profile{
			Name:   KeeperKind,
			Prompt: s.keeperPrompt,
			Tools:  KeeperTools,
			Model:  func() string { return s.cfg().MemoryModel() },
		},
		// The sweep reads quiet chats and Tend tidies a pile-up, both only
		// while the operator has collecting on.
		OnItsOwn: func() bool { return s.cfg().Memory.Collect },
	}
}

// keeperPrompt is the keeper's system prompt: the same text for every Bot,
// with the part about working things out only while the operator allows it.
func (s *Service) keeperPrompt() string {
	infer := prompts.MemoryStated
	if s.cfg().Memory.Infer {
		infer = prompts.MemoryInfer
	}
	return strings.ReplaceAll(prompts.Memory, "{infer}", strings.TrimSpace(infer))
}

// Tally is what the keeper did with one stretch of a conversation.
type Tally struct {
	Saved, Updated, Forgotten int
	// IDs are the memories it saved or updated.
	IDs []string
}

// reading is the conversation the keeper is reading for a Bot.
type reading struct {
	mu     sync.Mutex
	chatID string
	tally  Tally
}

// count files what a save became. A nil reading counts nothing.
func (r *reading) count(out Saved) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if out.Updated || len(out.Retired) > 0 {
		r.tally.Updated++
	} else {
		r.tally.Saved++
	}
	r.tally.IDs = append(r.tally.IDs, out.ID)
}

func (r *reading) forgot() {
	r.mu.Lock()
	r.tally.Forgotten++
	r.mu.Unlock()
}

// readingFor is the conversation a save by the keeper's run belongs to.
func (s *Service) readingFor(m Entry) *reading {
	if !m.Keeper {
		return nil
	}
	if rd, ok := s.reading.Load(m.BotID); ok {
		return rd.(*reading)
	}
	return nil
}

// Read hands the keeper one stretch of a conversation (text, as Excerpt wrote
// it) and waits for its run. It answers what the keeper did and the status
// the run ended with ("" when ctx ended first); ErrBusy while the keeper is
// already at work for the Bot.
func (s *Service) Read(ctx context.Context, botID, chatID, text string) (Tally, string, error) {
	if s.jobs == nil {
		return Tally{}, "", errors.New("memory is not tended here")
	}
	rd := &reading{chatID: chatID}
	if _, busy := s.reading.LoadOrStore(botID, rd); busy {
		return Tally{}, "", ErrBusy
	}
	defer s.reading.Delete(botID)
	_, status, err := s.jobs.Do(ctx, botID, KeeperKind, text)
	if errors.Is(err, automation.ErrBusy) {
		return Tally{}, "", ErrBusy
	}
	rd.mu.Lock()
	defer rd.mu.Unlock()
	return rd.tally, status, err
}

// Excerpt is the opening of a reading run: the memories closest to the
// conversation, as the keeper needs them to tell what is new, then the
// conversation itself, last.
func Excerpt(shown []Recalled, excerpt, note string) string {
	var b strings.Builder
	b.WriteString("Read this conversation and keep what is worth keeping.\n\nAlready in memory (closest to this excerpt):\n")
	if len(shown) == 0 {
		b.WriteString("(none)\n")
	}
	b.WriteString(formatDetailed(shown))
	fmt.Fprintf(&b, "\nConversation excerpt%s:\n\n%s", note, excerpt)
	return b.String()
}

// unreviewed narrows a query to the memories waiting to be looked over: new
// or reworded ones, guesses nothing has supported for a while, and work
// nobody has touched for a while.
func unreviewed(q *gorm.DB, now time.Time) *gorm.DB {
	stale := now.Add(-workStale)
	return q.Where("status <> ? AND (reviewed_at IS NULL OR (status = ? AND reviewed_at < ?) OR (kind = ? AND created_at < ? AND reviewed_at < ?))",
		StatusRetired, StatusTentative, now.Add(-tentativeStale), KindWork, stale, stale)
}

// batch is how many review batches a keeper run has taken.
type batch struct {
	run string
	n   int
}

// Review is the review_memories tool: the next batch of memories to look
// over, each with the ones closest to it, so duplicates and contradictions
// sit side by side. What it hands out counts as looked over.
func (s *Service) Review(botID, runID string) (string, error) {
	b, _ := s.batches.Load(botID)
	cur, _ := b.(batch)
	if cur.run != runID {
		cur = batch{run: runID}
	}
	if cur.n >= reviewBatches {
		return "That is enough for one run. The rest waits for the next.", nil
	}
	now := time.Now()
	var seeds []db.Memory
	unreviewed(s.db.Select(recalledCols+", embedding").Where("bot_id = ?", botID), now).
		Order("created_at, id").Limit(reviewSeeds).Find(&seeds)
	if len(seeds) == 0 {
		return "Nothing left to look over.", nil
	}
	// Only what fits the room is shown, and only what is shown counts as
	// looked over: the rest is the next batch's.
	room := s.room()
	var seedIDs []string
	var out strings.Builder
	for _, m := range seeds {
		var one strings.Builder
		one.WriteString(formatDetailed([]Recalled{recalled(m)}))
		near, err := s.nearest(botID, m.Embedding, reviewNear+1, reviewDistance, true)
		if err != nil {
			return "", err
		}
		for _, n := range near {
			if n.ID != m.ID {
				one.WriteString("    close to it: " + strings.TrimPrefix(formatDetailed([]Recalled{n}), "- "))
			}
		}
		if len(seedIDs) > 0 && out.Len()+one.Len() > room {
			break
		}
		seedIDs = append(seedIDs, m.ID)
		out.WriteString(one.String())
	}
	for i := 0; i < len(seedIDs); i += 500 {
		s.db.Model(&db.Memory{}).Where("id IN ?", seedIDs[i:min(i+500, len(seedIDs))]).Update("reviewed_at", now)
	}
	cur.n++
	s.batches.Store(botID, cur)
	var left int64
	unreviewed(s.db.Model(&db.Memory{}).Where("bot_id = ?", botID), now).Count(&left)
	return fmt.Sprintf("%d to look over (%d more wait after these):\n%s", len(seedIDs), left, out.String()), nil
}

// room is how many bytes one review batch may fill.
func (s *Service) room() int {
	if s.Room == nil {
		return ReviewRoomMin
	}
	return min(max(s.Room(), ReviewRoomMin), ReviewRoomMax)
}

func recalled(m db.Memory) Recalled {
	return Recalled{ID: m.ID, Content: m.Content, CreatedAt: m.CreatedAt, Kind: m.Kind, Status: m.Status, Evidence: m.Evidence, UseCount: m.UseCount}
}

// Tend is the keeper's own sweep: it purges what was kept only to be put
// back, and has the keeper tidy one Bot whose memories have piled up
// unlooked-at. It returns how many tidies it ran.
func (s *Service) Tend(ctx context.Context, now time.Time) int {
	if s.db == nil || s.jobs == nil {
		return 0
	}
	s.purge(now)
	if !s.cfg().Memory.Collect {
		return 0
	}
	var bots []string
	unreviewed(s.db.Model(&db.Memory{}), now).Select("bot_id").Group("bot_id").
		Having("COUNT(*) >= ?", tidyMin).Order("bot_id").Scan(&bots)
	for _, botID := range bots {
		if last, ok := s.tidied.Load(botID); ok && now.Sub(last.(time.Time)) < tidyEvery {
			continue
		}
		s.tidied.Store(botID, now)
		tctx, cancel := context.WithTimeout(ctx, tidyWait)
		_, status, err := s.jobs.Do(tctx, botID, KeeperKind, "")
		cancel()
		if errors.Is(err, automation.ErrBusy) {
			s.tidied.Delete(botID) // it is reading a conversation: next tick
			continue
		}
		if err != nil {
			log.Printf("memory keeper: tidy bot %s: %v", botID, err)
			continue
		}
		if status != "done" {
			log.Printf("memory keeper: tidy bot %s ended %q", botID, status)
		}
		return 1 // one tidy a tick: the sweep waits on each
	}
	return 0
}

// purge deletes what nobody can be told any more: memories retired longer
// ago than retiredKeep, and guesses nothing supported in tentativeKeep.
func (s *Service) purge(now time.Time) {
	s.db.Where("status = ? AND retired_at < ?", StatusRetired, now.Add(-retiredKeep)).Delete(&db.Memory{})
	s.db.Where("status = ? AND evidence < ? AND created_at < ?", StatusTentative, PromoteAt, now.Add(-tentativeKeep)).Delete(&db.Memory{})
}
