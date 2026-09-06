//go:build !windows

package containers

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// defaultDockerEndpoint resolves the Engine API socket the way the
// docker CLI does: DOCKER_HOST when it names a unix socket, then the
// well-known daemon path and the rootless / Colima / Rancher Desktop
// fallbacks. "" when nothing is present.
func defaultDockerEndpoint() string {
	if dh := os.Getenv("DOCKER_HOST"); strings.HasPrefix(dh, "unix://") {
		if p := strings.TrimPrefix(dh, "unix://"); fileExists(p) {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".docker/run/docker.sock"),
		filepath.Join(home, ".colima/default/docker.sock"),
		filepath.Join(home, ".rd/docker.sock"),
	} {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

func dialDockerDefault(ctx context.Context, socket string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", socket)
}
