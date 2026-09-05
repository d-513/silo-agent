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
	return a.liveLocked(ctx, b)
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
	if st.Running {
		if b.ContainerID != st.ID {
			b.ContainerID = st.ID
			a.DB.Save(b)
		}
		return true
	}
	_ = a.Docker.Stop(ctx, st.ID)
	a.Docker.Drop(ctx, b.ID, b.ContainerID)
	b.ContainerID = ""
	if b.Status != "stopped" {
		b.Status = "stopped"
	}
	a.DB.Save(b)
	return false
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
	if a.liveLocked(ctx, b) {
		if b.Status == "stopped" {
			b.Status = "starting"
			a.DB.Save(b)
		}
		return nil
	}
	a.Docker.Drop(ctx, b.ID, b.ContainerID)
	token := ids.Token()
	b.TokenHash = ids.Hash(token)
	cid, err := a.Docker.Create(ctx, b.ID, token)
	if err != nil {
		b.ContainerID = ""
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
	b.ContainerID = cid
	b.Status = "starting"
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
}
