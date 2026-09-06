//go:build windows

package containers

import (
	"os"
	"testing"
)

func TestDefaultDockerEndpointWindowsParsesDockerHostNpipe(t *testing.T) {
	// A DOCKER_HOST npipe URL is normalised to a \\.\pipe\ path. The pipe
	// almost certainly isn't dial-able on a CI runner, so the function
	// returns "" — but it must not panic and must not return the unix
	// form or the raw URL.
	t.Setenv("DOCKER_HOST", "npipe:////./pipe/some_custom_engine")
	got := defaultDockerEndpoint()
	if got != "" && got != `\\.\pipe\some_custom_engine` {
		t.Errorf("defaultDockerEndpoint() = %q, want \"\" or the normalised pipe path", got)
	}

	os.Unsetenv("DOCKER_HOST")
	// No DOCKER_HOST: either the real Docker pipe answers on this runner
	// (returns the default pipe path) or it doesn't (returns ""). Both are
	// valid; assert only that it's one of those two, never a garbled value.
	got = defaultDockerEndpoint()
	if got != "" && got != dockerPipe {
		t.Errorf("defaultDockerEndpoint() = %q, want \"\" or %q", got, dockerPipe)
	}
}

func TestDefaultTransportWindowsWiring(t *testing.T) {
	if defaultTransport.dockerEndpoint == nil || defaultTransport.dialDocker == nil {
		t.Fatal("Windows defaultTransport is missing its endpoint/dial wiring")
	}
	// Dialing a pipe that doesn't exist must error promptly, not hang.
	if _, err := dialDockerDefault(t.Context(), `\\.\pipe\vitals-nonexistent-test-pipe`); err == nil {
		t.Error("dialing a missing named pipe should error")
	}
}
