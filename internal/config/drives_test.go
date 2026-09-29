package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriveSystemValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, YAMLName)
	if err := os.WriteFile(path, []byte("drives:\n  providers:\n    gdrive:\n      client_id: from-yaml\n      client_secret: yaml-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SILO_DRIVES__PROVIDERS__GDRIVE__CLIENT_SECRET", "env-secret")
	s, err := LoadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.DriveSystem("gdrive")
	if got["client_id"] != "from-yaml" || got["client_secret"] != "env-secret" {
		t.Fatalf("DriveSystem = %v", got)
	}
	if s.Source(DriveSystemKey("gdrive", "client_secret")) != SourceEnv {
		t.Fatal("env must win for drive system values")
	}
	if !KnownKey("drives.providers.onedrive.client_id") || KnownKey("drives.providers.onedrive.nope") || KnownKey("drives.providers.nope.client_id") {
		t.Fatal("KnownKey drive system keys wrong")
	}
	if err := s.Patch(map[string]string{DriveSystemKey("dropbox", "client_id"): "dbx"}); err != nil {
		t.Fatal(err)
	}
	if s.DriveSystem("dropbox")["client_id"] != "dbx" {
		t.Fatal("Patch did not write the drive value")
	}
	err = s.WriteYAML([]byte("drives:\n  providers:\n    gdrive:\n      clientid: typo\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown drive setting") {
		t.Fatalf("typo accepted: %v", err)
	}
	c := s.Config()
	if c.DriveImage() != DefaultDriveImage || c.Drives.CacheMaxSize != "10G" || c.DriveMountRoot() == "" {
		t.Fatalf("drive defaults: %q %q %q", c.DriveImage(), c.Drives.CacheMaxSize, c.DriveMountRoot())
	}
}
