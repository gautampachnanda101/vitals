package tools

import (
	"strings"
	"testing"
)

func TestFindIsCaseInsensitive(t *testing.T) {
	if _, ok := Find("NCDU"); !ok {
		t.Error("Find should be case-insensitive")
	}
	if _, ok := Find("no-such-tool"); ok {
		t.Error("Find should report false for an unknown tool")
	}
}

func TestByCategoryPreservesRegistryOrder(t *testing.T) {
	explorers := byCategory("disk explorer")
	if len(explorers) < 2 {
		t.Fatalf("expected multiple disk explorers, got %+v", explorers)
	}
	if explorers[0].Name != "gdu" {
		t.Errorf("first disk explorer = %q, want gdu (fastest) preferred first", explorers[0].Name)
	}
	for _, e := range explorers {
		if e.Category != "disk explorer" {
			t.Errorf("byCategory leaked a %q entry into disk explorer results", e.Category)
		}
	}
}

func TestByCategoryIsCaseInsensitiveAndEmptyForUnknown(t *testing.T) {
	if got := byCategory("DISK EXPLORER"); len(got) == 0 {
		t.Error("byCategory should be case-insensitive")
	}
	if got := byCategory("time machine"); len(got) != 0 {
		t.Errorf("byCategory(%q) = %+v, want empty", "time machine", got)
	}
}

func TestInstallCommandPrefixesSudoOnLinuxPackageManagers(t *testing.T) {
	// installCommand itself doesn't check the OS (withSudo does), so this
	// exercises the actual branch used in Install for apt/dnf/pacman.
	name, args := installCommand(Apt, "ncdu")
	if name != "sudo" && name != "apt-get" {
		t.Fatalf("installCommand(Apt, ...) name = %q, want sudo or apt-get", name)
	}
	if name == "sudo" {
		if len(args) < 3 || args[0] != "apt-get" || args[len(args)-1] != "ncdu" {
			t.Errorf("installCommand(Apt, ncdu) args = %v, want apt-get install -y ncdu", args)
		}
	}
}

func TestInstallCommandBrewNeverUsesSudo(t *testing.T) {
	name, args := installCommand(Brew, "ncdu")
	if name != "brew" {
		t.Errorf("installCommand(Brew, ...) name = %q, want brew (never sudo)", name)
	}
	if len(args) != 2 || args[0] != "install" || args[1] != "ncdu" {
		t.Errorf("installCommand(Brew, ncdu) args = %v, want [install ncdu]", args)
	}
}

func TestInstallCommandDnfAndPacman(t *testing.T) {
	if name, args := installCommand(Dnf, "ncdu"); name != "sudo" && name != "dnf" {
		t.Errorf("installCommand(Dnf, ...) name = %q", name)
	} else if name == "dnf" && (len(args) < 3 || args[len(args)-1] != "ncdu") {
		t.Errorf("installCommand(Dnf, ncdu) args = %v", args)
	}
	if name, args := installCommand(Pacman, "ncdu"); name != "sudo" && name != "pacman" {
		t.Errorf("installCommand(Pacman, ...) name = %q", name)
	} else if name == "pacman" && (len(args) < 3 || args[len(args)-1] != "ncdu") {
		t.Errorf("installCommand(Pacman, ncdu) args = %v", args)
	}
}

func TestInstallCommandWingetAndScoopNeverUseSudo(t *testing.T) {
	name, args := installCommand(Winget, "jdupes")
	if name != "winget" || len(args) != 4 || args[len(args)-1] != "jdupes" {
		t.Errorf("installCommand(Winget, jdupes) = %q %v, want winget install -e --id jdupes", name, args)
	}
	name, args = installCommand(Scoop, "gdu")
	if name != "scoop" || len(args) != 2 || args[1] != "gdu" {
		t.Errorf("installCommand(Scoop, gdu) = %q %v, want scoop install gdu", name, args)
	}
}

