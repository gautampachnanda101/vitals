package info

import (
	"os"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/host"

	"vitals/internal/ui"
)

func TestHumanUptime(t *testing.T) {
	cases := []struct {
		secs uint64
		want string
	}{
		{45, "0m"},
		{90, "1m"},
		{3661, "1h 1m"},
		{90000, "1d 1h 0m"},
	}
	for _, c := range cases {
		if got := humanUptime(c.secs); got != c.want {
			t.Errorf("humanUptime(%d) = %q, want %q", c.secs, got, c.want)
		}
	}
}

func TestCollectFillsBinaryFromRuntime(t *testing.T) {
	r := Collect("v1.2.3")
	if r.Binary.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", r.Binary.Version)
	}
	if r.Binary.GoVersion == "" || r.Binary.OS == "" || r.Binary.Arch == "" {
		t.Errorf("Binary fields from runtime should never be empty: %+v", r.Binary)
	}
}

func TestCollectFillsMachineFromInjectedHostInfo(t *testing.T) {
	old := hostInfoFn
	defer func() { hostInfoFn = old }()
	hostInfoFn = func() (*host.InfoStat, error) {
		return &host.InfoStat{Hostname: "test-host", Platform: "testos", PlatformVersion: "9.9", KernelArch: "x86_64", Uptime: 7200}, nil
	}

	r := Collect("v1")
	if r.Machine.Hostname != "test-host" {
		t.Errorf("Hostname = %q, want test-host", r.Machine.Hostname)
	}
	if r.Machine.OS != "testos 9.9" {
		t.Errorf("OS = %q, want %q", r.Machine.OS, "testos 9.9")
	}
	if r.Machine.UptimeSecs != 7200 {
		t.Errorf("UptimeSecs = %d, want 7200", r.Machine.UptimeSecs)
	}
}

func TestCollectLeavesMachineEmptyWhenHostInfoFails(t *testing.T) {
	old := hostInfoFn
	defer func() { hostInfoFn = old }()
	hostInfoFn = func() (*host.InfoStat, error) { return nil, os.ErrPermission }

	r := Collect("v1")
	if r.Machine.Hostname != "" || r.Machine.OS != "" {
		t.Errorf("Machine should stay zero-valued when hostInfoFn fails, got %+v", r.Machine)
	}
}

func TestCollectLeavesBinaryPathEmptyWhenExecutableFails(t *testing.T) {
	old := executableFn
	defer func() { executableFn = old }()
	executableFn = func() (string, error) { return "", os.ErrNotExist }

	r := Collect("v1")
	if r.Binary.Path != "" {
		t.Errorf("Binary.Path should be empty when executableFn fails, got %q", r.Binary.Path)
	}
}

func TestCollectReportsWhetherTheConfigFileActuallyExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "")

	r := Collect("v1")
	if r.Config.Exists {
		t.Errorf("Config.Exists should be false with no config file present, got true (path %q)", r.Config.Path)
	}
	if r.Config.Path == "" {
		t.Error("Config.Path should still be populated even when the file doesn't exist")
	}
}

func TestRenderIncludesEveryMajorSection(t *testing.T) {
	r := Collect("v9.9.9")
	out := ui.StripANSI(Render(r))
	for _, want := range []string{"Binary", "version", "v9.9.9", "Machine", "cores", "Config", "disk warn"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render missing %q, got:\n%s", want, out)
		}
	}
}

func TestRenderOmitsPathWhenExecutableFailed(t *testing.T) {
	old := executableFn
	defer func() { executableFn = old }()
	executableFn = func() (string, error) { return "", os.ErrNotExist }

	r := Collect("v1")
	out := ui.StripANSI(Render(r))
	binarySection := strings.SplitN(out, "Machine", 2)[0]
	if strings.Contains(binarySection, "path") {
		t.Errorf("Binary section should omit the path line when executableFn failed, got:\n%s", binarySection)
	}
}
