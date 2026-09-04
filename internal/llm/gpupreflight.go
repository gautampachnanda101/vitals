package llm

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// GPUDriverStatus is the result of checking whether a GPU management CLI is
// present and actually runs — the first step in every "why isn't Ollama
// using my GPU" guide, checked before blaming the runtime itself.
type GPUDriverStatus struct {
	Checked   bool   `json:"checked"`
	Name      string `json:"name,omitempty"` // "nvidia-smi", "rocm-smi", ""
	Reachable bool   `json:"reachable"`
	Err       string `json:"error,omitempty"`
}

// needsGPUPreflightCheck reports whether any currently-loaded model looks
// CPU-bound enough that a GPU driver check is worth the extra subprocess
// call — no point shelling out on every run when offload already looks fine.
func needsGPUPreflightCheck(models []ModelState) bool {
	for _, m := range models {
		if m.Resident && m.GPUOffload < 5 {
			return true
		}
	}
	return false
}

// gpuPreflightDeps is the live exec/PATH surface checkGPUDriver reads
// from, pulled out so a test can substitute fakes — same shape as
// internal/tools'/internal/gpu's deps structs. defaultGPUPreflightDeps
// wires the real calls; production always goes through it via
// checkGPUDriver.
type gpuPreflightDeps struct {
	goos     string
	lookPath func(file string) (string, error)
	runCmd   func(ctx context.Context, name string, args ...string) error
}

var defaultGPUPreflightDeps = gpuPreflightDeps{
	goos:     runtime.GOOS,
	lookPath: exec.LookPath,
	runCmd: func(ctx context.Context, name string, args ...string) error {
		return exec.CommandContext(ctx, name, args...).Run()
	},
}

// checkGPUDriver tries the GPU management CLI relevant to this OS and
// reports whether it ran successfully. Apple Silicon has no separate driver
// layer to check — Metal is always available — so darwin is reported
// unchecked rather than flagged as a problem.
func checkGPUDriver() GPUDriverStatus { return checkGPUDriverWith(defaultGPUPreflightDeps) }

func checkGPUDriverWith(d gpuPreflightDeps) GPUDriverStatus {
	var name string
	switch d.goos {
	case "linux", "windows":
		name = "nvidia-smi" // checked first; AMD hosts fall through to rocm-smi below
	default:
		return GPUDriverStatus{}
	}

	if ok, err := runsCleanly(d, name, "-L"); ok {
		return GPUDriverStatus{Checked: true, Name: name, Reachable: true}
	} else if err != nil {
		if _, lookErr := d.lookPath(name); lookErr == nil {
			// present but failed — likely a driver/kernel-module mismatch
			return GPUDriverStatus{Checked: true, Name: name, Reachable: false, Err: err.Error()}
		}
	}

	name = "rocm-smi"
	if ok, err := runsCleanly(d, name, "--showid"); ok {
		return GPUDriverStatus{Checked: true, Name: name, Reachable: true}
	} else if _, lookErr := d.lookPath(name); lookErr == nil {
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		return GPUDriverStatus{Checked: true, Name: name, Reachable: false, Err: errStr}
	}

	return GPUDriverStatus{Checked: true, Name: "", Reachable: false}
}

func runsCleanly(d gpuPreflightDeps, name string, args ...string) (ok bool, err error) {
	if _, lookErr := d.lookPath(name); lookErr != nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = d.runCmd(ctx, name, args...)
	return err == nil, err
}

// gpuPreflightMessage renders status as a one-line diagnostic, or "" if
// status was never checked (nothing to say — no model looked CPU-bound, or
// this OS has no separate driver layer to check).
func gpuPreflightMessage(status GPUDriverStatus) string {
	if !status.Checked {
		return ""
	}
	switch {
	case status.Name == "":
		return "no GPU management CLI found (nvidia-smi/rocm-smi) — either there's no supported discrete GPU, or its driver isn't installed"
	case !status.Reachable:
		return status.Name + " is installed but failed to run (" + status.Err + ") — a driver/kernel-module mismatch is the likely cause, not the LLM runtime"
	default:
		return status.Name + " responded fine — the GPU itself is reachable, so an unoffloaded model is a runtime/VRAM-sizing issue, not a missing driver"
	}
}
