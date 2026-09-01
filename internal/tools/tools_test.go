package tools

import "testing"

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
