package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// The workspace history is a shadow git repository outside /workspace, used as
// a snapshot store and nothing more: no branches, no HEAD, no parents. A
// checkpoint is a parentless commit of the workspace tree under its own ref
// (refs/silo/cp/<unix nanos>), so any two diff and forgetting one is deleting a
// ref. It compares state, not actions, so a file a script wrote is seen like
// one the write tool wrote.
//
// What keeps it from swallowing the workspace:
//   - drives/ (remote data behind FUSE: listing it is network calls), tmp/,
//     bot/ and the heavy trees are excluded and never descended into;
//   - the workspace's own .gitignore files are honoured;
//   - a file over the size cap is never hashed: the tree holds a stub with its
//     size and mtime, so it still shows as changed, without its content;
//   - a nested repository is one gitlink (its HEAD commit), not its files;
//   - one checkpoint adds a bounded number of files and bytes (a huge
//     workspace is indexed over several, marked partial meanwhile);
//   - old checkpoints expire by count, age and the store's size.
const (
	historyRefs = "refs/silo/cp/"

	defaultMaxFileBytes  = 8 << 20
	defaultKeep          = 300
	defaultKeepDays      = 30
	defaultMaxStoreBytes = 1 << 30

	changesDefaultLimit = 100
	changeFilesMax      = 500
	changePatchMax      = 256 << 10

	stubMagic = "silo-large-file\n"
)

// One checkpoint adds at most this much, so the first snapshot of a huge
// workspace cannot hold a run's first tool call for minutes. Vars so tests can
// shrink them.
var (
	checkpointMaxFiles       = 20000
	checkpointMaxBytes int64 = 512 << 20
	checkpointMaxLarge       = 5000
)

// historyExcludes is the shadow repository's info/exclude: what a checkpoint
// never reads. Anchored entries are workspace-relative.
func historyExcludes() string {
	var b strings.Builder
	b.WriteString("/drives/\n/tmp/\n/bot/\n")
	for _, d := range grepPruneDirs {
		b.WriteString(d + "/\n")
	}
	return b.String()
}

func historyDir() string {
	if d := os.Getenv("SILO_HISTORY_DIR"); d != "" {
		return d
	}
	return "/var/lib/silo/history"
}

type historyLimits struct {
	maxFile  int64
	keep     int
	keepDays int
	maxStore int64
}

func limitsOf(l *v1.HistoryLimits) historyLimits {
	out := historyLimits{maxFile: l.GetMaxFileBytes(), keep: int(l.GetKeep()), keepDays: int(l.GetKeepDays()), maxStore: l.GetMaxStoreBytes()}
	if out.maxFile <= 0 {
		out.maxFile = defaultMaxFileBytes
	}
	if out.keep <= 0 {
		out.keep = defaultKeep
	}
	if out.keepDays <= 0 {
		out.keepDays = defaultKeepDays
	}
	if out.maxStore <= 0 {
		out.maxStore = defaultMaxStoreBytes
	}
	return out
}

// git runs one git command against the history store with the workspace as its
// work tree. The user's and the system's git config are ignored, so nothing in
// the box (a global excludes file, a filter driver, an fsmonitor hook) changes
// what a checkpoint does.
func (w *worker) git(ctx context.Context, stdin []byte, env []string, args ...string) ([]byte, error) {
	full := append([]string{
		"-c", "core.quotepath=false",
		"-c", "advice.addEmbeddedRepo=false",
		"-c", "core.autocrlf=false",
		"-c", "gc.auto=0",
		"--literal-pathspecs",
	}, args...)
	c := exec.CommandContext(ctx, "git", full...)
	c.Dir = w.workspace
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			c.Env = append(c.Env, e)
		}
	}
	c.Env = append(c.Env,
		"GIT_DIR="+w.history,
		"GIT_WORK_TREE="+w.workspace,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Silo", "GIT_AUTHOR_EMAIL=silo@localhost",
		"GIT_COMMITTER_NAME=Silo", "GIT_COMMITTER_EMAIL=silo@localhost",
		"LC_ALL=C",
	)
	c.Env = append(c.Env, env...)
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errb
	if err := c.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("git is not installed on this Bot's machine; rebuild the bot image and reset the container")
		}
		msg := strings.TrimSpace(errb.String())
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return out.Bytes(), fmt.Errorf("git %s: %v: %s", args[0], err, msg)
	}
	return out.Bytes(), nil
}

