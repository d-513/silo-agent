package app

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
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/rag"
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
	knowledgeTick          = 30 * time.Second
	knowledgePerTick       = 2
	knowledgePauseFor      = 15 * time.Minute
	knowledgeSyncTimeout   = 45 * time.Minute
)

var errDriveEmpty = errors.New("this drive folder shows no files but was indexed before; the drive may be disconnected, so the index was kept")

// isDrivePath reports whether a workspace-relative path is on a mounted drive.
func isDrivePath(p string) bool {
	return p == "drives" || strings.HasPrefix(p, "drives/")
}

func (a *App) folderLock(id string) *sync.Mutex {
	mu, _ := a.knowLocks.LoadOrStore(id, &sync.Mutex{})
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
func (a *App) walkWorker(ctx context.Context, botID, path string, max int32) ([]walkEntry, bool, error) {
	raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_Walk{Walk: &v1.WalkCmd{Path: path, MaxFiles: max}}})
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

func (a *App) extractWorker(ctx context.Context, botID, path string, ocr bool) (extracted, error) {
	raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_Extract{Extract: &v1.ExtractCmd{Path: path, Ocr: ocr}}})
	if err != nil {
		return extracted{}, err
	}
	var out extracted
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

// SyncKnowledgeFolder brings one folder's index up to date with the files on
// the Bot's box. One sync per folder runs at a time; a folder removed while it
// waits is a no-op. The folder's status and detail record the outcome.
func (a *App) SyncKnowledgeFolder(ctx context.Context, id string) error {
	mu := a.folderLock(id)
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.knowCancel.Store(id, cancel)
	defer a.knowCancel.Delete(id)

	var f db.KnowledgeFolder
	if err := a.DB.First(&f, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	a.DB.Model(&f).Updates(map[string]any{"status": "syncing", "dirty_at": nil})
	note, err := a.syncFolder(ctx, &f)
	now := time.Now()
	upd := map[string]any{"last_sync_at": now}
	if err != nil {
		upd["status"], upd["detail"] = "error", capRunes(err.Error(), 400)
	} else {
		upd["status"], upd["detail"] = "idle", note
	}
	a.refreshFolderStats(f.ID, upd)
	return err
}

// refreshFolderStats writes the outcome plus fresh file/chunk counts.
func (a *App) refreshFolderStats(folderID string, upd map[string]any) {
	var ok, skipped, chunks int64
	a.DB.Model(&db.KnowledgeSource{}).Where("folder_id = ? AND status = 'ok'", folderID).Count(&ok)
	a.DB.Model(&db.KnowledgeSource{}).Where("folder_id = ? AND status <> 'ok'", folderID).Count(&skipped)
	a.DB.Model(&db.KnowledgeSource{}).Where("folder_id = ? AND status = 'ok'", folderID).
		Select("COALESCE(SUM(chunks), 0)").Scan(&chunks)
	upd["files"], upd["skipped"], upd["chunks"] = int(ok), int(skipped), int(chunks)
	a.DB.Model(&db.KnowledgeFolder{}).Where("id = ?", folderID).Updates(upd)
}

func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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
func (a *App) syncFolder(ctx context.Context, f *db.KnowledgeFolder) (string, error) {
	model := a.embedModelID()
	ocr := a.cfg().Knowledge.OCR
	files, truncated, err := a.walkWorker(ctx, f.BotID, f.Path, knowledgeMaxFiles)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", f.Path, err)
	}
	var existing []db.KnowledgeSource
	a.DB.Where("folder_id = ?", f.ID).Find(&existing)
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
		ex, err := a.extractWorker(ctx, f.BotID, wf.Path, ocr)
		if err != nil {
			// A worker that went away ends the sweep; a file that cannot be
			// read is that file's problem.
			if ctx.Err() != nil || !a.Hub.Connected(f.BotID) || errors.Is(err, hub.ErrNoWorker) {
				return "", fmt.Errorf("the Bot's machine went away: %w", err)
			}
			a.saveSource(f, old, wf, model, "error", "", capRunes(err.Error(), 300), nil)
			continue
		}
		if ex.Kind == "skipped" {
			a.saveSource(f, old, wf, model, "skipped", ex.Sha256, ex.Detail, nil)
			continue
		}
		if old != nil && old.Status == "ok" && old.Hash == ex.Sha256 && old.EmbedModel == model && !(ocr && waitsForOCR(old)) {
			a.DB.Model(old).Updates(map[string]any{"size": wf.Size, "mtime": wf.Mtime})
			continue
		}
		if old == nil {
			if src := byHash[ex.Sha256]; src != nil {
				a.DB.Model(src).Updates(map[string]any{"path": wf.Path, "size": wf.Size, "mtime": wf.Mtime})
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
			a.saveSource(f, old, wf, model, "skipped", ex.Sha256, detail, nil)
			continue
		}
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.EmbedText(rel(wf.Path))
		}
		vecs, err := a.embedBatches(ctx, texts)
		if err != nil {
			return "", providerError{err}
		}
		detail := ex.Detail
		if ex.Truncated {
			if detail != "" {
				detail += "; "
			}
			detail += "only the first part of this file is indexed"
		}
		a.saveSource(f, old, wf, model, "ok", ex.Sha256, detail, &indexed{chunks: chunks, vecs: vecs})
	}

	if len(missing) > 0 {
		gone := make([]string, 0, len(missing))
		for id := range missing {
			gone = append(gone, id)
		}
		a.DB.Transaction(func(tx *gorm.DB) error {
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
func (a *App) embedBatches(ctx context.Context, texts []string) ([]pgvector.Vector, error) {
	out := make([]pgvector.Vector, 0, len(texts))
	for i := 0; i < len(texts); i += knowledgeEmbedBatch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(i+knowledgeEmbedBatch, len(texts))
		vecs, _, err := a.embed(ctx, texts[i:end])
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
func (a *App) saveSource(f *db.KnowledgeFolder, old *db.KnowledgeSource, wf walkEntry, model, status, hash, detail string, ix *indexed) {
	err := a.DB.Transaction(func(tx *gorm.DB) error {
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

func (a *App) knowledgeLoop(stop <-chan struct{}) {
	t := time.NewTicker(knowledgeTick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			a.SweepKnowledge(now)
		}
	}
}

// recoverKnowledge clears "syncing" left by a CP that stopped mid-sync.
func (a *App) recoverKnowledge() {
	a.DB.Model(&db.KnowledgeFolder{}).Where("status = 'syncing'").Update("status", "idle")
}

// SweepKnowledge syncs the folders that are due — written to and then quiet,
// or past their interval — at most knowledgePerTick of them, skipping Bots
// whose machine is down (a sweep never starts a box). It returns how many it
// synced.
func (a *App) SweepKnowledge(now time.Time) int {
	cfg := a.cfg().Knowledge
	if a.DB == nil || !cfg.Enabled || now.UnixNano() < a.knowPause.Load() {
		return 0
	}
	var folders []db.KnowledgeFolder
	a.DB.Where("status <> 'syncing'").Order("last_sync_at ASC NULLS FIRST").Find(&folders)
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
		if (!dirty && !stale) || !a.Hub.Connected(f.BotID) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), knowledgeSyncTimeout)
		err := a.SyncKnowledgeFolder(ctx, f.ID)
		cancel()
		done++
		if err != nil {
			log.Printf("knowledge: folder %s (%s): %v", f.Path, f.BotID, err)
			var pe providerError
			if errors.As(err, &pe) {
				a.knowPause.Store(now.Add(knowledgePauseFor).UnixNano())
				return done
			}
		}
	}
	return done
}

// markKnowledgeDirty notes that something wrote under (or removed) path, so
// the sweep re-reads any folder it touches once things go quiet.
func (a *App) markKnowledgeDirty(botID, path string) {
	p := strings.Trim(relWorkspace(path), "/")
	if a.DB == nil || p == "" {
		return
	}
	a.DB.Model(&db.KnowledgeFolder{}).
		Where("bot_id = ? AND (path = ? OR ?::text LIKE path || '/%' OR path LIKE ?::text || '/%')", botID, p, p, p).
		Update("dirty_at", time.Now())
}

// dropKnowledgeFolder stops a folder's sync and deletes its rows.
func (a *App) dropKnowledgeFolder(id string) {
	if cancel, ok := a.knowCancel.Load(id); ok {
		cancel.(context.CancelFunc)()
	}
	mu := a.folderLock(id)
	mu.Lock()
	defer mu.Unlock()
	a.DB.Where("source_id IN (?)", a.DB.Model(&db.KnowledgeSource{}).Select("id").Where("folder_id = ?", id)).Delete(&db.KnowledgeChunk{})
	a.DB.Where("folder_id = ?", id).Delete(&db.KnowledgeSource{})
	a.DB.Where("id = ?", id).Delete(&db.KnowledgeFolder{})
	a.knowLocks.Delete(id)
}

// dropKnowledge removes every folder of a Bot (DeleteBot).
func (a *App) dropKnowledge(botID string) {
	var ids []string
	a.DB.Model(&db.KnowledgeFolder{}).Where("bot_id = ?", botID).Pluck("id", &ids)
	for _, id := range ids {
		a.dropKnowledgeFolder(id)
	}
	// A sync that raced the delete may have left rows behind.
	a.DB.Where("bot_id = ?", botID).Delete(&db.KnowledgeChunk{})
	a.DB.Where("bot_id = ?", botID).Delete(&db.KnowledgeSource{})
	a.DB.Where("bot_id = ?", botID).Delete(&db.KnowledgeFolder{})
}

// startKnowledgeSync runs a sync in the background (Add and Sync now).
func (a *App) startKnowledgeSync(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), knowledgeSyncTimeout)
		defer cancel()
		if err := a.SyncKnowledgeFolder(ctx, id); err != nil {
			log.Printf("knowledge: sync %s: %v", id, err)
		}
	}()
}
