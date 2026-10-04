package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/rag"
	"silo.agent/internal/textx"
)

const (
	// knowledgeMaxFiles caps one folder; the worker walk stops there.
	knowledgeMaxFiles = 5000
	// knowledgeEmbedBatch is how many chunks go to the embedder per call.
	knowledgeEmbedBatch = 64
	// knowledgeQuiet is how long a folder must be quiet after a write before
	// the sweep re-reads it, so a Bot writing ten files causes one sync.
	knowledgeQuiet = 15 * time.Second
	// knowledgeRetryError is how long a file whose extraction failed waits
	// before the sweep tries it again.
	knowledgeRetryError = time.Hour
	// knowledgeDriveSlowdown stretches the re-check interval of drive folders:
	// every look is a network round trip.
	knowledgeDriveSlowdown = 4
	// Tick is how often the sweep looks for folders that are due.
	Tick                 = 30 * time.Second
	knowledgePerTick     = 2
	knowledgePauseFor    = 15 * time.Minute
	knowledgeSyncTimeout = 45 * time.Minute
)

var errDriveEmpty = errors.New("this drive folder shows no files but was indexed before; the drive may be disconnected, so the index was kept")

// isDrivePath reports whether a workspace-relative path is on a mounted drive.
func isDrivePath(p string) bool {
	return p == "drives" || strings.HasPrefix(p, "drives/")
}

func (s *Service) folderLock(id string) *sync.Mutex {
	mu, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// walkEntry mirrors the worker's walk output.
type walkEntry struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

type extracted struct {
	Text      string `json:"text"`
	Sha256    string `json:"sha256"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Detail    string `json:"detail"`
}

// walkWorker lists a folder through the worker.
func (s *Service) walk(ctx context.Context, botID, path string, max int32) ([]walkEntry, bool, error) {
	raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_Walk{Walk: &v1.WalkCmd{Path: path, MaxFiles: max}}})
	if err != nil {
		return nil, false, err
	}
	var out struct {
		Files     []walkEntry `json:"files"`
		Truncated bool        `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false, err
	}
	return out.Files, out.Truncated, nil
}

func (s *Service) extract(ctx context.Context, botID, path string, ocr bool) (extracted, error) {
	raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_Extract{Extract: &v1.ExtractCmd{Path: path, Ocr: ocr}}})
	if err != nil {
		return extracted{}, err
	}
	var out extracted
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

