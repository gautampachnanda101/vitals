package heal

import (
	"errors"
	"strings"
	"testing"

	"vitals/internal/diag"
	"vitals/internal/doctor"
)

// fakeRunner builds a runner with everything stubbed and a captured
// stdout. Callers override the fields they care about.
type recorder struct {
	ranArgv  [][]string
	prompts  []string
	answers  []bool // consumed in order by confirm
	promptIx int
	out      strings.Builder
}

func newRunner(t *testing.T, report diag.Report, rec *recorder) runner {
	t.Helper()
	return runner{
		assess: func(doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, report
		},
		exec: func(argv []string) error {
			rec.ranArgv = append(rec.ranArgv, argv)
			return nil
		},
		confirm: func(p string) bool {
			rec.prompts = append(rec.prompts, p)
			if rec.promptIx < len(rec.answers) {
				a := rec.answers[rec.promptIx]
				rec.promptIx++
				return a
			}
			return false
		},
		isTTY: func() bool { return true },
		goos:  "darwin",
		out:   &rec.out,
	}
}

func reportWith(findings ...diag.Finding) diag.Report {
	var r diag.Report
	for _, f := range findings {
		r.Add(f)
	}
	return r
}

func purgeFinding() diag.Finding {
	return diag.Finding{
		Severity: diag.Warn, ID: "mac-reclaimable", Title: "RAM high",
		Fixes: []string{"sudo purge"},
		Remedy: &diag.Remedy{
			Kind: diag.RemedyExec, Label: "sudo purge", Argv: []string{"sudo", "purge"},
			Risk: diag.RiskLow, Reversible: true,
		},
	}
}

func cleanFinding() diag.Finding {
	return diag.Finding{
		Severity: diag.Critical, ID: "disk-low", Title: "Disk / nearly full",
		Fixes: []string{"clean"},
		Remedy: &diag.Remedy{
			Kind: diag.RemedyDelegate, Label: "vitals clean", Argv: []string{"vitals", "clean"},
			Risk: diag.RiskMedium, Reversible: false,
		},
	}
}

func TestRunRefusesWithoutATTY(t *testing.T) {
	rec := &recorder{}
	r := newRunner(t, reportWith(purgeFinding()), rec)
	r.isTTY = func() bool { return false }
	if code := run(r, Options{}); code != 2 {
		t.Errorf("no-TTY run should exit 2, got %d", code)
	}
	if len(rec.ranArgv) != 0 {
		t.Error("no-TTY run must not execute anything")
	}
	if !strings.Contains(rec.out.String(), "interactive terminal") {
		t.Errorf("should explain the TTY requirement: %s", rec.out.String())
	}
}

