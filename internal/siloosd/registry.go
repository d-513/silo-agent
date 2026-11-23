package siloosd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// The registry is asked with skopeo, which bootc itself depends on (its image
// proxy), always about a tag of the repository the release file names: the
// caller supplies at most a tag that passed tagAllowed.

const (
	registryTimeout = 45 * time.Second
	// versionsShown is how many release versions ListUpdateChannels offers.
	versionsShown = 12
	// inspectParallel bounds the registry requests made at once.
	inspectParallel = 4
)

// authFiles are where bootc reads registry credentials, in its order; the
// same file is handed to skopeo so a private repository answers alike.
var authFiles = []string{"etc/ostree/auth.json", "run/ostree/auth.json", "usr/lib/ostree/auth.json"}

func (s *Server) skopeo(ctx context.Context, args ...string) ([]byte, error) {
	cmd := []string{args[0]}
	for _, f := range authFiles {
		if _, err := os.Stat(s.file(f)); err == nil {
			cmd = append(cmd, "--authfile", "/"+f)
			break
		}
	}
	cmd = append(cmd, args[1:]...)
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	return s.run.Run(ctx, "skopeo", cmd...)
}

// remoteImage is what the registry says of one tag.
type remoteImage struct {
	Version string
	Created time.Time
}

func (s *Server) inspect(ctx context.Context, repo, tag string) (remoteImage, error) {
	raw, err := s.skopeo(ctx, "inspect", "--no-tags", "docker://"+repo+":"+tag)
	if err != nil {
		return remoteImage{}, err
	}
	var out struct {
		Created string            `json:"Created"`
		Labels  map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return remoteImage{}, fmt.Errorf("skopeo inspect: %w", err)
	}
	r := remoteImage{Version: out.Labels["org.opencontainers.image.version"]}
	if r.Version == "" && versionTagRE.MatchString(tag) {
		r.Version = tag
	}
	r.Created, _ = time.Parse(time.RFC3339Nano, out.Created)
	return r, nil
}

func (s *Server) listTags(ctx context.Context, repo string) ([]string, error) {
	raw, err := s.skopeo(ctx, "list-tags", "docker://"+repo)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tags []string `json:"Tags"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("skopeo list-tags: %w", err)
	}
	return out.Tags, nil
}

// running is what the machine boots, for telling a downgrade: its release
// version, its build time, and whether it is a build of the repository its
// channels are in (a local or custom image's build time says nothing about
// where it stands among the releases).
type running struct {
	Version string
	Built   time.Time
	Ours    bool
}

func (s *Server) running(ctx context.Context, rel release) (running, error) {
	h, err := s.bootcStatus(ctx)
	if err != nil {
		return running{}, err
	}
	r := running{Version: rel.Version}
	ch, _ := bootedChannel(h, rel)
	r.Ours = ch != channelLocal && ch != channelCustom
	if b := h.Status.Booted; b != nil && b.Image != nil {
		r.Built, _ = time.Parse(time.RFC3339Nano, b.Image.Timestamp)
	}
	return r, nil
}

// olderThan says why the image img (tag) is older than the one running, or
// "" when it is not: a lower release version, or, between builds of the same
// repository, built before it (which is what orders master-<sha> builds and
// a latest that is behind a master build).
func olderThan(tag string, img remoteImage, run running) string {
	v := img.Version
	if v == "" {
		v = tag
	}
	if older(v, run.Version) {
		return fmt.Sprintf("%s is an older version than the %s running.", v, run.Version)
	}
	if run.Ours && !img.Created.IsZero() && !run.Built.IsZero() && img.Created.Before(run.Built) {
		what := tag
		if img.Version != "" && img.Version != tag {
			what = tag + " (" + img.Version + ")"
		}
		return fmt.Sprintf("%s was built %s, before the image running (%s).", what, img.Created.UTC().Format("2006-01-02 15:04"), run.Built.UTC().Format("2006-01-02 15:04"))
	}
	return ""
}

func (s *Server) ListUpdateChannels(ctx context.Context, _ *connect.Request[v1.ListUpdateChannelsRequest]) (*connect.Response[v1.UpdateChannels], error) {
	rel := s.release()
	if rel.Repository == "" {
		return nil, refused("This image does not say which repository its channels are in (/usr/lib/siloos/release).")
	}
	run, err := s.running(ctx, rel)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	tags, err := s.listTags(ctx, rel.Repository)
	if err != nil {
		return nil, refused("Could not ask the registry which versions there are: " + clip(err.Error(), 400))
	}
	// Release versions not older than the one running, newest first.
	var versions []string
	for _, t := range tags {
		if versionTagRE.MatchString(t) && !older(t, run.Version) {
			versions = append(versions, t)
		}
	}
	sort.SliceStable(versions, func(i, j int) bool { return newer(versions[i], versions[j]) })
	if len(versions) > versionsShown {
		versions = versions[:versionsShown]
	}
	offer := []string{}
	for _, t := range []string{channelLatest, channelMaster} {
		for _, have := range tags {
			if have == t {
				offer = append(offer, t)
				break
			}
		}
	}
	offer = append(offer, versions...)

	out := make([]*v1.UpdateChannel, len(offer))
	var wg sync.WaitGroup
	sem := make(chan struct{}, inspectParallel)
	for i, t := range offer {
		out[i] = &v1.UpdateChannel{Tag: t}
		wg.Add(1)
		go func(c *v1.UpdateChannel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			img, err := s.inspect(ctx, rel.Repository, c.Tag)
			if err != nil {
				// Not known to be older; a move asks again and refuses then.
				return
			}
			c.Version = img.Version
			if !img.Created.IsZero() {
				c.Created = img.Created.UTC().Format(time.RFC3339)
			}
			if why := olderThan(c.Tag, img, run); why != "" {
				c.Older, c.Reason = true, why
			}
		}(out[i])
	}
	wg.Wait()
	return connect.NewResponse(&v1.UpdateChannels{Channels: out}), nil
}

// newer reports whether version tag a is a release after b (a prerelease
// before its release).
func newer(a, b string) bool {
	if older(b, a) {
		return true
	}
	if older(a, b) {
		return false
	}
	_, pa, _ := strings.Cut(a, "-")
	_, pb, _ := strings.Cut(b, "-")
	if pa == "" || pb == "" {
		return pa == "" && pb != ""
	}
	return pa > pb
}