// SyncFolder brings one folder's index up to date with the files on
// the Bot's box. One sync per folder runs at a time; a folder removed while it
// waits is a no-op. The folder's status and detail record the outcome.
func (s *Service) SyncFolder(ctx context.Context, id string) error {
	mu := s.folderLock(id)
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancel.Store(id, cancel)
	defer s.cancel.Delete(id)

	var f db.KnowledgeFolder
	if err := s.db.First(&f, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	s.db.Model(&f).Updates(map[string]any{"status": "syncing", "dirty_at": nil})
	note, err := s.syncFolder(ctx, &f)
	now := time.Now()
	upd := map[string]any{"last_sync_at": now}
	if err != nil {
		upd["status"], upd["detail"] = "error", textx.CapRunes(err.Error(), 400)
	} else {
		upd["status"], upd["detail"] = "idle", note
	}
	s.refreshFolderStats(f.ID, upd)
	return err
}

// refreshFolderStats writes the outcome plus fresh file/chunk counts.
func (s *Service) refreshFolderStats(folderID string, upd map[string]any) {
	var ok, skipped, chunks int64
	s.db.Model(&db.KnowledgeSource{}).Where("folder_id = ? AND status = 'ok'", folderID).Count(&ok)
	s.db.Model(&db.KnowledgeSource{}).Where("folder_id = ? AND status <> 'ok'", folderID).Count(&skipped)
	s.db.Model(&db.KnowledgeSource{}).Where("folder_id = ? AND status = 'ok'", folderID).
		Select("COALESCE(SUM(chunks), 0)").Scan(&chunks)
	upd["files"], upd["skipped"], upd["chunks"] = int(ok), int(skipped), int(chunks)
	s.db.Model(&db.KnowledgeFolder{}).Where("id = ?", folderID).Updates(upd)
}

// sourceUnchanged is the cheap check: same size and mtime means the file was
// not touched, so it is not read at all (on a drive that read is a download).
//
// A file that was reported as a scan while OCR was off is read again once OCR
// is on (ocrNow); one that OCR already looked at is not.
func sourceUnchanged(old *db.KnowledgeSource, wf walkEntry, model string, ocrNow bool, now time.Time) bool {
	if old.Size != wf.Size || old.Mtime != wf.Mtime {
		return false
	}
	if ocrNow && waitsForOCR(old) {
		return false
	}
	switch old.Status {
	case "ok":
		return old.EmbedModel == model
	case "skipped":
		return true
	default: // error: look again, but not on every sweep
		return now.Sub(old.IndexedAt) < knowledgeRetryError
	}
}

// ocrOffNote is how the worker words a scan it did not read; see ocr.go.
const ocrOffNote = "OCR is off"

func waitsForOCR(s *db.KnowledgeSource) bool { return strings.Contains(s.Detail, ocrOffNote) }

// syncFolder does the work; the caller records the outcome. The returned note
// is shown beside an otherwise healthy folder (a truncated walk).
func (s *Service) syncFolder(ctx context.Context, f *db.KnowledgeFolder) (string, error) {
	model := s.models.EmbedModelID()
	ocr := s.cfg().Knowledge.OCR
	files, truncated, err := s.walk(ctx, f.BotID, f.Path, knowledgeMaxFiles)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", f.Path, err)
	}
	var existing []db.KnowledgeSource
	s.db.Where("folder_id = ?", f.ID).Find(&existing)
	byPath := make(map[string]*db.KnowledgeSource, len(existing))
	for i := range existing {
		byPath[existing[i].Path] = &existing[i]
	}
	// A drive that comes up empty looks exactly like a deleted folder; only a
	// listing with files in it may remove anything.
	if len(files) == 0 && len(existing) > 0 && isDrivePath(f.Path) {
		return "", errDriveEmpty
	}

	seen := make(map[string]bool, len(files))
	var todo []walkEntry
	now := time.Now()
	for _, wf := range files {
		seen[wf.Path] = true
		if old := byPath[wf.Path]; old != nil && sourceUnchanged(old, wf, model, ocr, now) {
			continue
		}
		todo = append(todo, wf)
	}
	// Sources whose path is gone may be renames: a new file with the same hash
	// takes over their chunks instead of being embedded again.
	missing := map[string]*db.KnowledgeSource{}
	byHash := map[string]*db.KnowledgeSource{}
	for i := range existing {
		s := &existing[i]
		if seen[s.Path] {
			continue
		}
		missing[s.ID] = s
		if s.Status == "ok" && s.EmbedModel == model && s.Hash != "" {
			byHash[s.Hash] = s
		}
	}

	rel := func(p string) string { return strings.TrimPrefix(p, f.Path+"/") }
	for _, wf := range todo {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		old := byPath[wf.Path]
		ex, err := s.extract(ctx, f.BotID, wf.Path, ocr)
		if err != nil {
			// A worker that went away ends the sweep; a file that cannot be
			// read is that file's problem.
			if ctx.Err() != nil || !s.hub.Connected(f.BotID) || errors.Is(err, hub.ErrNoWorker) {
				return "", fmt.Errorf("the Bot's machine went away: %w", err)
			}
			s.saveSource(f, old, wf, model, "error", "", textx.CapRunes(err.Error(), 300), nil)
			continue
		}
		if ex.Kind == "skipped" {
			s.saveSource(f, old, wf, model, "skipped", ex.Sha256, ex.Detail, nil)
			continue
		}
		if old != nil && old.Status == "ok" && old.Hash == ex.Sha256 && old.EmbedModel == model && !(ocr && waitsForOCR(old)) {
			s.db.Model(old).Updates(map[string]any{"size": wf.Size, "mtime": wf.Mtime})
			continue
		}
		if old == nil {
			if src := byHash[ex.Sha256]; src != nil {
				s.db.Model(src).Updates(map[string]any{"path": wf.Path, "size": wf.Size, "mtime": wf.Mtime})
				delete(missing, src.ID)
				delete(byHash, ex.Sha256)
				continue
			}
		}
		chunks := rag.Split(ex.Text)
		if len(chunks) == 0 {
			detail := ex.Detail
			if detail == "" {
				detail = "no text found"
			}
			s.saveSource(f, old, wf, model, "skipped", ex.Sha256, detail, nil)
			continue
		}
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.EmbedText(rel(wf.Path))
		}
		vecs, err := s.embedBatches(ctx, texts)
		if err != nil {
			return "", models.ProviderError{Err: err}
		}
		detail := ex.Detail
		if ex.Truncated {
			if detail != "" {
				detail += "; "
			}
			detail += "only the first part of this file is indexed"
		}
		s.saveSource(f, old, wf, model, "ok", ex.Sha256, detail, &indexed{chunks: chunks, vecs: vecs})
	}

	if len(missing) > 0 {
		gone := make([]string, 0, len(missing))
		for id := range missing {
			gone = append(gone, id)
		}
		s.db.Transaction(func(tx *gorm.DB) error {
			tx.Where("source_id IN ?", gone).Delete(&db.KnowledgeChunk{})
			return tx.Where("id IN ?", gone).Delete(&db.KnowledgeSource{}).Error
		})
	}
	if truncated {
		return fmt.Sprintf("Only the first %d files are indexed; pick a smaller folder.", knowledgeMaxFiles), nil
	}
	return "", nil
}

