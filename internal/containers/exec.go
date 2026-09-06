package containers

import (
	"context"
	"os/exec"
)

// execLookPath / execRun are the real PATH lookup and subprocess call
// behind transport; pulled into their own file so the rest of the
// package has no direct os/exec dependency to reason about.
func execLookPath(file string) (string, error) { return exec.LookPath(file) }

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
