package changes

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// Drives are not snapshotted: listing one is network calls and reading one is
// a download of the owner's other data. They get a journal instead. Whatever
// writes to a drive goes through its FUSE mount, so the drive sidecar reads
// each write, delete and rename off rclone's log (internal/drivehost/journal.go)
// and reports it here. A line says what happened, never what was in the file;
// getting a file back is the provider's version history or trash.
const (
	// DriveKeep is how many journal lines a Bot keeps; the oldest fall off.
	DriveKeep = 2000
	// driveList is how many the pane shows.
	driveList = 300
	// driveTrimEvery spaces the trims out: one per so many recorded lines.
	driveTrimEvery = 200
	// driveGrace is how long after a run ends a drive change is still put down
	// to it: rclone uploads a file a few seconds after it is closed.
	driveGrace = 45 * time.Second
	drivePath  = 1024
)

var driveOps = map[string]bool{"added": true, "modified": true, "deleted": true, "renamed": true}

// RecordDrive files what a drive's sidecar saw happen on its mount. The runs at
// work now (or that ended a moment ago) are put on each line.
func (s *Service) RecordDrive(d *db.Drive, changes []*v1.DriveChange) {
	if s.db == nil || !s.enabled() || d == nil || len(changes) == 0 {
		return
	}
	sources := ""
	if who := s.recentlyWorking(d.BotID); len(who) > 0 {
		if raw, err := json.Marshal(who); err == nil {
			sources = string(raw)
		}
	}
	now := time.Now()
	rows := make([]db.DriveChange, 0, len(changes))
	for _, c := range changes {
		path := cleanDrivePath(c.GetPath())
		if !driveOps[c.GetOp()] || path == "" {
			continue
		}
		at := time.UnixMilli(c.GetAt())
		// A sidecar's clock is not trusted far: a line dated in the future or
		// long ago is dated now.
		if c.GetAt() <= 0 || at.After(now.Add(time.Minute)) || at.Before(now.Add(-24*time.Hour)) {
			at = now
		}
		rows = append(rows, db.DriveChange{
			ID: ids.New(), BotID: d.BotID, DriveID: d.ID, Drive: d.Name, Op: c.GetOp(),
			Path: path, OldPath: cleanDrivePath(c.GetOldPath()), Size: max(c.GetSize(), 0), Sources: sources, At: at,
		})
	}
	if len(rows) == 0 {
		return
	}
	if err := s.db.CreateInBatches(&rows, 200).Error; err != nil {
		log.Printf("changes: drive journal bot=%s: %v", d.BotID, err)
		return
	}
	s.mu.Lock()
	s.driveSeen[d.BotID] += len(rows)
	trim := s.driveSeen[d.BotID] >= driveTrimEvery
	if trim {
		s.driveSeen[d.BotID] = 0
	}
	s.mu.Unlock()
	if trim {
		s.trimDrive(d.BotID)
	}
}

// cleanDrivePath keeps a path a sidecar reported printable and bounded: it is
// shown as is.
func cleanDrivePath(p string) string {
	p = strings.Trim(strings.ToValidUTF8(p, "�"), "/")
	if r := []rune(p); len(r) > drivePath {
		p = "…" + string(r[len(r)-drivePath:])
	}
	return p
}

// trimDrive drops a Bot's journal lines past DriveKeep.
func (s *Service) trimDrive(botID string) {
	var cut db.DriveChange
	s.db.Select("seq").Where("bot_id = ?", botID).Order("seq DESC").Offset(DriveKeep).Limit(1).Find(&cut)
	if cut.Seq > 0 {
		s.db.Where("bot_id = ? AND seq <= ?", botID, cut.Seq).Delete(&db.DriveChange{})
	}
}

// Drop forgets a Bot's drive journal (the Bot is being deleted). Its workspace
// history goes with its data directory.
func (s *Service) Drop(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.DriveChange{})
	s.mu.Lock()
	delete(s.driveSeen, botID)
	delete(s.ended, botID)
	s.mu.Unlock()
}

func (s *Service) ListDriveChanges(ctx context.Context, req *connect.Request[v1.ListDriveChangesRequest]) (*connect.Response[v1.ListDriveChangesResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	out := &v1.ListDriveChangesResponse{State: "ok", Keep: DriveKeep}
	if !s.enabled() {
		out.State = "off"
		return connect.NewResponse(out), nil
	}
	var drives int64
	s.db.Model(&db.Drive{}).Where("bot_id = ? AND draft = ?", b.ID, false).Count(&drives)
	out.HasDrives = drives > 0
	var rows []db.DriveChange
	if err := s.db.Where("bot_id = ?", b.ID).Order("seq DESC").Limit(driveList).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		e := &v1.DriveChangeEntry{Id: r.ID, Drive: r.Drive, Op: r.Op, Path: r.Path, OldPath: r.OldPath, Size: r.Size, At: r.At.UTC().Format(time.RFC3339)}
		var sources []Source
		if r.Sources != "" {
			_ = json.Unmarshal([]byte(r.Sources), &sources)
		}
		for _, src := range sources {
			e.Sources = append(e.Sources, &v1.ChangeSource{Kind: src.Kind, Name: src.Name, ChatId: src.ChatID})
		}
		out.Changes = append(out.Changes, e)
	}
	return connect.NewResponse(out), nil
}