func TestDryRunTouchesNothingEvenWithoutATTY(t *testing.T) {
	rec := &recorder{}
	r := newRunner(t, reportWith(purgeFinding(), cleanFinding()), rec)
	r.isTTY = func() bool { return false }
	if code := run(r, Options{DryRun: true}); code != 0 {
		t.Errorf("dry-run exit = %d, want 0", code)
	}
	if len(rec.ranArgv) != 0 || len(rec.prompts) != 0 {
		t.Errorf("dry-run executed or prompted: argv=%v prompts=%v", rec.ranArgv, rec.prompts)
	}
	out := rec.out.String()
	for _, want := range []string{"sudo purge", "vitals clean", "not executed", "disk-low", "mac-reclaimable"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestConfirmYesRunsAnExecRemedy(t *testing.T) {
	rec := &recorder{answers: []bool{true}}
	r := newRunner(t, reportWith(purgeFinding()), rec)
	if code := run(r, Options{}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.ranArgv) != 1 || strings.Join(rec.ranArgv[0], " ") != "sudo purge" {
		t.Errorf("expected `sudo purge` to run, got %v", rec.ranArgv)
	}
}

func TestConfirmNoSkips(t *testing.T) {
	rec := &recorder{answers: []bool{false}}
	r := newRunner(t, reportWith(purgeFinding()), rec)
	run(r, Options{})
	if len(rec.ranArgv) != 0 {
		t.Errorf("a declined remedy must not run: %v", rec.ranArgv)
	}
	if !strings.Contains(rec.out.String(), "skipped") {
		t.Errorf("output should say skipped: %s", rec.out.String())
	}
}

func TestYesPreAnswersButStillNeedsTTY(t *testing.T) {
	rec := &recorder{}
	r := newRunner(t, reportWith(purgeFinding()), rec)
	if code := run(r, Options{Yes: true}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.prompts) != 0 {
		t.Errorf("--yes should skip the prompt, got %v", rec.prompts)
	}
	if len(rec.ranArgv) != 1 {
		t.Errorf("--yes should run the remedy, got %v", rec.ranArgv)
	}
	// but --yes without a TTY is still refused
	rec2 := &recorder{}
	r2 := newRunner(t, reportWith(purgeFinding()), rec2)
	r2.isTTY = func() bool { return false }
	if code := run(r2, Options{Yes: true}); code != 2 {
		t.Errorf("--yes without a TTY must still refuse, got %d", code)
	}
}

func TestDelegateRunsPreviewThenRealOnDoubleConfirm(t *testing.T) {
	rec := &recorder{answers: []bool{true /*apply?*/, true /*proceed after preview?*/}}
	r := newRunner(t, reportWith(cleanFinding()), rec)
	if code := run(r, Options{}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.ranArgv) != 2 {
		t.Fatalf("delegate should run preview then real, got %v", rec.ranArgv)
	}
	if !strings.Contains(strings.Join(rec.ranArgv[0], " "), "--dry-run") {
		t.Errorf("first run should be the --dry-run preview: %v", rec.ranArgv[0])
	}
	if strings.Contains(strings.Join(rec.ranArgv[1], " "), "--dry-run") {
		t.Errorf("second run should be the real cleanup: %v", rec.ranArgv[1])
	}
	// the leading "vitals" is resolved to this binary's own path
	if rec.ranArgv[1][0] == "vitals" {
		t.Errorf("delegate should resolve `vitals` to os.Executable(), got %v", rec.ranArgv[1])
	}
}

func TestDelegateAbortsAfterPreviewIfDeclined(t *testing.T) {
	rec := &recorder{answers: []bool{true /*apply?*/, false /*proceed?*/}}
	r := newRunner(t, reportWith(cleanFinding()), rec)
	code := run(r, Options{})
	if len(rec.ranArgv) != 1 {
		t.Errorf("declining after preview should leave only the preview run, got %v", rec.ranArgv)
	}
	if code != 1 {
		t.Errorf("an aborted remedy is a failure exit, got %d", code)
	}
}

func TestManualFindingPrintsFixesAndRunsNothing(t *testing.T) {
	rec := &recorder{}
	manual := diag.Finding{Severity: diag.Warn, ID: "thermal", Title: "Throttling",
		Fixes: []string{"improve airflow", "clear dust"}}
	r := newRunner(t, reportWith(manual), rec)
	run(r, Options{})
	if len(rec.ranArgv) != 0 || len(rec.prompts) != 0 {
		t.Error("a manual finding must not run or prompt")
	}
	out := rec.out.String()
	if !strings.Contains(out, "improve airflow") || !strings.Contains(out, "no automatable remedy") {
		t.Errorf("manual finding should print its fixes: %s", out)
	}
}

func TestSignalRemedyIsRejectedInV1(t *testing.T) {
	rec := &recorder{answers: []bool{true}}
	sig := diag.Finding{Severity: diag.Critical, ID: "swap-thrash", Title: "Swap thrashing",
		Fixes: []string{"quit the hog"},
		Remedy: &diag.Remedy{Kind: diag.RemedySignal, Label: "SIGTERM pid 42",
			Signal: "SIGTERM", PID: 42, Risk: diag.RiskHigh}}
	r := newRunner(t, reportWith(sig), rec)
	run(r, Options{})
	if len(rec.ranArgv) != 0 {
		t.Error("RemedySignal is disabled in v1 — must never execute")
	}
}

func TestPurgeRemedySkippedOnNonDarwin(t *testing.T) {
	rec := &recorder{answers: []bool{true}}
	r := newRunner(t, reportWith(purgeFinding()), rec)
	r.goos = "linux"
	run(r, Options{})
	if len(rec.ranArgv) != 0 {
		t.Errorf("`sudo purge` is macOS-only — must be skipped on linux, got %v", rec.ranArgv)
	}
}

func TestOnlySelectsOneFindingAndUnknownIdIsCleanExit(t *testing.T) {
	rec := &recorder{answers: []bool{true}}
	r := newRunner(t, reportWith(purgeFinding(), cleanFinding()), rec)
	run(r, Options{Only: "mac-reclaimable"})
	if len(rec.ranArgv) != 1 || strings.Join(rec.ranArgv[0], " ") != "sudo purge" {
		t.Errorf("--only mac-reclaimable should run just that remedy, got %v", rec.ranArgv)
	}

	rec2 := &recorder{}
	r2 := newRunner(t, reportWith(purgeFinding()), rec2)
	if code := run(r2, Options{Only: "disk-low"}); code != 0 {
		t.Errorf("--only for a finding that isn't present now should exit 0, got %d", code)
	}
	if !strings.Contains(rec2.out.String(), "nothing to do") {
		t.Errorf("should say nothing to do: %s", rec2.out.String())
	}
}

func TestExecFailurePropagatesExit1(t *testing.T) {
	rec := &recorder{answers: []bool{true}}
	r := newRunner(t, reportWith(purgeFinding()), rec)
	r.exec = func([]string) error { return errors.New("sudo: a password is required") }
	if code := run(r, Options{}); code != 1 {
		t.Errorf("a failed remedy should exit 1, got %d", code)
	}
	if !strings.Contains(rec.out.String(), "failed") {
		t.Errorf("should report the failure: %s", rec.out.String())
	}
}

func TestExecAllowlist(t *testing.T) {
	ok := [][]string{{"vitals", "clean"}, {"vitals", "clean", "--dry-run"}, {"sudo", "purge"}}
	bad := [][]string{{}, {"rm", "-rf", "/"}, {"sudo", "rm"}, {"sudo"}, {"bash", "-c", "x"}, {"vitals-evil"}}
	for _, a := range ok {
		if !execAllowed(a) {
			t.Errorf("execAllowed(%v) = false, want true", a)
		}
	}
	for _, a := range bad {
		if execAllowed(a) {
			t.Errorf("execAllowed(%v) = true, want false", a)
		}
	}
}

func TestNoActionableFindingsSaysSo(t *testing.T) {
	rec := &recorder{}
	r := newRunner(t, reportWith(diag.Finding{Severity: diag.OK, Title: "healthy"}), rec)
	if code := run(r, Options{}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(rec.out.String(), "no findings have an automatable remedy") {
		t.Errorf("should say there's nothing to heal: %s", rec.out.String())
	}
}

func TestDefaultRunnerWiringIsPopulated(t *testing.T) {
	// out is intentionally nil in defaultRunner — run() resolves it to
	// the live os.Stdout at call time.
	if defaultRunner.assess == nil || defaultRunner.exec == nil ||
		defaultRunner.confirm == nil || defaultRunner.isTTY == nil {
		t.Error("defaultRunner has a nil field")
	}
	// exercise realExec's empty-argv guard and a bogus binary
	if realExec(nil) == nil {
		t.Error("realExec(nil) should error")
	}
	if realExec([]string{"vitals-no-such-binary-zzz"}) == nil {
		t.Error("realExec of a missing binary should error")
	}
}

func TestReadYes(t *testing.T) {
	yes := []string{"y\n", "Y\n", "yes\n", "  YES  \n"}
	no := []string{"n\n", "\n", "nope\n", "yeah\n", ""}
	for _, s := range yes {
		if !readYes(strings.NewReader(s)) {
			t.Errorf("readYes(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if readYes(strings.NewReader(s)) {
			t.Errorf("readYes(%q) = true, want false", s)
		}
	}
}

func TestApplyRemedyPreviewFailureAborts(t *testing.T) {
	rec := &recorder{answers: []bool{true}}
	r := newRunner(t, reportWith(cleanFinding()), rec)
	r.exec = func(argv []string) error {
		rec.ranArgv = append(rec.ranArgv, argv)
		return errors.New("clean preview blew up")
	}
	if code := run(r, Options{}); code != 1 {
		t.Errorf("a failed delegate preview should exit 1, got %d", code)
	}
	if !strings.Contains(rec.out.String(), "preview failed") {
		t.Errorf("should name the preview failure: %s", rec.out.String())
	}
}

func TestRunEntryPointDryRunAgainstRealAssess(t *testing.T) {
	// Exercises Run -> run(defaultRunner, ...) once. --dry-run so it
	// executes nothing; QuickAssess is bounded (~700ms, no probes).
	if code := Run(Options{DryRun: true}); code != 0 {
		t.Errorf("Run(dry-run) exit = %d, want 0", code)
	}
}
