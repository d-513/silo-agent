package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"silo.agent/internal/builtin"
	"silo.agent/internal/catalog"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/drives"
	"silo.agent/internal/textx"
)

// A setup block in an ask offers the owner something from Silo's own catalog
// to add: a library connector, a channel adapter or a drive provider. The Bot
// may fill in a few plain fields (an email address, a provider choice), and
// the owner gets the normal add form with them filled in and submits it
// themselves, so the click is their consent and no approval is needed.
//
// What keeps a misled Bot from using it to point Silo somewhere: targets are
// catalog entries only (never a custom MCP URL), and prefill keeps only
// declared fields that are neither secret nor an address.
const (
	setupConnector = "connector"
	setupChannel   = "channel"
	setupDrive     = "drive"

	prefillMax = 200
)

// setupDone and setupSkipped are what the owner did with one setup block.
const (
	setupDone    = "done"
	setupSkipped = "skipped"
)

// addressy are field keys that name where to connect: never prefilled.
var addressy = []string{"url", "host", "server", "port", "endpoint", "domain", "address_book", "base"}

func addressField(key string) bool {
	k := strings.ToLower(key)
	for _, a := range addressy {
		if strings.Contains(k, a) {
			return true
		}
	}
	return false
}

// checkSetup validates a setup block in place: the target must be in the
// catalog, its display name is set from the catalog (never the Bot's), and
// prefill keeps only plain declared fields.
func (s *Service) checkSetup(blk map[string]any) error {
	kind, _ := blk["kind"].(string)
	target := strings.TrimSpace(fmt.Sprint(blk["target"]))
	if target == "" || target == "<nil>" {
		return errors.New("a setup block needs a target")
	}
	raw, _ := blk["prefill"].(map[string]any)
	plain := map[string]bool{}
	var label string
	switch kind {
	case setupConnector:
		var c db.Connector
		s.db.Where("kind = ? AND identifier = ?", catalog.KindLibrary, target).Limit(1).Find(&c)
		if c.ID == "" {
			return fmt.Errorf("no connector %q in the library; use a library identifier", target)
		}
		label = c.Name
		if d, ok := builtin.Lookup(c.Builtin); ok && c.Builtin != "" {
			for _, f := range d.Descriptor().Fields {
				if f.Type != builtin.FieldSecret && !addressField(f.Key) {
					plain[f.Key] = true
				}
			}
		}
	case setupChannel:
		ad, ok := channels.Lookup(target)
		if !ok {
			return fmt.Errorf("no channel adapter %q", target)
		}
		d := ad.Descriptor()
		label = d.Name
		for _, f := range d.Fields {
			if f.Type != channels.FieldSecret && !addressField(f.Key) {
				plain[f.Key] = true
			}
		}
	case setupDrive:
		t, ok := drives.Builtin().Get(target)
		if !ok {
			return fmt.Errorf("no drive provider %q", target)
		}
		label = t.Title
		for _, v := range t.Vars {
			if v.Kind == "user" && !v.Secret && v.Type != "password" && v.Type != "hidden" && !addressField(v.Key) {
				plain[v.Key] = true
			}
		}
	default:
		return errors.New(`a setup block's kind is "connector", "channel" or "drive"`)
	}
	prefill := map[string]string{}
	for k, v := range raw {
		if !plain[k] {
			continue
		}
		val := strings.TrimSpace(fmt.Sprint(v))
		if val != "" {
			prefill[k] = textx.ClipRunes(val, prefillMax)
		}
	}
	why, _ := blk["why"].(string)
	clean := map[string]any{"type": "setup", "kind": kind, "target": target, "label": label, "why": textx.ClipRunes(strings.TrimSpace(why), 300)}
	if len(prefill) > 0 {
		clean["prefill"] = prefill
	}
	for k := range blk {
		delete(blk, k)
	}
	for k, v := range clean {
		blk[k] = v
	}
	return nil
}

// setupLabel is the catalog name of a card's setup block at index i.
func setupLabel(c *db.DashboardCard, i int) (string, bool) {
	var blocks []map[string]any
	_ = json.Unmarshal([]byte(c.BlocksJSON), &blocks)
	if i < 0 || i >= len(blocks) || blocks[i]["type"] != "setup" {
		return "", false
	}
	label, _ := blocks[i]["label"].(string)
	return label, true
}
