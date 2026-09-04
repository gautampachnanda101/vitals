package tools

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout swaps os.Stdout for the duration of f and returns everything
// written to it. Drained concurrently, not after f returns, to avoid a
// pipe-buffer deadlock on Windows — same pattern as internal/monitor's,
// internal/dupes', and internal/memhogs' identical helpers.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

// fakeLookPath returns a deps.lookPath that reports found for exactly the
// names in found.
func fakeLookPath(found ...string) func(string) (string, error) {
	set := make(map[string]bool, len(found))
	for _, f := range found {
		set[f] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New(name + ": executable file not found in $PATH")
	}
}

// recordingRunCmd is a deps.runCmd that records every call instead of
// actually running a subprocess, so Install/Launch's call-site logic
// (argument construction, which candidate got chosen) is provable without
// ever shelling out in a test.
type recordingRunCmd struct {
	calls []struct {
		name string
		args []string
	}
	err error
}

func (r *recordingRunCmd) run(name string, args []string, _ io.Reader, _, _ io.Writer) error {
	r.calls = append(r.calls, struct {
		name string
		args []string
	}{name, args})
	return r.err
}

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

func TestInstalledUsesLookPath(t *testing.T) {
	d := deps{lookPath: fakeLookPath("ncdu")}
	if !installed(d, Tool{Name: "ncdu"}) {
		t.Error("installed should be true when lookPath finds the binary")
	}
	if installed(d, Tool{Name: "gdu"}) {
		t.Error("installed should be false when lookPath doesn't find the binary")
	}
}

func TestDetectManagerPerOS(t *testing.T) {
	cases := []struct {
		goos  string
		found string
		want  Manager
	}{
		{"darwin", "brew", Brew},
		{"linux", "apt-get", Apt},
		{"linux", "dnf", Dnf},
		{"linux", "pacman", Pacman},
		{"windows", "winget", Winget},
		{"windows", "scoop", Scoop},
	}
	for _, c := range cases {
		d := deps{lookPath: fakeLookPath(c.found)}
		got, ok := detectManager(d, c.goos)
		if !ok || got != c.want {
			t.Errorf("detectManager(%s, only %s on PATH) = %v, %v; want %v, true", c.goos, c.found, got, ok, c.want)
		}
	}
	// Nothing on PATH at all.
	d := deps{lookPath: fakeLookPath()}
	if _, ok := detectManager(d, "linux"); ok {
		t.Error("detectManager should report false when no candidate manager is on PATH")
	}
	// An OS with no candidates at all (e.g. a BSD) is the same as none found.
	if _, ok := detectManager(deps{lookPath: fakeLookPath("brew")}, "freebsd"); ok {
		t.Error("detectManager should report false for an OS with no known candidate managers")
	}
}

func TestListPrintsHeaderAndEveryToolsInstallStatus(t *testing.T) {
	d := deps{lookPath: fakeLookPath("ncdu")}
	out := captureStdout(t, func() { list(d) })
	if !strings.Contains(out, "COMPANION TOOLS") {
		t.Errorf("list() missing the header, got:\n%s", out)
	}
	if !strings.Contains(out, "ncdu") {
		t.Errorf("list() missing a registry entry, got:\n%s", out)
	}
}

func TestInstallUnknownTool(t *testing.T) {
	d := deps{lookPath: fakeLookPath()}
	if err := install(d, "not-a-real-tool", true); err == nil {
		t.Error("install should error for an unregistered tool name")
	}
}

