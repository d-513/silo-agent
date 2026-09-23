package app

import (
	"context"
	"log"
	"sync"

	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
)

// DeriveStatus: worker and Docker are truth; the DB row is a cache.
func DeriveStatus(connected, running bool, dbStatus string) string {
	switch {
	case connected:
		if dbStatus == "working" || dbStatus == "needs_you" {
			return dbStatus
		}
		return "online"
	case running:
		return "starting"
	default:
		return "stopped"
	}
}

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
			if b.ContainerID != "" {
				b.ContainerID = ""
				if b.Status != "stopped" {
					b.Status = "stopped"
				}
				a.DB.Save(b)
			}
			return false
		}
		log.Printf("inspect bot=%s: %v", b.ID, err)
		return b.ContainerID != ""
	}
	if h, err := a.Docker.EnvTokenHash(ctx, st.ID); err == nil && h != "" && h != b.TokenHash {
		a.Docker.Drop(ctx, b.ID, st.ID)
		b.ContainerID = ""
		if b.Status != "stopped" {
			b.Status = "stopped"
		}
		a.DB.Save(b)
		return false
	}
	if b.ContainerID != st.ID {
		b.ContainerID = st.ID
		a.DB.Save(b)
	}
	if !st.Running {
		if b.Status != "stopped" {
			b.Status = "stopped"
			a.DB.Save(b)
		}
		return false
	}
	return true
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

// ensureRunning starts the existing box or recreates it if Docker lost it.
func (a *App) ensureRunning(ctx context.Context, b *db.Bot) error {
	unlock := a.lockBot(b.ID)
	defer unlock()
	// Another ensure/start may have created a box since this row was loaded;
	// reload so the token-hash check and the writes below do not undo it.
	a.reloadBot(b)
	if a.Hub.Connected(b.ID) {
		// A worker session can outlive its container (removed out of band, a
		// daemon restart, a failed recreate). Confirm the box with Docker
		// before trusting the session; when it is gone, drop the stale session
		// so the recreate path below can bring the Bot back.
		st, err := a.inspectBot(ctx, b)
		switch {
		case err == nil && st.Running:
			if b.ContainerID != st.ID {
				b.ContainerID = st.ID
				a.DB.Save(b)
			}
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
		if b.Status == "stopped" {
			b.Status = "starting"
			a.DB.Save(b)
		}
		return nil
	}
	if st, err := a.inspectBot(ctx, b); err == nil {
		if err := a.Docker.Start(ctx, st.ID); err != nil {
			b.Status = "stopped"
			b.LastTask = err.Error()
			a.DB.Save(b)
			return err
		}
		if b.ContainerID != st.ID {
			b.ContainerID = st.ID
		}
		b.Status = "starting"
		b.LastTask = ""
		return a.DB.Save(b).Error
	} else if !dockerx.IsNotFound(err) {
		b.Status = "stopped"
		b.LastTask = err.Error()
		a.DB.Save(b)
		return err
	}
	a.Docker.Drop(ctx, b.ID, b.ContainerID)
	token := ids.Token()
	cid, err := a.Docker.Create(ctx, b.ID, token)
	if err != nil {
		b.Status = "stopped"
		b.LastTask = err.Error()
		a.DB.Save(b)
		return err
	}
	if err := a.Docker.Start(ctx, cid); err != nil {
		a.Docker.Drop(ctx, b.ID, cid)
		b.ContainerID = ""
		b.Status = "stopped"
		b.LastTask = err.Error()
		a.DB.Save(b)
		return err
	}
	a.Mask(b.ID).Add(token)
	b.TokenHash = ids.Hash(token)
	b.ContainerID = cid
	b.Status = "starting"
	b.LastTask = ""
	return a.DB.Save(b).Error
}

func (a *App) ensureRunningBg(b *db.Bot) {
	go func() {
		if err := a.ensureRunning(context.Background(), b); err != nil {
			log.Printf("ensure bot=%s: %v", b.ID, err)
		}
	}()
}

func (a *App) haltBot(ctx context.Context, b *db.Bot) {
	unlock := a.lockBot(b.ID)
	defer unlock()
	a.cancelBot(b.ID)
	a.Hub.Drop(b.ID)
	if b.ContainerID != "" {
		_ = a.Docker.Stop(ctx, b.ContainerID)
	}
	_ = a.Docker.Stop(ctx, dockerx.Name(b.ID))
}

func (a *App) destroyBot(ctx context.Context, b *db.Bot) {
	unlock := a.lockBot(b.ID)
	defer unlock()
	a.cancelBot(b.ID)
	a.Hub.Drop(b.ID)
	a.Docker.Drop(ctx, b.ID, b.ContainerID)
	b.ContainerID = ""
}