// historyInit makes the store on first use and rewrites its exclude list, so
// the list follows the code.
func (w *worker) historyInit(ctx context.Context) error {
	if w.history == "" {
		w.history = historyDir()
	}
	if _, err := os.Stat(filepath.Join(w.history, "objects")); err != nil {
		if err := os.MkdirAll(w.history, 0o700); err != nil {
			return err
		}
		if _, err := w.git(ctx, nil, nil, "init", "--quiet"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(w.history, "info"), 0o700); err != nil {
		return err
	}
	// A git killed mid-write (a cancelled checkpoint) leaves its lock behind.
	// histMu is held, so no git of ours is running: the lock is stale.
	_ = os.Remove(filepath.Join(w.history, "index.lock"))
	return os.WriteFile(filepath.Join(w.history, "info", "exclude"), []byte(historyExcludes()), 0o600)
}

func splitNul(b []byte) []string {
	var out []string
	for _, p := range bytes.Split(b, []byte{0}) {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

func joinNul(xs []string) []byte {
	var b bytes.Buffer
	for _, x := range xs {
		b.WriteString(x)
		b.WriteByte(0)
	}
	return b.Bytes()
}

type snapshot struct {
	tree string
	// partial: the per-checkpoint budget ran out with files still unread.
	partial bool
	warn    string
}

// snap brings the store's index up to the workspace and returns the tree that
// describes it: the tracked files, plus a stub per file over the size cap.
// Nothing is committed; the caller decides whether this tree is a checkpoint.
func (w *worker) snap(ctx context.Context, lim historyLimits) (snapshot, error) {
	var s snapshot
	if err := w.historyInit(ctx); err != nil {
		return s, err
	}
	// A path that vanishes between the listing and the add (a temp file of a
	// running script) fails the whole add: list again, once.
	var large []bigFile
	for attempt := 0; ; attempt++ {
		retry, err := w.stage(ctx, lim, &s, &large)
		if err != nil {
			return s, err
		}
		if !retry || attempt > 0 {
			break
		}
	}
	out, err := w.git(ctx, nil, nil, "write-tree")
	if err != nil {
		return s, err
	}
	s.tree = strings.TrimSpace(string(out))
	if len(large) == 0 {
		return s, nil
	}

	// The large files join the tree as stubs through a throwaway index, so the
	// real index never holds an entry git would have to hash to refresh.
	tmp, err := os.MkdirTemp(w.history, "stubs-")
	if err != nil {
		return s, err
	}
	defer os.RemoveAll(tmp)
	var names []string
	for i, f := range large {
		name := filepath.Join(tmp, strconv.Itoa(i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("%ssize %d\nmtime %d\n", stubMagic, f.size, f.mtime)), 0o600); err != nil {
			return s, err
		}
		names = append(names, name)
	}
	out, err = w.git(ctx, []byte(strings.Join(names, "\n")+"\n"), nil, "hash-object", "-w", "--no-filters", "--stdin-paths")
	if err != nil {
		return s, err
	}
	shas := strings.Fields(string(out))
	if len(shas) != len(large) {
		return s, errors.New("history: stub objects do not match the large files")
	}
	var info bytes.Buffer
	for i, f := range large {
		fmt.Fprintf(&info, "100644 %s\t%s\x00", shas[i], f.path)
	}
	idx := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
	if _, err := w.git(ctx, nil, idx, "read-tree", s.tree); err != nil {
		return s, err
	}
	if _, err := w.git(ctx, info.Bytes(), idx, "update-index", "-z", "--index-info"); err != nil {
		return s, err
	}
	out, err = w.git(ctx, nil, idx, "write-tree")
	if err != nil {
		return s, err
	}
	s.tree = strings.TrimSpace(string(out))
	return s, nil
}

type bigFile struct {
	path        string
	size, mtime int64
}

// stage updates the index from the workspace and collects the files over the
// size cap. retry reports an add that failed because a listed path was gone.
func (w *worker) stage(ctx context.Context, lim historyLimits, s *snapshot, large *[]bigFile) (retry bool, err error) {
	// Untracked, modified and deleted paths. Only stat data is compared here;
	// excluded directories are not descended into.
	raw, err := w.git(ctx, nil, nil, "ls-files", "-z", "--others", "--modified", "--deleted", "--exclude-standard")
	if err != nil {
		return false, err
	}
	cands := splitNul(raw)
	sort.Strings(cands)
	var add, drop []string
	var bytesAdded int64
	*large = (*large)[:0]
	s.partial, s.warn = false, ""
	seen := ""
	for _, p := range cands {
		if p == seen {
			continue // a deleted path is listed as modified too
		}
		seen = p
		st, err := os.Lstat(filepath.Join(w.workspace, p))
		switch {
		case err != nil || !w.realParents(p):
			// Gone, or under a folder that is now a link or a file: whatever the
			// link leads to is not the workspace's, and git add refuses the path
			// outright, which would cost the whole checkpoint.
			drop = append(drop, p)
		case st.Mode().IsRegular() && st.Size() > lim.maxFile:
			// May be tracked from when it was small.
			drop = append(drop, p)
			if len(*large) < checkpointMaxLarge {
				*large = append(*large, bigFile{p, st.Size(), st.ModTime().UnixNano()})
			}
		case st.IsDir() || st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0:
			// A directory here is a nested repository (ls-files lists it as
			// one entry): it becomes a gitlink.
			if len(add) >= checkpointMaxFiles || bytesAdded > checkpointMaxBytes {
				s.partial = true
				continue
			}
			if st.Mode().IsRegular() {
				bytesAdded += st.Size()
			}
			add = append(add, strings.TrimSuffix(p, "/"))
		}
	}
	if len(drop) > 0 {
		if _, err := w.git(ctx, joinNul(drop), nil, "update-index", "--force-remove", "-z", "--stdin"); err != nil {
			return false, err
		}
	}
	if len(add) > 0 {
		// One unreadable file (or a nested repository with no commit yet) must
		// not cost the checkpoint: the rest is still added.
		if _, err := w.git(ctx, joinNul(add), nil, "add", "-A", "--ignore-errors", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			s.warn = err.Error()
			return strings.Contains(s.warn, "did not match any file"), nil
		}
	}
	return false, nil
}

// checkpointMeta is a checkpoint's commit message: one line of JSON.
type checkpointMeta struct {
	Note    json.RawMessage `json:"note,omitempty"`
	Partial bool            `json:"partial,omitempty"`
	Files   int             `json:"files"`
	Added   int             `json:"added"`
	Deleted int             `json:"deleted"`
}

type checkpointRef struct {
	ID     string
	Commit string
	Tree   string
	Meta   checkpointMeta
}

func (c checkpointRef) at() int64 {
	n, _ := strconv.ParseInt(c.ID, 10, 64)
	return n
}

// checkpoints lists the store's checkpoints, newest first.
func (w *worker) checkpoints(ctx context.Context) ([]checkpointRef, error) {
	out, err := w.git(ctx, nil, nil, "for-each-ref", "--sort=-refname",
		"--format=%(refname) %(objectname) %(tree) %(contents:subject)", historyRefs)
	if err != nil {
		return nil, err
	}
	var refs []checkpointRef
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, " ", 4)
		if len(f) < 3 {
			continue
		}
		r := checkpointRef{ID: strings.TrimPrefix(f[0], historyRefs), Commit: f[1], Tree: f[2]}
		if len(f) == 4 {
			_ = json.Unmarshal([]byte(f[3]), &r.Meta)
		}
		refs = append(refs, r)
	}
	return refs, nil
}

type diffStat struct{ files, added, deleted int }

// stat counts what differs between two history objects.
func (w *worker) stat(ctx context.Context, base, head string) (diffStat, error) {
	var st diffStat
	out, err := w.git(ctx, nil, nil, "diff-tree", "-r", "-M", "--numstat", "-z", "--no-commit-id", base, head)
	if err != nil {
		return st, err
	}
	for _, n := range parseNumstat(out) {
		st.files++
		st.added += n.added
		st.deleted += n.deleted
	}
	return st, nil
}

type numstat struct {
	path           string
	added, deleted int
	binary         bool
}

// parseNumstat reads `--numstat -z`: "added\tdeleted\tpath\0", or for a rename
// "added\tdeleted\t\0old\0new\0". A binary file counts as "-".
func parseNumstat(out []byte) []numstat {
	parts := bytes.Split(out, []byte{0})
	var rows []numstat
	for i := 0; i < len(parts); i++ {
		f := strings.SplitN(string(parts[i]), "\t", 3)
		if len(f) < 3 {
			continue
		}
		n := numstat{path: f[2], binary: f[0] == "-"}
		n.added, _ = strconv.Atoi(f[0])
		n.deleted, _ = strconv.Atoi(f[1])
		if f[2] == "" && i+2 < len(parts) {
			n.path = string(parts[i+2])
			i += 2
		}
		rows = append(rows, n)
	}
	return rows
}

type checkpointResult struct {
	ID      string `json:"id"`
	Changed bool   `json:"changed"`
	Files   int    `json:"files"`
	Partial bool   `json:"partial,omitempty"`
	Warn    string `json:"warn,omitempty"`
}

// checkpoint snapshots the workspace. An unchanged tree makes no checkpoint
// and answers the last one.
func (w *worker) checkpoint(ctx context.Context, cmd *v1.CheckpointCmd) (string, error) {
	w.histMu.Lock()
	defer w.histMu.Unlock()
	lim := limitsOf(cmd.GetLimits())
	s, err := w.snap(ctx, lim)
	if err != nil {
		return "", err
	}
	refs, err := w.checkpoints(ctx)
	if err != nil {
		return "", err
	}
	res, _, err := w.take(ctx, lim, s, refs, cmd.GetNote())
	if err != nil {
		return "", err
	}
	return marshalJSON(res)
}

// take makes s a checkpoint unless the last one already holds the same tree,
// and returns the checkpoints with the new one first. A partial last
// checkpoint is closed by a full one even when nothing else moved, so "still
// indexing" ends.
func (w *worker) take(ctx context.Context, lim historyLimits, s snapshot, refs []checkpointRef, note string) (checkpointResult, []checkpointRef, error) {
	res := checkpointResult{Partial: s.partial, Warn: s.warn}
	if len(refs) > 0 && refs[0].Tree == s.tree && !refs[0].Meta.Partial {
		res.ID = refs[0].ID
		return res, refs, nil
	}
	meta := checkpointMeta{Partial: s.partial}
	if note = strings.TrimSpace(note); note != "" && json.Valid([]byte(note)) {
		meta.Note = json.RawMessage(note)
	}
	if len(refs) > 0 {
		st, err := w.stat(ctx, refs[0].Commit, s.tree)
		if err != nil {
			return res, refs, err
		}
		meta.Files, meta.Added, meta.Deleted = st.files, st.added, st.deleted
	}
	msg, err := json.Marshal(meta)
	if err != nil {
		return res, refs, err
	}
	out, err := w.git(ctx, msg, nil, "commit-tree", s.tree)
	if err != nil {
		return res, refs, err
	}
	at := time.Now().UnixNano()
	if len(refs) > 0 && at <= refs[0].at() {
		at = refs[0].at() + 1
	}
	ref := checkpointRef{ID: fmt.Sprintf("%019d", at), Commit: strings.TrimSpace(string(out)), Tree: s.tree, Meta: meta}
	if _, err := w.git(ctx, nil, nil, "update-ref", historyRefs+ref.ID, ref.Commit); err != nil {
		return res, refs, err
	}
	res.ID, res.Changed, res.Files = ref.ID, true, meta.Files
	refs = append([]checkpointRef{ref}, refs...)
	return res, refs[:w.expire(ctx, lim, refs)], nil
}

// expire forgets checkpoints past the count and age limits and lets git pack
// what is left; over the size budget the oldest half goes too. It returns how
// many checkpoints remain. Failures are logged: a checkpoint that was taken
// stays taken.
func (w *worker) expire(ctx context.Context, lim historyLimits, refs []checkpointRef) int {
	cutoff := time.Now().AddDate(0, 0, -lim.keepDays).UnixNano()
	keep := len(refs)
	for keep > 2 && (keep > lim.keep || refs[keep-1].at() < cutoff) {
		keep--
	}
	over := keep > 2 && w.storeSize(ctx) > lim.maxStore
	if over {
		keep = max(2, keep/2)
	}
	if keep < len(refs) {
		var del bytes.Buffer
		for _, r := range refs[keep:] {
			fmt.Fprintf(&del, "delete %s%s\n", historyRefs, r.ID)
		}
		if _, err := w.git(ctx, del.Bytes(), nil, "update-ref", "--stdin"); err != nil {
			log.Printf("history expire: %v", err)
			return len(refs)
		}
	}
	// Loose objects pile up one per changed file; gc packs them once there are
	// enough and drops what no checkpoint reaches. The hour's grace keeps the
	// tree of a pending change the pane is still looking at.
	args := []string{"-c", "gc.auto=2000", "-c", "gc.autoDetach=false", "-c", "gc.pruneExpire=1.hour.ago", "gc", "--auto", "--quiet"}
	if over {
		args = []string{"gc", "--quiet", "--prune=now"}
	}
	if _, err := w.git(ctx, nil, nil, args...); err != nil {
		log.Printf("history gc: %v", err)
	}
	return keep
}

// storeSize is the object store's size on disk in bytes.
func (w *worker) storeSize(ctx context.Context) int64 {
	out, err := w.git(ctx, nil, nil, "count-objects", "-v")
	if err != nil {
		return 0
	}
	var kib int64
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(line, ": ")
		if k == "size" || k == "size-pack" {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			kib += n
		}
	}
	return kib << 10
}