func TestInstallSkipsWhenAlreadyInstalled(t *testing.T) {
	rc := &recordingRunCmd{}
	d := deps{lookPath: fakeLookPath("ncdu"), runCmd: rc.run}
	out := captureStdout(t, func() {
		if err := install(d, "ncdu", true); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if len(rc.calls) != 0 {
		t.Errorf("install should not run anything when already installed, got calls %v", rc.calls)
	}
	if !strings.Contains(out, "already installed") {
		t.Errorf("install should say the tool is already installed, got:\n%s", out)
	}
}

func TestInstallErrorsWithNoPackageManagerDetected(t *testing.T) {
	d := deps{lookPath: fakeLookPath()} // nothing on PATH: not ncdu, not any manager
	if err := install(d, "ncdu", true); err == nil || !strings.Contains(err.Error(), "no supported package manager") {
		t.Errorf("install() = %v, want a no-package-manager error", err)
	}
}

func TestInstallErrorsWithNoKnownPackageForTheDetectedManager(t *testing.T) {
	// nvtop has no Winget/Scoop entry in the Registry.
	d := deps{lookPath: fakeLookPath("winget"), goos: "windows"}
	if err := install(d, "nvtop", true); err == nil || !strings.Contains(err.Error(), "no known") {
		t.Errorf("install() = %v, want a no-known-package error", err)
	}
}

func TestInstallRunsTheDetectedManagerWhenYes(t *testing.T) {
	rc := &recordingRunCmd{}
	d := deps{lookPath: fakeLookPath("brew"), runCmd: rc.run, goos: "darwin"}
	captureStdout(t, func() {
		if err := install(d, "ncdu", true); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if len(rc.calls) != 1 || rc.calls[0].name != "brew" || strings.Join(rc.calls[0].args, " ") != "install ncdu" {
		t.Errorf("install should have run `brew install ncdu`, got calls %v", rc.calls)
	}
}

func TestInstallPropagatesTheRunCmdError(t *testing.T) {
	rc := &recordingRunCmd{err: errors.New("exit status 1")}
	d := deps{lookPath: fakeLookPath("brew"), runCmd: rc.run, goos: "darwin"}
	captureStdout(t, func() {
		if err := install(d, "ncdu", true); err == nil {
			t.Error("install should propagate the runCmd error")
		}
	})
}

func TestInstallConfirmsWhenNotYes(t *testing.T) {
	rc := &recordingRunCmd{}

	// "y" confirms and runs.
	d := deps{lookPath: fakeLookPath("brew"), runCmd: rc.run, confirmReader: strings.NewReader("y\n"), goos: "darwin"}
	captureStdout(t, func() {
		if err := install(d, "ncdu", false); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if len(rc.calls) != 1 {
		t.Fatalf("a 'y' answer should have run the install, got %d calls", len(rc.calls))
	}

	// "n" aborts without running anything.
	d.confirmReader = strings.NewReader("n\n")
	out := captureStdout(t, func() {
		if err := install(d, "ncdu", false); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if len(rc.calls) != 1 { // still just the one call from above
		t.Errorf("a 'n' answer should not have run anything, got %d calls", len(rc.calls))
	}
	if !strings.Contains(out, "aborted") {
		t.Errorf("install should print an aborted notice, got:\n%s", out)
	}

	// EOF (no input at all) is the same as "no".
	d.confirmReader = strings.NewReader("")
	captureStdout(t, func() { _ = install(d, "ncdu", false) })
	if len(rc.calls) != 1 {
		t.Errorf("EOF on the confirm reader should not have run anything, got %d calls", len(rc.calls))
	}
}

func TestLaunchRunsTheFirstInstalledCandidate(t *testing.T) {
	rc := &recordingRunCmd{}
	// gdu (preferred) is not installed; ncdu is — Launch should skip to it.
	d := deps{lookPath: fakeLookPath("ncdu"), runCmd: rc.run}
	if err := launch(d, "disk explorer", []string{"-x"}); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(rc.calls) != 1 || rc.calls[0].name != "ncdu" || strings.Join(rc.calls[0].args, " ") != "-x" {
		t.Errorf("launch should have run `ncdu -x`, got calls %v", rc.calls)
	}
}

func TestLaunchNoneInstalledListsCandidateNames(t *testing.T) {
	d := deps{lookPath: fakeLookPath()}
	err := launch(d, "disk explorer", nil)
	if err == nil || !strings.Contains(err.Error(), "gdu") || !strings.Contains(err.Error(), "ncdu") {
		t.Errorf("launch() = %v, want an error naming every disk-explorer candidate", err)
	}
}

func TestLaunchPropagatesTheRunCmdError(t *testing.T) {
	rc := &recordingRunCmd{err: errors.New("exit status 1")}
	d := deps{lookPath: fakeLookPath("ncdu"), runCmd: rc.run}
	if err := launch(d, "disk explorer", nil); err == nil {
		t.Error("launch should propagate the runCmd error")
	}
}

func TestConfirmParsesYesNoAndEOF(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "Y\n": true, "n\n": false, "\n": false, "": false, "garbage\n": false}
	for in, want := range cases {
		out := captureStdout(t, func() {
			if got := confirm(strings.NewReader(in)); got != want {
				t.Errorf("confirm(%q) = %v, want %v", in, got, want)
			}
		})
		if !strings.Contains(out, "Continue?") {
			t.Errorf("confirm should print the prompt, got:\n%s", out)
		}
	}
}

func TestRunDispatchesToInstallOrList(t *testing.T) {
	rc := &recordingRunCmd{}
	d := deps{lookPath: fakeLookPath("brew"), runCmd: rc.run, goos: "darwin"}

	out := captureStdout(t, func() {
		if err := run(d, Options{}); err != nil {
			t.Fatalf("run(list): %v", err)
		}
	})
	if !strings.Contains(out, "COMPANION TOOLS") {
		t.Errorf("run with no Install option should list, got:\n%s", out)
	}

	captureStdout(t, func() {
		if err := run(d, Options{Install: "ncdu", Yes: true}); err != nil {
			t.Fatalf("run(install): %v", err)
		}
	})
	if len(rc.calls) != 1 {
		t.Errorf("run with Install set should have installed, got %d calls", len(rc.calls))
	}
}

func TestDefaultRunCmdActuallyRunsAProcess(t *testing.T) {
	// install()/launch() are fully exercised above via a fake runCmd (so no
	// test ever shells out through a real package manager or TUI) — this is
	// the one place defaultDeps.runCmd's own body (the real exec.Command
	// wiring) gets exercised for real, using `go version` as a stand-in
	// real command: harmless, deterministic, and — since these are Go
	// tests — guaranteed to be on PATH on every OS this repo tests on.
	if err := defaultDeps.runCmd("go", []string{"version"}, nil, io.Discard, io.Discard); err != nil {
		t.Errorf("defaultDeps.runCmd(go version) = %v, want nil", err)
	}
	if err := defaultDeps.runCmd("a-binary-that-does-not-exist", nil, nil, io.Discard, io.Discard); err == nil {
		t.Error("defaultDeps.runCmd should return an error for a nonexistent binary")
	}
}

func TestPublicWrappersGoThroughTheRealPATH(t *testing.T) {
	// One real, live call through defaultDeps for each public entry point —
	// the memcheck/monitor-style single end-to-end exercise of the actual
	// wiring, not just the injected-fake paths above.
	_ = Installed(Tool{Name: "a-tool-name-nothing-installs"})
	out := captureStdout(t, List)
	if !strings.Contains(out, "COMPANION TOOLS") {
		t.Errorf("List() produced no output:\n%s", out)
	}
	if err := Install("not-a-real-tool", true); err == nil {
		t.Error("Install should error for an unregistered tool name")
	}
	if err := Launch("a category nothing registers", nil); err == nil {
		t.Error("Launch should error when no tool in the category is installed")
	}
	out = captureStdout(t, func() {
		if err := Run(Options{}); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if !strings.Contains(out, "COMPANION TOOLS") {
		t.Errorf("Run() produced no output:\n%s", out)
	}
}
