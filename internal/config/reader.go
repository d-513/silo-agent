package config

import (
	"fmt"

	"github.com/knadh/koanf/v2"

	"silo.agent/internal/netguard"
)

// Reader configures the page reader (silo_runtime.web_read): the pages the
// control plane fetches because a Bot named their address.
type Reader struct {
	// BlockPrivate refuses loopback, private, link-local and other internal
	// addresses, and names that resolve to one. On by default: the address
	// comes from a model, which may be repeating what a web page told it.
	BlockPrivate bool `koanf:"block_private"`
	// Allow lists the internal addresses that stay readable: comma-separated
	// IPs or CIDRs, each optionally with one port.
	Allow string `koanf:"allow"`
	// Render lets a page that needs its scripts be rendered by the headless
	// browser in the Bot's own machine.
	Render bool `koanf:"render"`
	// GitHubToken lifts GitHub's limit on API calls made without one.
	GitHubToken string `koanf:"github_token"`
}

// ReaderPolicy is what the page reader may connect to. A bad allow entry
// (possible only from the environment, which is not validated) opens nothing.
func (c Config) ReaderPolicy() netguard.Policy {
	allow, err := netguard.ParseAllow(c.Reader.Allow)
	if err != nil {
		allow = nil
	}
	return netguard.Policy{Block: c.Reader.BlockPrivate, Allow: allow}
}

// validateReader checks the reader.* keys of a YAML document being written.
func validateReader(k *koanf.Koanf) error {
	for _, key := range []string{"reader.block_private", "reader.render"} {
		switch v := k.Get(key).(type) {
		case nil, bool:
		case string:
			if v != "true" && v != "false" && v != "" {
				return fmt.Errorf("%s must be true or false", key)
			}
		default:
			return fmt.Errorf("%s must be true or false", key)
		}
	}
	switch k.Get("reader.allow").(type) {
	case nil, string:
	default:
		return fmt.Errorf("reader.allow must be one comma-separated string of IPs and CIDRs")
	}
	if _, err := netguard.ParseAllow(k.String("reader.allow")); err != nil {
		return fmt.Errorf("reader.allow: %w", err)
	}
	return nil
}