type changeRow struct {
	ID      string          `json:"id"`
	At      int64           `json:"at"` // unix nanoseconds
	Base    string          `json:"base"`
	Head    string          `json:"head"`
	Files   int             `json:"files"`
	Added   int             `json:"added"`
	Deleted int             `json:"deleted"`
	Note    json.RawMessage `json:"note,omitempty"`
}

// changes lists the change sets, newest first, and what is pending since the
// last checkpoint. The oldest checkpoint is the baseline: it has nothing to be
// compared with. A change set whose base was partial is the index catching up,
// not a change, and is left out.
func (w *worker) changes(ctx context.Context, cmd *v1.ChangesCmd) (string, error) {
	w.histMu.Lock()
	defer w.histMu.Unlock()
	lim := limitsOf(cmd.GetLimits())
	limit := int(cmd.GetLimit())
	if limit <= 0 || limit > changesDefaultLimit {
		limit = changesDefaultLimit
	}
	s, err := w.snap(ctx, lim)
	if err != nil {
		return "", err
	}
	refs, err := w.checkpoints(ctx)
	if err != nil {
		return "", err
	}
	// With nothing to compare with, history starts here. While the last
	// checkpoint is partial, every look takes the next slice of the index.
	if len(refs) == 0 || refs[0].Meta.Partial {
		if _, refs, err = w.take(ctx, lim, s, refs, ""); err != nil {
			return "", err
		}
	}
	res := struct {
		Changes  []changeRow `json:"changes"`
		Pending  *changeRow  `json:"pending,omitempty"`
		Since    int64       `json:"since"`
		Indexing bool        `json:"indexing,omitempty"`
		Warn     string      `json:"warn,omitempty"`
	}{Changes: []changeRow{}, Warn: s.warn}
	res.Since = refs[len(refs)-1].at()
	res.Indexing = refs[0].Meta.Partial
	if refs[0].Tree != s.tree {
		st, err := w.stat(ctx, refs[0].Commit, s.tree)
		if err != nil {
			return "", err
		}
		if st.files > 0 {
			res.Pending = &changeRow{At: time.Now().UnixNano(), Base: refs[0].Commit, Head: s.tree, Files: st.files, Added: st.added, Deleted: st.deleted}
		}
	}
	for i := 0; i+1 < len(refs) && len(res.Changes) < limit; i++ {
		r, base := refs[i], refs[i+1]
		if base.Meta.Partial || r.Meta.Files == 0 {
			continue
		}
		res.Changes = append(res.Changes, changeRow{
			ID: r.ID, At: r.at(), Base: base.Commit, Head: r.Commit,
			Files: r.Meta.Files, Added: r.Meta.Added, Deleted: r.Meta.Deleted, Note: r.Meta.Note,
		})
	}
	return marshalJSON(res)
}

