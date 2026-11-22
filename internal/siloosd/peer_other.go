//go:build !linux

package siloosd

import (
	"errors"
	"net"
)

// peerOf needs SO_PEERCRED: siloosd runs on Linux only.
func peerOf(net.Conn) (Peer, error) { return Peer{}, errors.New("peer credentials need Linux") }
