//go:build windows

package containers

import (
	"context"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

// dockerPipe is the Engine API's default Windows named pipe.
const dockerPipe = `\\.\pipe\docker_engine`

// defaultDockerEndpoint resolves the Engine API named pipe: DOCKER_HOST
// when it names one (npipe://...), else the well-known default. It
// returns "" when the pipe can't be reached at all, so a Windows box
// with no Docker shows nothing rather than a spurious "daemon not
// responding" — the same closed-fail behaviour fileExists gives on unix.
func defaultDockerEndpoint() string {
	pipe := dockerPipe
	if dh := os.Getenv("DOCKER_HOST"); strings.HasPrefix(dh, "npipe://") {
		// npipe:////./pipe/docker_engine -> \\.\pipe\docker_engine
		pipe = strings.ReplaceAll(strings.TrimPrefix(dh, "npipe://"), "/", `\`)
	}
	timeout := 150 * time.Millisecond
	c, err := winio.DialPipe(pipe, &timeout)
	if err != nil {
		return ""
	}
	_ = c.Close()
	return pipe
}

func dialDockerDefault(ctx context.Context, pipe string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipe)
}