var historyObject = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

func historyPair(base, head string) error {
	if !historyObject.MatchString(base) || !historyObject.MatchString(head) {
		return errors.New("unknown change")
	}
	return nil
}

type changeFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary,omitempty"`
	Large   bool   `json:"large,omitempty"`
	OldSize int64  `json:"old_size"`
	NewSize int64  `json:"new_size"`

	oldSha, newSha string
}

const zeroSha = "0000000000000000000000000000000000000000"

// changeFiles lists what differs between two history objects: each file with
// its status, line counts and sizes. A file over the size cap reports the sizes
// its stubs recorded.
func (w *worker) changeFiles(ctx context.Context, cmd *v1.ChangeFilesCmd) (string, error) {
	if err := historyPair(cmd.GetBase(), cmd.GetHead()); err != nil {
		return "", err
	}
	w.histMu.Lock()
	defer w.histMu.Unlock()
	if err := w.historyInit(ctx); err != nil {
		return "", err
	}
	max := int(cmd.GetMaxFiles())
	if max <= 0 || max > changeFilesMax {
		max = changeFilesMax
	}
	all, err := w.diffRaw(ctx, cmd.GetBase(), cmd.GetHead(), true)
	if err != nil {
		return "", err
	}
	res := struct {
		Files     []*changeFile `json:"files"`
		Truncated bool          `json:"truncated,omitempty"`
	}{Files: all}
	if len(all) > max {
		res.Files, res.Truncated = all[:max], true
	}
	byPath := map[string]*changeFile{}
	for _, f := range res.Files {
		byPath[f.Path] = f
	}
	num, err := w.git(ctx, nil, nil, "diff-tree", "-r", "-M", "--numstat", "-z", "--no-commit-id", cmd.GetBase(), cmd.GetHead())
	if err != nil {
		return "", err
	}
	for _, n := range parseNumstat(num) {
		if f := byPath[n.path]; f != nil {
			f.Added, f.Deleted, f.Binary = n.added, n.deleted, n.binary
		}
	}
	if err := w.fillSizes(ctx, res.Files); err != nil {
		return "", err
	}
	return marshalJSON(res)
}

