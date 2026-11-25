package siloosd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// FirstPasswordRetired is the file, in the state folder, that says the first
// admin's generated password no longer signs anyone in. siloos-init shows
// that password on the console at every boot until it exists
// (images/os/rootfs/usr/libexec/siloos/init reads this same path).
const FirstPasswordRetired = "first-password-retired"

// RetireFirstPassword takes the generated first password off the console:
// the marker for every later boot, then the banner written again and the
// login prompts redrawn, so it is gone from a screen left on too. It writes
// one fixed file and runs two fixed commands; the caller chooses nothing.
func (s *Server) RetireFirstPassword(ctx context.Context, _ *connect.Request[v1.RetireFirstPasswordRequest]) (*connect.Response[v1.RetireFirstPasswordResponse], error) {
	marker := filepath.Join(s.state, FirstPasswordRetired)
	if _, err := os.Stat(marker); err != nil {
		if err := os.MkdirAll(s.state, 0o700); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		// It holds a time, no secret; only siloos-init, as root, reads it.
		if err := os.WriteFile(marker, []byte(s.now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		audit(ctx, "the first password was changed; it is off the console")
	}
	if _, err := s.run.Run(ctx, "siloos-init"); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("the marker is written, but the banner was not: %w", err))
	}
	// A prompt already on screen keeps the old banner until told.
	if _, err := s.run.Run(ctx, "agetty", "--reload"); err != nil {
		fmt.Fprintf(os.Stderr, "siloosd: redraw the login prompts: %v\n", err)
	}
	return connect.NewResponse(&v1.RetireFirstPasswordResponse{}), nil
}
