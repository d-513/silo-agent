//go:build live

package llm

import (
	"context"
	"testing"
	"time"
)

// TestLiveClaudeInstall downloads the real Claude Code from npm (a few
// hundred MB) and runs its --version. Run it after touching the installer or
// when the package's layout may have moved:
//
//	go test -tags live ./internal/llm -run TestLiveClaudeInstall -v
func TestLiveClaudeInstall(t *testing.T) {
	c := &claudeClient{conf: claudeConf{dir: t.TempDir()}}
	if err := c.InstallProgram(context.Background()); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(15 * time.Minute); time.Now().Before(end); time.Sleep(time.Second) {
		st := c.ProgramStatus(context.Background())
		if st.Installing {
			continue
		}
		if !st.Installed || !st.Managed || st.Version == "" || st.Problem != "" {
			t.Fatalf("%+v", st)
		}
		t.Logf("installed Claude Code %s (%d bytes downloaded) at %s", st.Version, st.Done, st.Path)
		return
	}
	t.Fatal("the install never ended")
}
