package dashboard

import (
	"strings"
	"testing"

	"vitals/internal/tools"
)

func TestSystemModuleIsRegistered(t *testing.T) {
	m, exists, available := findModule("system", PageContext{})
	if !exists {
		t.Fatal("system module should be registered")
	}
	if !available {
		t.Error("system module should always be available")
	}
	if m.NavLabel != "System" || m.Group != "System" {
		t.Errorf("NavLabel/Group = %q/%q, want \"System\"/\"System\"", m.NavLabel, m.Group)
	}
}

func TestRenderSystemShowsMachineAndConfigAndTools(t *testing.T) {
	// Exercises the real info.Collect/tools.Registry wiring against this
	// machine — read-only (info.Collect stats a config path and reads
	// gopsutil host info; tools.Installed only calls exec.LookPath per
	// registry entry), matching resourcePage's own direct-per-request
	// call pattern rather than needing a cache.
	out := renderSystem(PageContext{Version: "1.2.3"})
	for _, want := range []string{"1.2.3", "Machine", "Config", "Companion tools", "disk_warn_percent", "of 8 installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSystem missing %q, got: %s", want, out)
		}
	}
	// Every registered tool's name should appear somewhere on the page.
	for _, tl := range tools.Registry {
		if !strings.Contains(out, tl.Name) {
			t.Errorf("renderSystem missing tool %q, got: %s", tl.Name, out)
		}
	}
}

func TestConfigSectionTitleTextThreeStates(t *testing.T) {
	cases := []struct {
		exists bool
		n      int
		want   string
	}{
		{false, 0, "not created yet"},
		{true, 0, "every value still at its default"},
		{true, 1, "1 value changed"},
		{true, 3, "3 values changed"},
	}
	for _, c := range cases {
		got := configSectionTitleText(c.exists, c.n)
		if !strings.Contains(got, c.want) {
			t.Errorf("configSectionTitleText(%v, %d) = %q, want it to contain %q", c.exists, c.n, got, c.want)
		}
	}
}

func TestConfigRowMarksOverriddenKeys(t *testing.T) {
	overridden := configRow("disk_warn_percent", "80", []string{"disk_warn_percent"})
	if !strings.Contains(overridden, "set in file") {
		t.Errorf("configRow should mark an overridden key as set in file, got: %s", overridden)
	}
	deflt := configRow("ram_warn_percent", "85", []string{"disk_warn_percent"})
	if !strings.Contains(deflt, "built-in default") {
		t.Errorf("configRow should mark a non-overridden key as built-in default, got: %s", deflt)
	}
}

func TestToolCardShowsInstalledStatus(t *testing.T) {
	tl := tools.Tool{Name: "gdu", Category: "disk explorer", Description: "fast disk usage"}
	installed := toolCard(tl, true)
	if !strings.Contains(installed, "installed") || strings.Contains(installed, "not installed") {
		t.Errorf("toolCard(installed) = %s", installed)
	}
	notInstalled := toolCard(tl, false)
	if !strings.Contains(notInstalled, "not installed") {
		t.Errorf("toolCard(not installed) = %s", notInstalled)
	}
}

func TestToolCardEscapesUntrustedFields(t *testing.T) {
	tl := tools.Tool{Name: "<script>alert(1)</script>", Category: "x", Description: "<img src=x>"}
	out := toolCard(tl, false)
	for _, bad := range []string{"<script>alert(1)</script>", "<img src=x>"} {
		if strings.Contains(out, bad) {
			t.Errorf("toolCard did not escape %q, got: %s", bad, out)
		}
	}
}
