package siloosd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// Updates are bootc's: the machine boots one image and keeps the one before
// it (rollback); a new one is downloaded beside them (staged) and used at the
// next boot. siloosd runs the three steps as jobs, one at a time, and keeps
// the update policy in place of the base image's bootc-fetch-apply-updates
// timer (masked in os/Containerfile), which reboots whenever it finds
// something: an admin chooses that here.

const (
	policyName = "updates.json"
	// jobTimeout bounds a download, which can be gigabytes.
	jobTimeout = time.Hour
	outputMax  = 8 << 10

	modeOff      = "off"
	modeDownload = "download"
	modeReboot   = "reboot"
)

// policyFile is the update policy and when the policy last ran.
type policyFile struct {
	Mode    string    `json:"mode"`
	Hour    int       `json:"hour"`
	LastRun time.Time `json:"last_run,omitempty"`
}

// defaultPolicy downloads at 04:00 and leaves the reboot to an admin.
var defaultPolicy = policyFile{Mode: modeDownload, Hour: 4}

func (s *Server) loadPolicy() policyFile {
	b, err := os.ReadFile(s.statePath(policyName))
	if err != nil {
		return defaultPolicy
	}
	var p policyFile
	if json.Unmarshal(b, &p) != nil || !validMode(p.Mode) || p.Hour < 0 || p.Hour > 23 {
		return defaultPolicy
	}
	return p
}

func (s *Server) savePolicy() {
	b, _ := json.Marshal(s.policy)
	writeFile(s.statePath(policyName), b)
}

func validMode(m string) bool { return m == modeOff || m == modeDownload || m == modeReboot }

// bootcHost is the part of `bootc status --format=json` siloosd reads.
type bootcHost struct {
	Status struct {
		Staged         *bootEntry `json:"staged"`
		Booted         *bootEntry `json:"booted"`
		Rollback       *bootEntry `json:"rollback"`
		RollbackQueued bool       `json:"rollbackQueued"`
	} `json:"status"`
}

type bootEntry struct {
	Image        *imageStatus `json:"image"`
	CachedUpdate *imageStatus `json:"cachedUpdate"`
}