// diffRaw lists the paths that differ between two history objects, with the
// blob on each side. With renames off a moved file is its old path deleted and
// its new path added.
func (w *worker) diffRaw(ctx context.Context, base, head string, renames bool) ([]*changeFile, error) {
	mode := "--no-renames"
	if renames {
		mode = "-M"
	}
	raw, err := w.git(ctx, nil, nil, "diff-tree", "-r", mode, "--raw", "-z", "--no-abbrev", "--no-commit-id", base, head)
	if err != nil {
		return nil, errors.New("this change is no longer in the history")
	}
	files := []*changeFile{}
	// ":oldmode newmode oldsha newsha status\0path\0[newpath\0]"
	parts := bytes.Split(raw, []byte{0})
	for i := 0; i+1 < len(parts); i++ {
		head := strings.Fields(strings.TrimPrefix(string(parts[i]), ":"))
		if len(head) < 5 {
			continue
		}
		f := &changeFile{Path: string(parts[i+1]), oldSha: head[2], newSha: head[3], OldSize: -1, NewSize: -1}
		i++
		switch head[4][0] {
		case 'A':
			f.Status = "added"
		case 'D':
			f.Status = "deleted"
		case 'R', 'C':
			f.Status = "renamed"
			if i+1 < len(parts) {
				f.OldPath, f.Path = f.Path, string(parts[i+1])
				i++
			}
		default:
			f.Status = "modified"
		}
		if head[0] == "160000" || head[1] == "160000" {
			f.Status = "repo"
		}
		files = append(files, f)
	}
	return files, nil
}

