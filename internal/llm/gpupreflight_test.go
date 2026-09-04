package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNeedsGPUPreflightCheckWhenAResidentModelIsCPUBound(t *testing.T) {
	models := []ModelState{{Resident: true, GPUOffload: 0}}
	if !needsGPUPreflightCheck(models) {
		t.Error("a resident model at 0 percent offload should trigger a preflight check")
	}
}

func TestNeedsGPUPreflightCheckSkipsWellOffloadedModels(t *testing.T) {
	models := []ModelState{{Resident: true, GPUOffload: 97}}
	if needsGPUPreflightCheck(models) {
		t.Error("a well-offloaded model should not trigger a preflight check")
	}
}

func TestNeedsGPUPreflightCheckIgnoresNonResidentModels(t *testing.T) {
	models := []ModelState{{Resident: false, GPUOffload: 0}}
	if needsGPUPreflightCheck(models) {
		t.Error("a model that isn't currently loaded shouldn't trigger a driver check")
	}
}

func TestNeedsGPUPreflightCheckWithNoModelsIsFalse(t *testing.T) {
	if needsGPUPreflightCheck(nil) {
		t.Error("no models at all should never trigger a preflight check")
	}
}

func TestGPUPreflightMessageNoDriverFound(t *testing.T) {
	msg := gpuPreflightMessage(GPUDriverStatus{Checked: true, Reachable: false})
	if msg == "" {
		t.Fatal("expected a message when no GPU driver CLI was found")
	}
	if !strings.Contains(msg, "no GPU") && !strings.Contains(msg, "No GPU") {
		t.Errorf("message should say no GPU driver was found: %q", msg)
	}
}

func TestGPUPreflightMessageDriverFailed(t *testing.T) {
	msg := gpuPreflightMessage(GPUDriverStatus{Checked: true, Name: "nvidia-smi", Reachable: false, Err: "exit status 1"})
	if !strings.Contains(msg, "nvidia-smi") {
		t.Errorf("message should name the failing driver CLI: %q", msg)
	}
}

func TestGPUPreflightMessageDriverFineButModelNotOffloaded(t *testing.T) {
	msg := gpuPreflightMessage(GPUDriverStatus{Checked: true, Name: "nvidia-smi", Reachable: true})
	if !strings.Contains(msg, "nvidia-smi") {
		t.Errorf("message should confirm the driver that responded: %q", msg)
	}
}

func TestGPUPreflightMessageNotCheckedIsEmpty(t *testing.T) {
	if msg := gpuPreflightMessage(GPUDriverStatus{Checked: false}); msg != "" {
		t.Errorf("an unchecked status should produce no message, got %q", msg)
	}
}

func fakeLookPath(found ...string) func(string) (string, error) {
	set := make(map[string]bool, len(found))
	for _, f := range found {
		set[f] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New(name + ": not found")
	}
}

func TestCheckGPUDriverWithDarwinIsUnchecked(t *testing.T) {
	// No separate driver layer on Apple Silicon (Metal is always
	// available) — checkGPUDriver must not shell out at all.
	got := checkGPUDriverWith(gpuPreflightDeps{goos: "darwin"})
	if got.Checked {
		t.Errorf("darwin should report unchecked, got %+v", got)
	}
}

func TestCheckGPUDriverWithNvidiaReachable(t *testing.T) {
	d := gpuPreflightDeps{
		goos:     "linux",
		lookPath: fakeLookPath("nvidia-smi"),
		runCmd:   func(context.Context, string, ...string) error { return nil },
	}
	got := checkGPUDriverWith(d)
	if !got.Checked || got.Name != "nvidia-smi" || !got.Reachable {
		t.Errorf("checkGPUDriverWith(nvidia-smi reachable) = %+v", got)
	}
}

func TestCheckGPUDriverWithNvidiaPresentButFails(t *testing.T) {
	d := gpuPreflightDeps{
		goos:     "linux",
		lookPath: fakeLookPath("nvidia-smi"),
		runCmd:   func(context.Context, string, ...string) error { return errors.New("driver mismatch") },
	}
	got := checkGPUDriverWith(d)
	if !got.Checked || got.Name != "nvidia-smi" || got.Reachable || got.Err == "" {
		t.Errorf("checkGPUDriverWith(nvidia-smi present but failing) = %+v", got)
	}
}

func TestCheckGPUDriverWithFallsBackToRocm(t *testing.T) {
	d := gpuPreflightDeps{
		goos:     "windows",
		lookPath: fakeLookPath("rocm-smi"), // nvidia-smi absent, rocm-smi present
		runCmd:   func(context.Context, string, ...string) error { return nil },
	}
	got := checkGPUDriverWith(d)
	if !got.Checked || got.Name != "rocm-smi" || !got.Reachable {
		t.Errorf("checkGPUDriverWith(falls back to rocm-smi) = %+v", got)
	}
}

func TestCheckGPUDriverWithRocmPresentButFails(t *testing.T) {
	d := gpuPreflightDeps{
		goos:     "linux",
		lookPath: fakeLookPath("rocm-smi"),
		runCmd:   func(context.Context, string, ...string) error { return errors.New("boom") },
	}
	got := checkGPUDriverWith(d)
	if !got.Checked || got.Name != "rocm-smi" || got.Reachable || got.Err == "" {
		t.Errorf("checkGPUDriverWith(rocm-smi present but failing) = %+v", got)
	}
}

func TestCheckGPUDriverWithNeitherToolPresent(t *testing.T) {
	d := gpuPreflightDeps{goos: "linux", lookPath: fakeLookPath(), runCmd: func(context.Context, string, ...string) error { return nil }}
	got := checkGPUDriverWith(d)
	if !got.Checked || got.Name != "" || got.Reachable {
		t.Errorf("checkGPUDriverWith(neither tool present) = %+v", got)
	}
}

func TestCheckGPUDriverGoesThroughTheRealDefaultDeps(t *testing.T) {
	// One real end-to-end call through defaultGPUPreflightDeps — on
	// whatever OS runs this test, exercising the actual runtime.GOOS/
	// exec.LookPath/exec.CommandContext wiring, not just the fakes above.
	_ = checkGPUDriver()
}

func TestRunsCleanlyReturnsFalseWithoutErrorWhenTheToolIsMissing(t *testing.T) {
	ok, err := runsCleanly(gpuPreflightDeps{lookPath: fakeLookPath()}, "nvidia-smi", "-L")
	if ok || err != nil {
		t.Errorf("runsCleanly(missing tool) = %v, %v, want false, nil", ok, err)
	}
}