// embedBatches embeds texts in batches of knowledgeEmbedBatch.
func (s *Service) embedBatches(ctx context.Context, texts []string) ([]pgvector.Vector, error) {
	out := make([]pgvector.Vector, 0, len(texts))
	for i := 0; i < len(texts); i += knowledgeEmbedBatch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(i+knowledgeEmbedBatch, len(texts))
		vecs, _, err := s.models.Embed(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		if len(vecs) != end-i {
			return nil, fmt.Errorf("embed: got %d vectors for %d texts", len(vecs), end-i)
		}
		out = append(out, vecs...)
	}
	return out, nil
}

type indexed struct {
	chunks []rag.Chunk
	vecs   []pgvector.Vector
}

// saveSource writes one file's row and, when ix is set, replaces its chunks
// in the same transaction. An "error" keeps the chunks it had; "skipped" drops
// them (the file is no longer searchable text).
func (s *Service) saveSource(f *db.KnowledgeFolder, old *db.KnowledgeSource, wf walkEntry, model, status, hash, detail string, ix *indexed) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		src := db.KnowledgeSource{ID: ids.New(), BotID: f.BotID, FolderID: f.ID, Path: wf.Path}
		if old != nil {
			src = *old
		}
		if status != "error" {
			if err := tx.Where("source_id = ?", src.ID).Delete(&db.KnowledgeChunk{}).Error; err != nil {
				return err
			}
			src.Chunks = 0
		}
		src.Size, src.Mtime, src.EmbedModel = wf.Size, wf.Mtime, model
		src.Status, src.Detail, src.IndexedAt = status, detail, time.Now()
		if status != "error" {
			src.Hash = hash
		}
		if ix != nil {
			rows := make([]db.KnowledgeChunk, len(ix.chunks))
			for i, c := range ix.chunks {
				rows[i] = db.KnowledgeChunk{ID: ids.New(), BotID: f.BotID, SourceID: src.ID, Ord: i, Locator: c.Locator, Content: c.Text, Embedding: ix.vecs[i]}
			}
			if err := tx.CreateInBatches(rows, 100).Error; err != nil {
				return err
			}
			src.Chunks = len(rows)
		}
		return tx.Save(&src).Error
	})
	if err != nil {
		log.Printf("knowledge: save %s: %v", wf.Path, err)
	}
}