// fillSizes sets each file's sizes from its blobs, and reads the ones small
// enough to be a stub: a stub stands for a file over the cap and carries its
// real size.
func (w *worker) fillSizes(ctx context.Context, files []*changeFile) error {
	var ask bytes.Buffer
	for _, f := range files {
		if f.Status == "repo" {
			continue
		}
		for _, sha := range []string{f.oldSha, f.newSha} {
			if sha != zeroSha {
				ask.WriteString(sha + "\n")
			}
		}
	}
	if ask.Len() == 0 {
		return nil
	}
	out, err := w.git(ctx, ask.Bytes(), nil, "cat-file", "--batch-check=%(objectname) %(objectsize)")
	if err != nil {
		return err
	}
	size := map[string]int64{}
	var stubs bytes.Buffer
	for _, line := range strings.Split(string(out), "\n") {
		sha, n, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			continue
		}
		size[sha] = v
		if v <= 96 {
			stubs.WriteString(sha + "\n")
		}
	}
	big := map[string]int64{}
	if stubs.Len() > 0 {
		out, err := w.git(ctx, stubs.Bytes(), nil, "cat-file", "--batch")
		if err != nil {
			return err
		}
		// "<sha> blob <size>\n<content>\n" per object.
		for len(out) > 0 {
			nl := bytes.IndexByte(out, '\n')
			if nl < 0 {
				break
			}
			h := strings.Fields(string(out[:nl]))
			if len(h) != 3 {
				break
			}
			n, _ := strconv.Atoi(h[2])
			if nl+1+n > len(out) {
				break
			}
			body := string(out[nl+1 : nl+1+n])
			out = out[min(len(out), nl+2+n):]
			if rest, ok := strings.CutPrefix(body, stubMagic+"size "); ok {
				v, _, _ := strings.Cut(rest, "\n")
				if real, err := strconv.ParseInt(v, 10, 64); err == nil {
					big[h[0]] = real
				}
			}
		}
	}
	side := func(sha string) (int64, bool) {
		if sha == zeroSha {
			return -1, false
		}
		if real, ok := big[sha]; ok {
			return real, true
		}
		return size[sha], false
	}
	for _, f := range files {
		if f.Status == "repo" {
			continue
		}
		var oldBig, newBig bool
		f.OldSize, oldBig = side(f.oldSha)
		f.NewSize, newBig = side(f.newSha)
		if oldBig || newBig {
			f.Large, f.Binary, f.Added, f.Deleted = true, false, 0, 0
		}
	}
	return nil
}

