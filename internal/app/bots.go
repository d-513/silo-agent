package app

import (
	"context"
	"log"
	"sync"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
)

func (a *App) lockBot(id string) func() {
	v, _ := a.lifecycle.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// live reports whether a container is actually running. Recovers a name-match
// if the stored ID is stale. Clears the ID only when the box is confirmed gone.
func (a *App) live(ctx context.Context, b *db.Bot) bool {
	unlock := a.lockBot(b.ID)
	defer unlock()
	// Re-read under the lock: the caller's row can predate a concurrent
	// create/start, and acting on a stale TokenHash would drop the box that was
	// just created and clobber the token ensureRunning stored.
	a.reloadBot(b)
	return a.liveLocked(ctx, b)
}

// reloadBot replaces b with the current row. Callers hold lockBot so the
// lifecycle fields they act on match the ones ensureRunning writes.
func (a *App) reloadBot(b *db.Bot) {
	var cur db.Bot
	if err := a.DB.First(&cur, "id = ?", b.ID).Error; err != nil {
		return
	}
	*b = cur
}

func (a *App) liveLocked(ctx context.Context, b *db.Bot) bool {
	if a.Hub.Connected(b.ID) {
		return true
	}
	st, err := a.inspectBot(ctx, b)
	if err != nil {
		if dockerx.IsNotFound(err) {
			a.setContainer(b, "")
			return false
		}
		log.Printf("inspect bot=%s: %v", b.ID, err)
		return b.ContainerID != ""
	}
	if h, err := a.Docker.EnvTokenHash(ctx, st.ID); err == nil && h != "" && h != b.TokenHash {
		a.Docker.Drop(ctx, b.ID, st.ID)
		a.setContainer(b, "")
		return false
	}
	a.setContainer(b, st.ID)
	return st.Running
}

// The bots row is written by many hands (a run's last_task, the audit
// counter, proactivity, the zone, settings), so the box lifecycle writes only
// its own columns, by name, and never the whole row: a row loaded before a
// Docker call that takes seconds would put back whatever changed meanwhile,
// and a whole-row save would insert a Bot deleted meanwhile again.

// setContainer moves the Bot's container id from the one this caller saw to
// id. The update is conditional on what was seen, so a read (GetBot, ListBots
// polling through live) that clears a vanished box cannot undo a box made
// since. Callers hold lockBot.
func (a *App) setContainer(b *db.Bot, id string) {
	if b.ContainerID == id {
		return
	}
	a.DB.Model(&db.Bot{}).Where("id = ? AND COALESCE(container_id, '') = ?", b.ID, b.ContainerID).Update("container_id", id)
	b.ContainerID = id
}

// setBox writes lifecycle columns of the Bot. Callers hold lockBot.
func (a *App) setBox(b *db.Bot, cols map[string]any) error {
	return a.DB.Model(&db.Bot{}).Where("id = ?", b.ID).Updates(cols).Error
}

// startFailed records why the box would not start, where the Bot's last task
// shows, and returns err.
func (a *App) startFailed(b *db.Bot, err error) error {
	b.LastTask = err.Error()
	a.setBox(b, map[string]any{"last_task": b.LastTask})
	return err
}

// unheldToken is a token hash no worker holds: what a Bot with no box carries,
// since token_hash is unique and an empty one would clash with the next Bot's.
func unheldToken() string {
	return ids.Hash(ids.Token())
}

// dropStale removes a box that is not what Silo would make now, so the rest
// of ensureRunning makes it again, as a Container reset would: the workspace,
// drives and caches live outside the box. Runs are left alone; their
// worker-backed tools wait for the new box. Callers hold lockBot.
//
//   - A box that trusts a network filter certificate the gateway no longer
//     signs with (the CA was rotated since it was made) goes even while it
//     runs: every HTTPS call it makes fails until it is made again.
//   - A box on an older image or made another way (dockerx.Outdated: a new
//     release, a rebuilt tag, new bot_limits) goes only while it is stopped.
//     A running box holds what the Bot has open (its desktop, Python
//     sessions, servers, what it installed outside /workspace), so it is
//     updated at its next start, never under it; the Containers tab says an
//     update is waiting.
func (a *App) dropStale(ctx context.Context, b *db.Bot) {
	st, err := a.inspectBot(ctx, b)
	if err != nil {
		return
	}
	switch {
	case a.Egress.StaleCA(st, "bot", b.ID, ""):
		log.Printf("bot=%s: the network filter's certificate changed; recreating its box", b.ID)
	case st.Running:
		return
	default:
		why := a.Docker.Outdated(ctx, st, "bot", a.cfg().BotImage)
		if why == "" {
			return
		}
		log.Printf("bot=%s: its box is outdated (%s); recreating it", b.ID, why)
	}
	a.Hub.Drop(b.ID)
	a.Docker.Drop(ctx, b.ID, st.ID)
	a.setContainer(b, "")
}

func (a *App) inspectBot(ctx context.Context, b *db.Bot) (dockerx.State, error) {
	st, err := a.Docker.Inspect(ctx, b.ContainerID)
	if err == nil {
		return st, nil
	}
	if b.ContainerID != "" && !dockerx.IsNotFound(err) {
		return dockerx.State{}, err
	}
	st, err = a.Docker.Inspect(ctx, dockerx.Name(b.ID))
	return st, err
}

// ensureRunning starts the existing box or recreates it if Docker lost it. A
// running Bot also gets its drive sidecar (in the background; mounts appear in
// /workspace/drives by propagation whenever they are ready).
func (a *App) ensureRunning(ctx context.Context, b *db.Bot) (err error) {
	defer func() {
		if err == nil {
			a.Drives.EnsureBg(b.ID)
		}
	}()
	unlock := a.lockBot(b.ID)
	defer unlock()
	// Another ensure/start may have created a box since this row was loaded;
	// reload so the token-hash check and the writes below do not undo it.
	a.reloadBot(b)
	a.dropStale(ctx, b)
	if a.Hub.Connected(b.ID) {
		// A worker session can outlive its container (removed out of band, a
		// daemon restart, a failed recreate). Confirm the box with Docker
		// before trusting the session; when it is gone, drop the stale session
		// so the recreate path below can bring the Bot back.
		st, err := a.inspectBot(ctx, b)
		switch {
		case err == nil && st.Running:
			a.setContainer(b, st.ID)
			return nil
		case err != nil && !dockerx.IsNotFound(err):
			// Docker is unreachable: keep trusting the live worker rather than
			// tearing down a working session on a transient error.
			return nil
		default:
			a.Hub.Drop(b.ID)
		}
	}
	if a.liveLocked(ctx, b) {
		return nil
	}
	if st, err := a.inspectBot(ctx, b); err == nil {
		if err := a.Docker.Start(ctx, st.ID); err != nil {
			return a.startFailed(b, err)
		}
		a.setContainer(b, st.ID)
		return nil
	} else if !dockerx.IsNotFound(err) {
		return a.startFailed(b, err)
	}
	a.Docker.Drop(ctx, b.ID, b.ContainerID)
	token := ids.Token()
	net, err := a.Egress.ClientNetwork(ctx, "bot", b.ID, "", b.Name)
	if err != nil {
		return a.startFailed(b, err)
	}
	cid, err := a.Docker.Create(ctx, b.ID, token, net)
	if err != nil {
		return a.startFailed(b, err)
	}
	if err := a.Docker.Start(ctx, cid); err != nil {
		a.Docker.Drop(ctx, b.ID, cid)
		a.setContainer(b, "")
		return a.startFailed(b, err)
	}
	a.Mask(b.ID).Add(token)
	// Only now that the box exists: a token stored for a box that failed
	// would turn the leftover one into a stranger (see liveLocked).
	b.TokenHash = ids.Hash(token)
	b.ContainerID = cid
	return a.setBox(b, map[string]any{"token_hash": b.TokenHash, "container_id": cid})
}

func (a *App) ensureRunningBg(b *db.Bot) {
	go func() {
		if err := a.ensureRunning(context.Background(), b); err != nil {
			log.Printf("ensure bot=%s: %v", b.ID, err)
		}
	}()
}

// Wake brings a Bot's machine up, if it may run, and waits for its worker: for
// work that must reach the box before a run starts.
func (a *App) Wake(ctx context.Context, botID string) bool {
	var b db.Bot
	if a.DB.Where("id = ?", botID).Limit(1).Find(&b); b.ID == "" || a.ownerDisabled(&b) {
		return false
	}
	a.ensureRunningBg(&b)
	return a.Hub.WaitConnected(ctx, botID, 90*time.Second)
}

// haltBot stops the Bot's box (docker stop: it keeps its id). It writes
// nothing on the row.
func (a *App) haltBot(ctx context.Context, b *db.Bot) {
	unlock := a.lockBot(b.ID)
	defer unlock()
	// The box to stop is the one stored now, not when the caller loaded b.
	a.reloadBot(b)
	a.cancelBot(b.ID)
	a.Hub.Drop(b.ID)
	a.boxes.forget(b.ID)
	if b.ContainerID != "" {
		_ = a.Docker.Stop(ctx, b.ContainerID)
	}
	_ = a.Docker.Stop(ctx, dockerx.Name(b.ID))
	a.Drives.Stop(ctx, b.ID)
}

// destroyBot removes the Bot's box and forgets it: the container id goes, and
// the token hash becomes one nobody holds, so the removed box's token opens
// nothing (the next create stores its own once the box exists).
func (a *App) destroyBot(ctx context.Context, b *db.Bot) {
	unlock := a.lockBot(b.ID)
	defer unlock()
	a.reloadBot(b)
	a.cancelBot(b.ID)
	a.Hub.Drop(b.ID)
	a.boxes.forget(b.ID)
	a.Docker.Drop(ctx, b.ID, b.ContainerID)
	b.ContainerID = ""
	b.TokenHash = unheldToken()
	a.setBox(b, map[string]any{"container_id": "", "token_hash": b.TokenHash})
}
