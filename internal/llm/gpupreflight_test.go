package llm

import (
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
