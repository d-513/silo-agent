package mcpbridge

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CAFile is where the sidecar installs the network filter's certificate: the
// Debian local store, which update-ca-certificates adds to the system bundle.
// The CP's env points Node (NODE_EXTRA_CA_CERTS) at this file and requests,
// httpx and pip at the bundle. It matches dockerx.EgressCAFile.
const CAFile = "/usr/local/share/ca-certificates/silo-egress.crt"

// InstallCA trusts the network filter's certificate before the child starts:
// the gateway decrypts HTTPS and signs what it hands back with it. An empty
// pem is no filter, and nothing to do. The bridge runs as root in its own
// sidecar, so it can write the system store.
func InstallCA(pem string) error {
	return installCA(pem, CAFile, func() error {
		out, err := exec.Command("update-ca-certificates").CombinedOutput()
		if err != nil {
			return fmt.Errorf("update-ca-certificates: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	})
}

func installCA(pem, path string, update func() error) error {
	pem = strings.TrimSpace(pem)
	if pem == "" {
		return nil
	}
	if !strings.HasPrefix(pem, "-----BEGIN CERTIFICATE-----") {
		return errors.New("SILO_EGRESS_CA is not a PEM certificate")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(pem+"\n"), 0o644); err != nil {
		return err
	}
	return update()
}