// --- sweep ---

// Recover clears "syncing" left by a CP that stopped mid-sync.
func (s *Service) Recover() {
	s.db.Model(&db.KnowledgeFolder{}).Where("status = 'syncing'").Update("status", "idle")
}

// Sweep syncs the folders that are due — written to and then quiet,
// or past their interval — at most knowledgePerTick of them, skipping Bots
// whose machine is down (a sweep never starts a box). It returns how many it
// synced.
func (s *Service) Sweep(now time.Time) int {
	cfg := s.cfg().Knowledge
	if s.db == nil || !cfg.Enabled || now.UnixNano() < s.pause.Load() {
		return 0
	}
	var folders []db.KnowledgeFolder
	s.db.Where("status <> 'syncing'").Order("last_sync_at ASC NULLS FIRST").Find(&folders)
	done := 0
	for _, f := range folders {
		if done == knowledgePerTick {
			break
		}
		interval := cfg.Interval()
		if isDrivePath(f.Path) {
			interval *= knowledgeDriveSlowdown
		}
		dirty := f.DirtyAt != nil && now.Sub(*f.DirtyAt) >= knowledgeQuiet
		stale := f.LastSyncAt == nil || now.Sub(*f.LastSyncAt) >= interval
		if (!dirty && !stale) || !s.hub.Connected(f.BotID) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), knowledgeSyncTimeout)
		err := s.SyncFolder(ctx, f.ID)
		cancel()
		done++
		if err != nil {
			log.Printf("knowledge: folder %s (%s): %v", f.Path, f.BotID, err)
			var pe models.ProviderError
			if errors.As(err, &pe) {
				s.pause.Store(now.Add(knowledgePauseFor).UnixNano())
				return done
			}
		}
	}
	return done
}

// Dirty notes that something wrote under (or removed) path, so
// the sweep re-reads any folder it touches once things go quiet.
func (s *Service) Dirty(botID, path string) {
	p := strings.Trim(workspace.Rel(path), "/")
	if s.db == nil || p == "" {
		return
	}
	s.db.Model(&db.KnowledgeFolder{}).
		Where("bot_id = ? AND (path = ? OR ?::text LIKE path || '/%' OR path LIKE ?::text || '/%')", botID, p, p, p).
		Update("dirty_at", time.Now())
}

// dropFolder stops a folder's sync and deletes its rows.
func (s *Service) dropFolder(id string) {
	if cancel, ok := s.cancel.Load(id); ok {
		cancel.(context.CancelFunc)()
	}
	mu := s.folderLock(id)
	mu.Lock()
	defer mu.Unlock()
	s.db.Where("source_id IN (?)", s.db.Model(&db.KnowledgeSource{}).Select("id").Where("folder_id = ?", id)).Delete(&db.KnowledgeChunk{})
	s.db.Where("folder_id = ?", id).Delete(&db.KnowledgeSource{})
	s.db.Where("id = ?", id).Delete(&db.KnowledgeFolder{})
	s.locks.Delete(id)
}

// Drop removes every folder of a Bot (DeleteBot).
func (s *Service) Drop(botID string) {
	var ids []string
	s.db.Model(&db.KnowledgeFolder{}).Where("bot_id = ?", botID).Pluck("id", &ids)
	for _, id := range ids {
		s.dropFolder(id)
	}
	// A sync that raced the delete may have left rows behind.
	s.db.Where("bot_id = ?", botID).Delete(&db.KnowledgeChunk{})
	s.db.Where("bot_id = ?", botID).Delete(&db.KnowledgeSource{})
	s.db.Where("bot_id = ?", botID).Delete(&db.KnowledgeFolder{})
}

// startSync runs a sync in the background (Add and Sync now).
func (s *Service) startSync(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), knowledgeSyncTimeout)
		defer cancel()
		if err := s.SyncFolder(ctx, id); err != nil {
			log.Printf("knowledge: sync %s: %v", id, err)
		}
	}()
}
