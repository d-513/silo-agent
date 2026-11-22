package siloosd

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// A SiloOS machine follows one tag of the repository its image was built for
// (its channel), as bootc does: `bootc upgrade` fetches that tag again.
//
//	local    built on someone's machine (localhost/silo-os:dev): follows
//	         nothing, and is updated by building and installing again, or by
//	         moving to a channel
//	latest   stable releases: the highest full vX.Y.Z
//	master   a build of every commit to master (master-<sha>)
//	pinned   one version tag (v0.4.2) or one commit (master-<sha>): fetching
//	         it again never finds anything new
//	custom   an image of another repository (a fork, a mirror)
//
// The image says what it is in /usr/lib/siloos/release (images/os/Containerfile):
// its version and the repository its channels are tags of. siloosd switches
// only within that repository, so the control plane can move the machine
// between SiloOS's own releases and nowhere else.

const releaseFile = "usr/lib/siloos/release"

const (
	channelLocal  = "local"
	channelLatest = "latest"
	channelMaster = "master"
	channelPinned = "pinned"
	channelCustom = "custom"
)

var (
	// A tag a machine may be moved to: a channel or a release version.
	versionTagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)
	commitTagRE  = regexp.MustCompile(`^master-[0-9a-f]{7,40}$`)
	// A repository as the release file may name it.
	repositoryRE = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)+$`)
)

// release is what /usr/lib/siloos/release says.
type release struct {
	Version    string
	Repository string
}

func (s *Server) release() release {
	kv := keyValues(s.readFile(releaseFile))
	r := release{Version: kv["SILOOS_VERSION"], Repository: kv["SILOOS_REPOSITORY"]}
	// (The release file is read again each time: a staged image's applies
	// only after the reboot that boots it, when siloosd starts anew anyway.)
	if !repositoryRE.MatchString(r.Repository) {
		r.Repository = ""
	}
	return r
}

// channelOf is what a machine booted from ref (by bootc's transport) follows,
// and the tag.
func channelOf(ref, transport, repository string) (channel, tag string) {
	if transport != "" && transport != "registry" {
		return channelLocal, ""
	}
	repo, tag := splitRef(ref)
	switch {
	case repo == "" || strings.HasPrefix(repo, "localhost/"):
		return channelLocal, tag
	case repository == "" || repo != repository:
		return channelCustom, tag
	case tag == channelLatest || tag == channelMaster:
		return tag, tag
	default:
		return channelPinned, tag
	}
}

// splitRef cuts an image reference into its repository and tag (latest when
// it names none). A digest reference has no tag.
func splitRef(ref string) (repo, tag string) {
	ref, _, _ = strings.Cut(ref, "@")
	slash := strings.LastIndexByte(ref, '/')
	if colon := strings.LastIndexByte(ref, ':'); colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, "latest"
}

func (s *Server) SetUpdateChannel(ctx context.Context, req *connect.Request[v1.SetUpdateChannelRequest]) (*connect.Response[v1.Updates], error) {
	tag := strings.TrimSpace(req.Msg.GetChannel())
	if tag != channelLatest && tag != channelMaster && !versionTagRE.MatchString(tag) && !commitTagRE.MatchString(tag) {
		return nil, invalid("The channel is latest, master, or a version (v0.4.2).")
	}
	rel := s.release()
	if rel.Repository == "" {
		return nil, refused("This image does not say which repository its channels are in (/usr/lib/siloos/release).")
	}
	if older(tag, rel.Version) {
		return nil, refused("That version is older than the one running (" + rel.Version + "). Going back would hand Silo a database newer than it understands; roll back instead, if the previous image is the one you want.")
	}
	ref := rel.Repository + ":" + tag
	if !s.startJob("switch", []string{"switch", ref}, false, nil) {
		return nil, refused("An update job is running already.")
	}
	audit(ctx, "moved the machine to %s", ref)
	return s.GetUpdates(ctx, nil)
}

// older reports whether version tag a is a release before b. Anything that
// is not a vX.Y.Z (latest, master, a commit, dev) compares as not older.
func older(a, b string) bool {
	va, oka := semver(a)
	vb, okb := semver(b)
	if !oka || !okb {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] < vb[i]
		}
	}
	return false
}

func semver(tag string) ([3]int, bool) {
	var out [3]int
	if !versionTagRE.MatchString(tag) {
		return out, false
	}
	core, _, _ := strings.Cut(strings.TrimPrefix(tag, "v"), "-")
	for i, p := range strings.SplitN(core, ".", 3) {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
