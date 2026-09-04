package info

import (
	"os"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/host"

	"vitals/internal/config"
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
	for _, want := range []string{"Binary", "version", "v9.9.9", "Machine", "cores", "Config", "disk_warn_percent"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render missing %q, got:\n%s", want, out)
		}
	}
}

func TestRenderConfigAlwaysListsEveryKeyItsValueAndSource(t *testing.T) {
	r := Report{Config: ConfigStatus{
		Path:   "/x/config.toml",
		Exists: false,
		Active: Config_{DiskWarnPercent: 90, DiskCriticalPercent: 97, RAMWarnPercent: 78, RAMHighPercent: 90, CPUOversubscribeMult: 2},
	}}
	out := ui.StripANSI(Render(r))
	for _, want := range []string{
		"disk_warn_percent", "disk_critical_percent", "ram_warn_percent",
		"ram_high_percent", "cpu_oversubscribe_multiplier", "ollama_url",
		"key", "value", "source",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the Config block must list %q, got:\n%s", want, out)
		}
	}
	// ollama_url has no default — it renders as (unset), not a blank.
	if !strings.Contains(out, "(unset)") {
		t.Errorf("an unset ollama_url should render as (unset), got:\n%s", out)
	}
}

func TestRenderMissingConfigPointsAtDoctorAndTheGuide(t *testing.T) {
	r := Report{Config: ConfigStatus{Path: "/some/where/config.toml", Exists: false}}
	out := ui.StripANSI(Render(r))
	if !strings.Contains(out, "not created yet") {
		t.Errorf("a missing config should say so plainly, got:\n%s", out)
	}
	if !strings.Contains(out, "vitals doctor") {
		t.Errorf("a missing config should point at `vitals doctor` to create one, got:\n%s", out)
	}
	if !strings.Contains(out, "vitals guide") {
		t.Errorf("the block should always point at `vitals guide` for the key reference, got:\n%s", out)
	}
}

func TestRenderPresentConfigWithNoChangesSaysEveryValueIsStillDefault(t *testing.T) {
	// A scaffolded file (all keys written out at their defaults): it's in
	// use, but nothing differs from stock yet.
	r := Report{Config: ConfigStatus{Path: "/x/config.toml", Exists: true, Overrides: nil}}
	out := ui.StripANSI(Render(r))
	if !strings.Contains(out, "every value still at its built-in default") {
		t.Errorf("a present, unchanged config should say every value is still default, got:\n%s", out)
	}
	if strings.Contains(out, "not created yet") {
		t.Errorf("the file exists — don't say it's not created, got:\n%s", out)
	}
	if strings.Contains(out, "set in file") {
		t.Errorf("nothing changed, so no row should be sourced 'set in file', got:\n%s", out)
	}
}

func TestRenderChangedConfigCountsAndSourcesTheChangedRows(t *testing.T) {
	r := Report{Config: ConfigStatus{
		Path:      "/x/config.toml",
		Exists:    true,
		Overrides: []string{"disk_warn_percent", "cpu_oversubscribe_multiplier"},
		Active:    Config_{DiskWarnPercent: 80, DiskCriticalPercent: 97, RAMWarnPercent: 78, RAMHighPercent: 90, CPUOversubscribeMult: 3},
	}}
	out := ui.StripANSI(Render(r))
	if !strings.Contains(out, "2 values changed from their defaults") {
		t.Errorf("status should count the changed values, got:\n%s", out)
	}

	// A single change uses the singular phrasing.
	one := r
	one.Config.Overrides = []string{"disk_warn_percent"}
	if s := ui.StripANSI(Render(one)); !strings.Contains(s, "1 value changed from its default") {
		t.Errorf("a single change should read '1 value changed from its default', got:\n%s", s)
	}
	for _, ln := range strings.Split(out, "\n") {
		fields := strings.Fields(ln)
		if len(fields) < 3 {
			continue
		}
		switch fields[0] {
		case "disk_warn_percent", "cpu_oversubscribe_multiplier":
			if !strings.Contains(ln, "set in file") {
				t.Errorf("%s was changed — its row should read 'set in file': %q", fields[0], ln)
			}
		case "disk_critical_percent", "ram_warn_percent", "ram_high_percent":
			if !strings.Contains(ln, "default") {
				t.Errorf("%s was not changed — its row should read 'default': %q", fields[0], ln)
			}
		}
	}
}

func TestOverriddenKeysDetectsEachFieldAndReturnsNilForDefaults(t *testing.T) {
	def := config.Default()
	if got := overriddenKeys(def, def); got != nil {
		t.Errorf("identical configs should yield no overrides, got %v", got)
	}

	// Flip one field at a time and confirm exactly its key comes back.
	perField := []struct {
		key    string
		mutate func(*config.Config)
	}{
		{"disk_warn_percent", func(c *config.Config) { c.DiskWarnPercent++ }},
		{"disk_critical_percent", func(c *config.Config) { c.DiskCriticalPercent++ }},
		{"ram_warn_percent", func(c *config.Config) { c.RAMWarnPercent++ }},
		{"ram_high_percent", func(c *config.Config) { c.RAMHighPercent++ }},
		{"cpu_oversubscribe_multiplier", func(c *config.Config) { c.CPUOversubscribeMult++ }},
		{"ollama_url", func(c *config.Config) { c.OllamaURL = "http://x:1" }},
	}
	for _, pf := range perField {
		changed := def
		pf.mutate(&changed)
		got := overriddenKeys(changed, def)
		if len(got) != 1 || got[0] != pf.key {
			t.Errorf("changing %s: overriddenKeys = %v, want [%s]", pf.key, got, pf.key)
		}
	}
}

func TestAbbrevHomeReplacesTheHomePrefixWithATilde(t *testing.T) {
	old := homeDirFn
	defer func() { homeDirFn = old }()
	homeDirFn = func() (string, error) { return "/Users/x", nil }

	cases := map[string]string{
		"/Users/x/Library/vitals/config.toml": "~/Library/vitals/config.toml",
		"/Users/x":                            "~",
		"/etc/vitals/config.toml":             "/etc/vitals/config.toml", // not under home — untouched
		"/Users/xanadu/thing":                 "/Users/xanadu/thing",     // prefix-but-not-a-dir-boundary — untouched
	}
	for in, want := range cases {
		if got := abbrevHome(in); got != want {
			t.Errorf("abbrevHome(%q) = %q, want %q", in, got, want)
		}
	}

	homeDirFn = func() (string, error) { return "", os.ErrNotExist }
	if got := abbrevHome("/Users/x/f"); got != "/Users/x/f" {
		t.Errorf("with no home dir resolvable, abbrevHome should pass the path through, got %q", got)
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
