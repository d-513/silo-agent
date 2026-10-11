package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/v2"

	"silo.agent/internal/netguard"
)

// Outbound limits the connections the control plane makes for a Bot's owner:
// a connector's server and its OAuth endpoints, a mail or calendar account, a
// skill's download address, a drive provider's sign-in. The models, the search
// engines, OIDC and whatever else the operator configured are not its
// business, and neither is what a Bot's own machine reaches.
type Outbound struct {
	// BlockPrivate refuses loopback, private, link-local and other internal
	// addresses, and names that resolve to one.
	BlockPrivate bool `koanf:"block_private"`
	// Allow lists the internal addresses that stay reachable: comma-separated
	// IPs or CIDRs, each optionally with one port.
	Allow string `koanf:"allow"`
}

// OutboundPolicy is the policy for connections made on a user's word. A bad allow
// entry (possible only from the environment, which is not validated) opens
// nothing.
func (c Config) OutboundPolicy() netguard.Policy {
	allow, err := netguard.ParseAllow(c.Outbound.Allow)
	if err != nil {
		allow = nil
	}
	return netguard.Policy{Block: c.Outbound.BlockPrivate, Allow: allow}
}

var outboundKeys = map[string]bool{"outbound.block_private": true, "outbound.allow": true}

// validateOutbound checks the outbound.* keys of a YAML document being written.
func validateOutbound(k *koanf.Koanf) error {
	for _, key := range k.Keys() {
		if strings.HasPrefix(key, "outbound.") && !outboundKeys[key] {
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	switch v := k.Get("outbound.block_private").(type) {
	case nil, bool:
	case string:
		if v != "true" && v != "false" && v != "" {
			return fmt.Errorf("outbound.block_private must be true or false")
		}
	default:
		return fmt.Errorf("outbound.block_private must be true or false")
	}
	switch k.Get("outbound.allow").(type) {
	case nil, string:
	default:
		return fmt.Errorf("outbound.allow must be one comma-separated string of IPs and CIDRs")
	}
	if _, err := netguard.ParseAllow(k.String("outbound.allow")); err != nil {
		return fmt.Errorf("outbound.allow: %w", err)
	}
	return nil
}
