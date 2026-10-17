package artifact

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"silo.agent/internal/db"
)

// SweepTick is how often the artifact sweep runs.
const SweepTick = 10 * time.Minute

// Sweep forgets the artifacts no thread shows any more: a deleted chat, an
// automation's or a subagent's dropped log, a truncated message. An artifact
// lives while any artifact event of its Bot names it (a diverged chat's copy
// counts). Its pinned bytes are forgotten on the Bot's machine first, and the
// rows go only once that worked, so a Bot that is offline is retried later.
func (s *Service) Sweep(ctx context.Context) {
	var bots []string
	s.db.Model(&db.Artifact{}).Distinct("bot_id").Pluck("bot_id", &bots)
	for _, botID := range bots {
		s.SweepBot(ctx, botID)
	}
}

// SweepBot is Sweep for one Bot.
func (s *Service) SweepBot(ctx context.Context, botID string) {
	live := map[string]bool{}
	var bodies []string
	s.db.Model(&db.RunEvent{}).
		Where("kind = ? AND run_id IN (?)", "artifact", s.db.Model(&db.Run{}).Select("id").Where("bot_id = ?", botID)).
		Pluck("body", &bodies)
	for _, b := range bodies {
		var v struct {
			ArtifactID string `json:"artifact_id"`
		}
		if json.Unmarshal([]byte(b), &v) == nil && v.ArtifactID != "" {
			live[v.ArtifactID] = true
		}
	}
	var arts []db.Artifact
	s.db.Where("bot_id = ?", botID).Find(&arts)
	var dead []string
	for _, a := range arts {
		// A card is made before its event is written: give it a moment.
		if !live[a.ID] && time.Since(a.UpdatedAt) > time.Minute {
			dead = append(dead, a.ID)
		}
	}
	if len(dead) == 0 {
		return
	}
	var deadBlobs, keptBlobs []string
	s.db.Model(&db.ArtifactVersion{}).Where("artifact_id IN ? AND blob <> ''", dead).Distinct("blob").Pluck("blob", &deadBlobs)
	s.db.Model(&db.ArtifactVersion{}).Where("bot_id = ? AND artifact_id NOT IN ? AND blob <> ''", botID, dead).Distinct("blob").Pluck("blob", &keptBlobs)
	keep := map[string]bool{}
	for _, b := range keptBlobs {
		keep[b] = true
	}
	var forget []string
	for _, b := range deadBlobs {
		if !keep[b] {
			forget = append(forget, b)
		}
	}
	if err := s.forget(ctx, botID, forget); err != nil {
		log.Printf("artifacts: forget on bot %s: %v (retried later)", botID, err)
		return
	}
	s.db.Where("artifact_id IN ?", dead).Delete(&db.ArtifactVersion{})
	s.db.Where("id IN ?", dead).Delete(&db.Artifact{})
}