func TestInstallCommandUnknownManagerReturnsEmpty(t *testing.T) {
	name, args := installCommand(Manager("unknown"), "x")
	if name != "" || args != nil {
		t.Errorf("installCommand(unknown manager) = %q %v, want empty", name, args)
	}
}

func TestWithSudoOnWindowsNeverPrefixesSudo(t *testing.T) {
	name, args := withSudo("windows", 1000, []string{"choco", "install", "x"})
	if name != "choco" || len(args) != 2 {
		t.Errorf("withSudo(windows, non-root) = %q %v, want no sudo prefix regardless of euid", name, args)
	}
}

func TestWithSudoAsRootNeverPrefixesSudo(t *testing.T) {
	name, args := withSudo("linux", 0, []string{"apt-get", "install", "x"})
	if name != "apt-get" || len(args) != 2 {
		t.Errorf("withSudo(linux, euid=0) = %q %v, want no sudo prefix — already root", name, args)
	}
}

func TestWithSudoPrefixesOnNonWindowsNonRoot(t *testing.T) {
	name, args := withSudo("linux", 1000, []string{"apt-get", "install", "x"})
	if name != "sudo" || len(args) != 3 || args[0] != "apt-get" {
		t.Errorf("withSudo(linux, euid=1000) = %q %v, want sudo apt-get install x", name, args)
	}
}

func TestBinaryFallsBackToNameWhenUnset(t *testing.T) {
	if got := (Tool{Name: "ncdu"}).binary(); got != "ncdu" {
		t.Errorf("binary() = %q, want the tool's Name when Binary is unset", got)
	}
}

func TestBinaryUsesExplicitOverrideWhenSet(t *testing.T) {
	if got := (Tool{Name: "smartctl", Binary: "smartctl"}).binary(); got != "smartctl" {
		t.Errorf("binary() = %q, want the explicit Binary field", got)
	}
	// The actual registry case this exists for: display name differs from
	// the on-PATH binary.
	if got := (Tool{Name: "S.M.A.R.T. tools", Binary: "smartctl"}).binary(); got != "smartctl" {
		t.Errorf("binary() = %q, want Binary to override a differing Name", got)
	}
}

func TestFirstOrEmpty(t *testing.T) {
	if got := firstOrEmpty(nil); got != "" {
		t.Errorf("firstOrEmpty(nil) = %q, want empty", got)
	}
	if got := firstOrEmpty([]string{"a", "b"}); got != "a" {
		t.Errorf("firstOrEmpty([a b]) = %q, want a", got)
	}
}

func TestFormatToolListShowsInstalledAndMissingStatus(t *testing.T) {
	tools := []Tool{
		{Name: "gdu", Category: "disk explorer", Description: "fast"},
		{Name: "ncdu", Category: "disk explorer", Description: "classic"},
	}
	installed := func(t Tool) bool { return t.Name == "gdu" }
	out := formatToolList(tools, installed)

	if !strings.Contains(out, "gdu") || !strings.Contains(out, "installed") {
		t.Errorf("formatToolList missing gdu/installed, got: %s", out)
	}
	if !strings.Contains(out, "ncdu") || !strings.Contains(out, "not installed") {
		t.Errorf("formatToolList missing ncdu/not installed, got: %s", out)
	}
}

func TestInstallUnknownToolErrors(t *testing.T) {
	if err := Install("not-a-real-tool", true); err == nil {
		t.Error("Install should error for an unregistered tool name")
	}
}

func TestLaunchWithNoCandidateInstalledErrors(t *testing.T) {
	if err := Launch("a category nothing registers", nil); err == nil {
		t.Error("Launch should error when no tool in the category is installed")
	}
}

func TestRegistryEntriesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range Registry {
		if tool.Name == "" || tool.Category == "" || tool.Description == "" {
			t.Errorf("tool %+v is missing a required field", tool)
		}
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %q in Registry", tool.Name)
		}
		seen[tool.Name] = true
		if len(tool.Packages) == 0 {
			t.Errorf("tool %q has no package manager entries at all", tool.Name)
		}
	}
}