// changePatch returns one file's unified diff between two history objects,
// without the file header (the caller already knows the path).
func (w *worker) changePatch(ctx context.Context, cmd *v1.ChangePatchCmd) (string, error) {
	if err := historyPair(cmd.GetBase(), cmd.GetHead()); err != nil {
		return "", err
	}
	path := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+cmd.GetPath())), "/")
	if path == "" {
		return "", errors.New("path required")
	}
	w.histMu.Lock()
	defer w.histMu.Unlock()
	if err := w.historyInit(ctx); err != nil {
		return "", err
	}
	max := cmd.GetMaxBytes()
	if max <= 0 || max > changePatchMax {
		max = changePatchMax
	}
	args := []string{"diff-tree", "-r", "-M", "-p", "--no-color", "--no-ext-diff", "--no-textconv", "--no-commit-id", "--unified=3", cmd.GetBase(), cmd.GetHead(), "--", path}
	if old := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+cmd.GetOldPath())), "/"); old != "" && old != path {
		args = append(args, old)
	}
	out, err := w.git(ctx, nil, nil, args...)
	if err != nil {
		return "", errors.New("this change is no longer in the history")
	}
	res := struct {
		Patch     string `json:"patch"`
		Truncated bool   `json:"truncated,omitempty"`
		Binary    bool   `json:"binary,omitempty"`
	}{}
	// Everything before the first hunk is the file header.
	if at := bytes.Index(out, []byte("\n@@ ")); at >= 0 {
		out = out[at+1:]
	} else {
		res.Binary = bytes.Contains(out, []byte("\nBinary files "))
		out = nil
	}
	if int64(len(out)) > max {
		out = out[:max]
		if nl := bytes.LastIndexByte(out, '\n'); nl >= 0 {
			out = out[:nl+1]
		}
		res.Truncated = true
	}
	res.Patch = string(out)
	return marshalJSON(res)
}

const (
	restoreSkipList = 20
	restorePathList = 500
)

type restoreSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// restore puts files back as they were at a history object. It works from the
// difference between that object and a checkpoint of the workspace taken just
// now, which is what makes it safe: what is about to be replaced is in the
// history first, so a restore can itself be undone, and only paths the history
// holds are acted on, so a name it was handed that it never tracked (a drive,
// bot/, an ignored folder) is left alone. For the same reason a file over the
// size cap or a nested repository on either side is skipped: its content was
// never kept, so replacing or removing it could not be taken back.
func (w *worker) restore(ctx context.Context, cmd *v1.RestoreCmd) (string, error) {
	to, from := cmd.GetTo(), cmd.GetFrom()
	if !historyObject.MatchString(to) || (from != "" && !historyObject.MatchString(from)) {
		return "", errors.New("unknown change")
	}
	want := map[string]bool{}
	for _, p := range cmd.GetPaths() {
		if p = strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+p)), "/"); p != "" {
			want[p] = true
		}
	}
	if len(want) == 0 && from == "" {
		return "", errors.New("unknown change")
	}
	w.histMu.Lock()
	defer w.histMu.Unlock()
	lim := limitsOf(cmd.GetLimits())
	s, err := w.snap(ctx, lim)
	if err != nil {
		return "", err
	}
	if s.partial {
		return "", errors.New("still reading this workspace for the first time; try again in a moment")
	}
	refs, err := w.checkpoints(ctx)
	if err != nil {
		return "", err
	}
	if _, refs, err = w.take(ctx, lim, s, refs, cmd.GetNoteBefore()); err != nil {
		return "", err
	}
	now := refs[0].Commit
	if len(want) == 0 {
		out, err := w.git(ctx, nil, nil, "diff-tree", "-r", "--no-renames", "--name-only", "-z", "--no-commit-id", to, from)
		if err != nil {
			return "", errors.New("this change is no longer in the history")
		}
		for _, p := range splitNul(out) {
			want[p] = true
		}
	}
	all, err := w.diffRaw(ctx, to, now, false)
	if err != nil {
		return "", err
	}
	var files []*changeFile
	for _, f := range all {
		if want[f.Path] {
			files = append(files, f)
		}
	}
	if err := w.fillSizes(ctx, files); err != nil {
		return "", err
	}

	res := struct {
		Restored     int           `json:"restored"`
		Skipped      []restoreSkip `json:"skipped"`
		SkippedTotal int           `json:"skipped_total"`
		// Paths are the files that were put back (the first restorePathList).
		Paths []string `json:"paths"`
	}{Skipped: []restoreSkip{}, Paths: []string{}}
	skip := func(path, reason string) {
		res.SkippedTotal++
		if len(res.Skipped) < restoreSkipList {
			res.Skipped = append(res.Skipped, restoreSkip{path, reason})
		}
	}
	done := func(path string) {
		res.Restored++
		if len(res.Paths) < restorePathList {
			res.Paths = append(res.Paths, path)
		}
	}
	// A path that was not there then is removed first, so a folder that became
	// a file (or a link) is out of the way before its files are written.
	var write []string
	for _, f := range files {
		switch {
		case f.Status == "repo":
			skip(f.Path, "repo")
		case f.Large:
			skip(f.Path, "large")
		case f.oldSha != zeroSha:
			write = append(write, f.Path)
		case !w.restoreRemove(f.Path):
			skip(f.Path, "blocked")
		default:
			done(f.Path)
		}
	}
	var clear []string
	for _, p := range write {
		if w.restoreBlocked(p) {
			skip(p, "blocked")
			continue
		}
		clear = append(clear, p)
	}
	if len(clear) > 0 {
		// Written by git from a throwaway index of the old tree: it knows the
		// exec bit and symlinks, and replaces a file instead of writing into it.
		tmp, err := os.MkdirTemp(w.history, "restore-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		idx := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
		if _, err := w.git(ctx, nil, idx, "read-tree", to); err != nil {
			return "", errors.New("this change is no longer in the history")
		}
		if _, err := w.git(ctx, joinNul(clear), idx, "checkout-index", "-f", "-z", "--stdin"); err != nil {
			return "", err
		}
		for _, p := range clear {
			done(p)
		}
	}
	if res.Restored > 0 {
		if s, err = w.snap(ctx, lim); err != nil {
			return "", err
		}
		if _, _, err = w.take(ctx, lim, s, refs, cmd.GetNote()); err != nil {
			return "", err
		}
	}
	return marshalJSON(res)
}

// realParents reports whether every folder above rel is a real directory or
// not there yet. A link there could lead out of the workspace, and a file there
// is somebody's content.
func (w *worker) realParents(rel string) bool {
	dir := w.workspace
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, part)
		st, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

// restoreBlocked reports that rel cannot be written: something that is not the
// history's to replace is in the way. An empty folder at the path is removed.
func (w *worker) restoreBlocked(rel string) bool {
	if !w.realParents(rel) {
		return true
	}
	abs := filepath.Join(w.workspace, filepath.FromSlash(rel))
	if st, err := os.Lstat(abs); err == nil && st.IsDir() {
		return os.Remove(abs) != nil
	}
	return false
}

// restoreRemove removes a file that was not there at the restored state, and
// the folders it leaves empty (git keeps no empty folders, so they came with
// it). It reports false when the file had to stay.
func (w *worker) restoreRemove(rel string) bool {
	if !w.realParents(rel) {
		return false
	}
	abs := filepath.Join(w.workspace, filepath.FromSlash(rel))
	if st, err := os.Lstat(abs); err == nil && st.IsDir() {
		return false
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false
	}
	for dir := filepath.Dir(abs); dir != w.workspace && strings.HasPrefix(dir, w.workspace); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return true
}

func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
