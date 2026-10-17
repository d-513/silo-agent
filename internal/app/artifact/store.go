package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

// The caps of one pinned version. A file is what present may show; a folder
// is what a skill install may hold.
const (
	maxFileBytes = 32 << 20
	maxTreeFiles = skills.MaxExtractFiles
	maxTreeBytes = skills.MaxExtractBytes
)

// ErrAltered is a read whose bytes do not hash to the id asked for: someone
// changed the store on the Bot's machine.
var ErrAltered = errors.New("this version was changed on the Bot's machine and cannot be shown")

// ErrGone is a version the store no longer keeps.
var ErrGone = errors.New("this version is no longer kept: the Bot's artifact store made room for newer ones")

type entry struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
	Mode uint32 `json:"mode"`
}

type manifest struct {
	V       int     `json:"v"`
	Entries []entry `json:"entries"`
}

// Preview is a card's look at a version, made once when it is pinned.
type Preview struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
	More  bool   `json:"more,omitempty"`
}

type pinned struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Size     int64    `json:"size"`
	Entries  []entry  `json:"entries"`
	Preview  *Preview `json:"preview"`
	TooLarge string   `json:"too_large"`
	Evicted  []string `json:"evicted"`
}

func (s *Service) pin(ctx context.Context, botID, rel string, asTree bool, requires []string) (pinned, error) {
	raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_ArtifactPin{ArtifactPin: &v1.ArtifactPinCmd{
		Path: rel, AsTree: asTree, TreeRequires: requires,
		MaxFileBytes: maxFileBytes, MaxTreeFiles: maxTreeFiles, MaxTreeBytes: maxTreeBytes,
		MaxStoreBytes: s.cfg().Artifacts.MaxBytes(), Preview: true,
	}}})
	if err != nil {
		if strings.Contains(err.Error(), "unknown cmd") {
			return pinned{}, errors.New("this Bot's machine is too old to keep artifacts: reset its container (Settings → Containers)")
		}
		return pinned{}, err
	}
	var p pinned
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return pinned{}, err
	}
	if p.ID != "" && !validID(p.ID) {
		return pinned{}, errors.New("the Bot's machine answered a bad artifact id")
	}
	return p, nil
}

func validID(id string) bool {
	if len(id) != 64 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// blob reads one blob of a Bot's store and checks it is what id names.
// Immutable, so a small cache keeps what was read recently.
func (s *Service) blob(ctx context.Context, botID, id string) ([]byte, error) {
	if !validID(id) {
		return nil, errors.New("bad artifact id")
	}
	if b, ok := s.cache.get(botID, id); ok {
		return b, nil
	}
	raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_ArtifactRead{ArtifactRead: &v1.ArtifactReadCmd{Id: id}}})
	if err != nil {
		if strings.Contains(err.Error(), "not kept") {
			return nil, ErrGone
		}
		return nil, err
	}
	var row struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(row.Data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != id {
		log.Printf("artifact %s on bot %s: bytes do not match the id", id, botID)
		return nil, ErrAltered
	}
	s.cache.put(botID, id, data)
	return data, nil
}

// manifestOf reads a folder version's file list.
func (s *Service) manifestOf(ctx context.Context, botID, id string) (manifest, error) {
	raw, err := s.blob(ctx, botID, id)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return manifest{}, fmt.Errorf("artifact manifest: %w", err)
	}
	for _, e := range m.Entries {
		if !validID(e.SHA) || !safeRel(e.Path) {
			return manifest{}, ErrAltered
		}
	}
	return m, nil
}

func safeRel(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	return path.Clean(p) == p && p != "." && !strings.HasPrefix(p, "../") && p != ".."
}

// treeFiles reads every file of a folder version (path to bytes).
func (s *Service) treeFiles(ctx context.Context, botID, id string) (map[string][]byte, error) {
	m, err := s.manifestOf(ctx, botID, id)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(m.Entries))
	for _, e := range m.Entries {
		b, err := s.blob(ctx, botID, e.SHA)
		if err != nil {
			return nil, err
		}
		out[e.Path] = b
	}
	return out, nil
}

// fileOf reads one file of a version: the version itself for a file, path in
// it for a folder.
func (s *Service) fileOf(ctx context.Context, v *db.ArtifactVersion, p string) (string, []byte, error) {
	if v.Gone {
		return "", nil, ErrGone
	}
	if v.Kind != "tree" {
		b, err := s.blob(ctx, v.BotID, v.Blob)
		return v.Name, b, err
	}
	m, err := s.manifestOf(ctx, v.BotID, v.Blob)
	if err != nil {
		return "", nil, err
	}
	for _, e := range m.Entries {
		if e.Path == p {
			b, err := s.blob(ctx, v.BotID, e.SHA)
			return path.Base(p), b, err
		}
	}
	return "", nil, errNoFile
}

var errNoFile = errors.New("no such file in this version")

func (s *Service) forget(ctx context.Context, botID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	s.cache.drop(botID)
	_, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_ArtifactForget{ArtifactForget: &v1.ArtifactForgetCmd{Ids: ids}}})
	return err
}

// blobCache holds recently read blobs, which never change: an app's files are
// asked for on every page load.
type blobCache struct {
	mu    sync.Mutex
	order []string
	items map[string][]byte
	size  int
}

const cacheBytes = 64 << 20

func (c *blobCache) get(botID, id string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.items[botID+"/"+id]
	return b, ok
}

func (c *blobCache) put(botID, id string, b []byte) {
	if len(b) > cacheBytes/8 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string][]byte{}
	}
	k := botID + "/" + id
	if _, ok := c.items[k]; ok {
		return
	}
	c.items[k] = b
	c.order = append(c.order, k)
	c.size += len(b)
	for c.size > cacheBytes && len(c.order) > 0 {
		old := c.order[0]
		c.order = c.order[1:]
		c.size -= len(c.items[old])
		delete(c.items, old)
	}
}

func (c *blobCache) drop(botID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.order[:0]
	for _, k := range c.order {
		if strings.HasPrefix(k, botID+"/") {
			c.size -= len(c.items[k])
			delete(c.items, k)
			continue
		}
		kept = append(kept, k)
	}
	c.order = kept
}
