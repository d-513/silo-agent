package version

import "testing"

func TestImageFollowsTheBuild(t *testing.T) {
	defer func(v, r string) { Version, Registry = v, r }(Version, Registry)

	Version, Registry = "dev", ""
	if !Dev() || Image("silo-bot") != "localhost/silo-bot:dev" {
		t.Fatalf("dev build: Dev=%v image=%q", Dev(), Image("silo-bot"))
	}
	Version, Registry = "", ""
	if got := Image("silo-egress"); got != "localhost/silo-egress:dev" {
		t.Fatalf("empty version: %q", got)
	}
	Version, Registry = "v0.4.2", "ghcr.io/d-513/"
	if Dev() || Image("silo-drive") != "ghcr.io/d-513/silo-drive:v0.4.2" {
		t.Fatalf("prod build: Dev=%v image=%q", Dev(), Image("silo-drive"))
	}
	Version = "master-abc1234"
	if got := Image("silo-mcp-stdio"); got != "ghcr.io/d-513/silo-mcp-stdio:master-abc1234" {
		t.Fatalf("nightly: %q", got)
	}
}