type imageStatus struct {
	Image struct {
		Image     string `json:"image"`
		Transport string `json:"transport"`
	} `json:"image"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
	Digest    string `json:"imageDigest"`
}

func (s *Server) bootcStatus(ctx context.Context) (*bootcHost, error) {
	raw, err := s.run.Run(ctx, "bootc", "status", "--format=json")
	if err != nil {
		return nil, err
	}
	var h bootcHost
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("bootc status: %w", err)
	}
	return &h, nil
}

// bootedChannel is the channel and tag of the booted image.
func bootedChannel(h *bootcHost, rel release) (string, string) {
	if h.Status.Booted == nil || h.Status.Booted.Image == nil {
		return channelLocal, ""
	}
	img := h.Status.Booted.Image.Image
	return channelOf(img.Image, img.Transport, rel.Repository)
}

func bootImage(e *bootEntry) *v1.BootImage {
	if e == nil || e.Image == nil {
		return nil
	}
	return image(e.Image)
}

func image(i *imageStatus) *v1.BootImage {
	return &v1.BootImage{Image: i.Image.Image, Version: i.Version, Digest: i.Digest, Timestamp: i.Timestamp}
}

func (s *Server) GetUpdates(ctx context.Context, _ *connect.Request[v1.GetUpdatesRequest]) (*connect.Response[v1.Updates], error) {
	out := &v1.Updates{}
	h, err := s.bootcStatus(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	st := h.Status
	out.Booted, out.Staged, out.Rollback = bootImage(st.Booted), bootImage(st.Staged), bootImage(st.Rollback)
	out.RollbackQueued = st.RollbackQueued
	if st.Booted != nil && st.Booted.CachedUpdate != nil && out.Booted != nil && st.Booted.CachedUpdate.Digest != out.Booted.Digest {
		out.Available = image(st.Booted.CachedUpdate)
	}
	s.mu.Lock()
	if s.job != nil {
		out.Job = cloneJob(s.job)
	}
	p := s.policy
	s.mu.Unlock()
	out.Policy = &v1.UpdatePolicy{Mode: p.Mode, Hour: int32(p.Hour)}
	rel := s.release()
	out.Version, out.Repository = rel.Version, rel.Repository
	out.Channel, out.Tag = bootedChannel(h, rel)
	if next := nextRun(p, s.now().In(s.zone(ctx))); !next.IsZero() && out.Channel != channelLocal {
		out.NextRunAt = next.UTC().Format(time.RFC3339)
	}
	return connect.NewResponse(out), nil
}

func (s *Server) RunUpdate(ctx context.Context, req *connect.Request[v1.RunUpdateRequest]) (*connect.Response[v1.Updates], error) {
	action := req.Msg.GetAction()
	args, ok := jobArgs[action]
	if !ok {
		return nil, invalid("The action is check, download or rollback.")
	}
	if action != "rollback" {
		if h, err := s.bootcStatus(ctx); err == nil {
			if ch, _ := bootedChannel(h, s.release()); ch == channelLocal {
				return nil, refused("This machine runs a local build, which follows no channel: move it to one (latest or master) to get updates.")
			}
		}
	}
	if !s.startJob(action, args, false, nil) {
		return nil, refused("An update job is running already.")
	}
	audit(ctx, "started the update job %s", action)
	return s.GetUpdates(ctx, nil)
}

// jobArgs are bootc's arguments per job. download is `bootc upgrade`, which
// stages the new image for the next boot and never reboots by itself.
var jobArgs = map[string][]string{
	"check":    {"upgrade", "--check"},
	"download": {"upgrade"},
	"rollback": {"rollback"},
}

// startJob runs bootc in the background unless a job is running; then runs
// done, if any, with the job's error.
func (s *Server) startJob(action string, args []string, scheduled bool, done func(error)) bool {
	if !s.jobs.TryLock() {
		return false
	}
	job := &v1.UpdateJob{Action: action, Running: true, StartedAt: s.now().UTC().Format(time.RFC3339), Scheduled: scheduled}
	s.mu.Lock()
	s.job = job
	s.mu.Unlock()
	go func() {
		defer s.jobs.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		out, err := s.run.Run(ctx, "bootc", args...)
		s.mu.Lock()
		job.Running = false
		job.EndedAt = s.now().UTC().Format(time.RFC3339)
		job.Output = tail(string(bytes.TrimSpace(out)), outputMax)
		if err != nil {
			job.Error = clip(err.Error(), 2000)
		}
		s.mu.Unlock()
		if err != nil {
			fmt.Fprintf(os.Stderr, "siloosd: update job %s failed: %v\n", action, err)
		}
		if done != nil {
			done(err)
		}
	}()
	return true
}

func cloneJob(j *v1.UpdateJob) *v1.UpdateJob {
	return &v1.UpdateJob{Action: j.Action, Running: j.Running, StartedAt: j.StartedAt, EndedAt: j.EndedAt, Error: j.Error, Output: j.Output, Scheduled: j.Scheduled}
}

func (s *Server) SetUpdatePolicy(ctx context.Context, req *connect.Request[v1.SetUpdatePolicyRequest]) (*connect.Response[v1.Updates], error) {
	p := req.Msg.GetPolicy()
	if p == nil || !validMode(p.GetMode()) {
		return nil, invalid("The policy's mode is off, download or reboot.")
	}
	if p.GetHour() < 0 || p.GetHour() > 23 {
		return nil, invalid("The hour is 0 to 23.")
	}
	s.mu.Lock()
	s.policy.Mode, s.policy.Hour = p.GetMode(), int(p.GetHour())
	s.savePolicy()
	s.mu.Unlock()
	audit(ctx, "set the update policy to %s at %02d:00", p.GetMode(), p.GetHour())
	return s.GetUpdates(ctx, nil)
}

// nextRun is when the policy next runs: the policy's hour today, unless it
// ran today or the hour is over, then tomorrow. Zero when it is off.
func nextRun(p policyFile, now time.Time) time.Time {
	if p.Mode == modeOff {
		return time.Time{}
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), p.Hour, 0, 0, 0, now.Location())
	ranToday := !p.LastRun.IsZero() && sameDay(p.LastRun.In(now.Location()), now)
	if ranToday || now.After(day.Add(time.Hour)) {
		return day.AddDate(0, 0, 1)
	}
	return day
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// scheduled runs the policy when its hour has come: a download, and with
// reboot, a reboot when the download staged an image.
func (s *Server) scheduled(ctx context.Context) {
	now := s.now().In(s.zone(ctx))
	s.mu.Lock()
	p := s.policy
	s.mu.Unlock()
	next := nextRun(p, now)
	if next.IsZero() || now.Before(next) || now.After(next.Add(time.Hour)) {
		return
	}
	// A local build has nothing to fetch.
	if h, err := s.bootcStatus(ctx); err == nil {
		if ch, _ := bootedChannel(h, s.release()); ch == channelLocal {
			return
		}
	}
	started := s.startJob("download", jobArgs["download"], true, func(err error) {
		if err != nil || p.Mode != modeReboot {
			return
		}
		h, err := s.bootcStatus(ctx)
		if err != nil || h.Status.Staged == nil {
			return
		}
		fmt.Fprintln(os.Stderr, "siloosd: a new image is staged; rebooting into it as the update policy says")
		s.reboot("reboot")
	})
	if !started {
		return
	}
	s.mu.Lock()
	s.policy.LastRun = now
	s.savePolicy()
	s.mu.Unlock()
}

// zone is the machine's time zone as it is now: an admin can change it while
// siloosd runs, and Go reads /etc/localtime only once.
func (s *Server) zone(ctx context.Context) *time.Location {
	if raw, err := s.run.Run(ctx, "timedatectl", "show", "-p", "Timezone", "--value"); err == nil {
		if loc, err := time.LoadLocation(string(bytes.TrimSpace(raw))); err == nil {
			return loc
		}
	}
	return time.Local
}

// writeFile replaces path with data in one step, readable by root only.
func writeFile(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "siloosd: %v\n", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "siloosd: %v\n", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "siloosd: %v\n", err)
	}
}
