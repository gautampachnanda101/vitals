package gpu

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const nvidiaSMILine = "0, RTX 4090, 24564, 18320, 42, 61, 180.5, 450.00, 2520, 2520\n"
const nvidiaAppsLine = "1234, chrome, 512\n"
const rocmJSON = `{"card0": {"Card series": "Radeon RX 7900", "GPU use (%)": "10", "Temperature (Sensor edge) (C)": "50", "VRAM Total Memory (B)": "17179869184", "VRAM Total Used Memory (B)": "1073741824", "Average Graphics Package Power (W)": "80"}}`
const iORegOutput = `+-o AGXAcceleratorG13G  <class AGXAcceleratorG13G>
      "model" = "Apple M3 Max"`

// fakeDeps builds a deps whose lookPath/runCmd are driven by the given
// maps, keyed by command name — a test only has to declare which
// commands "exist" and what each one's Output() call returns.
func fakeDeps(goos string, present map[string]bool, output map[string]string, errOn map[string]error) deps {
	return deps{
		goos: goos,
		lookPath: func(file string) (string, error) {
			if present[file] {
				return "/usr/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
		runCmd: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if err, ok := errOn[name]; ok {
				return nil, err
			}
			return []byte(output[name]), nil
		},
	}
}

func TestProbePrefersNvidiaWhenPresent(t *testing.T) {
	d := fakeDeps("linux",
		map[string]bool{"nvidia-smi": true, "rocm-smi": true},
		map[string]string{"nvidia-smi": nvidiaSMILine},
		nil)
	devs := probe(d)
	if len(devs) != 1 || devs[0].Vendor != NVIDIA {
		t.Fatalf("probe() = %+v, want 1 nvidia device", devs)
	}
}

func TestProbeAttachesComputeAppsWhenBothCallsSucceed(t *testing.T) {
	// Both invocations are "nvidia-smi" (device query, then compute-apps
	// query) so runCmd can't key output by command name alone here — it
	// keys by name and returns the same canned devices line for either
	// call, then this test drives attachment directly through a runCmd
	// that inspects the query flag instead.
	d := deps{
		goos:     "linux",
		lookPath: func(string) (string, error) { return "/usr/bin/nvidia-smi", nil },
		runCmd: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "--query-compute-apps=pid,process_name,used_memory" {
					return []byte(nvidiaAppsLine), nil
				}
			}
			return []byte(nvidiaSMILine), nil
		},
	}
	devs := probe(d)
	if len(devs) != 1 {
		t.Fatalf("probe() = %+v, want 1 device", devs)
	}
	if len(devs[0].Processes) != 1 || devs[0].Processes[0].PID != 1234 {
		t.Errorf("Processes = %+v, want the compute-apps process attached", devs[0].Processes)
	}
}

func TestProbeFallsThroughToRocmWhenNvidiaAbsent(t *testing.T) {
	d := fakeDeps("linux",
		map[string]bool{"rocm-smi": true},
		map[string]string{"rocm-smi": rocmJSON},
		nil)
	devs := probe(d)
	if len(devs) != 1 || devs[0].Vendor != AMD {
		t.Fatalf("probe() = %+v, want 1 amd device", devs)
	}
}

func TestProbeFallsThroughToRocmWhenNvidiaParsesEmpty(t *testing.T) {
	// nvidia-smi is present and "runs cleanly" but its output has no
	// parseable device lines — probe should still try rocm-smi, not stop.
	d := fakeDeps("linux",
		map[string]bool{"nvidia-smi": true, "rocm-smi": true},
		map[string]string{"nvidia-smi": "\n  \n", "rocm-smi": rocmJSON},
		nil)
	devs := probe(d)
	if len(devs) != 1 || devs[0].Vendor != AMD {
		t.Fatalf("probe() = %+v, want fallthrough to 1 amd device", devs)
	}
}

func TestProbeFallsThroughToIORegOnDarwinWhenNeitherVendorToolPresent(t *testing.T) {
	d := fakeDeps("darwin",
		map[string]bool{"ioreg": true},
		map[string]string{"ioreg": iORegOutput},
		nil)
	devs := probe(d)
	if len(devs) != 1 || devs[0].Vendor != Apple {
		t.Fatalf("probe() = %+v, want 1 apple device", devs)
	}
}

func TestProbeNeverTriesIORegOffDarwin(t *testing.T) {
	d := fakeDeps("linux",
		map[string]bool{"ioreg": true}, // present, but should never be looked up: not darwin
		map[string]string{"ioreg": iORegOutput},
		nil)
	if devs := probe(d); devs != nil {
		t.Errorf("probe() on linux = %+v, want nil (ioreg is darwin-only)", devs)
	}
}

func TestProbeNoToolingPresentReturnsNil(t *testing.T) {
	d := fakeDeps("linux", nil, nil, nil)
	if devs := probe(d); devs != nil {
		t.Errorf("probe() with nothing on PATH = %+v, want nil", devs)
	}
}

func TestProbeCommandPresentButFailingIsTreatedAsAbsent(t *testing.T) {
	d := fakeDeps("linux",
		map[string]bool{"nvidia-smi": true, "rocm-smi": true},
		map[string]string{"rocm-smi": rocmJSON},
		map[string]error{"nvidia-smi": errors.New("driver not loaded")})
	devs := probe(d)
	if len(devs) != 1 || devs[0].Vendor != AMD {
		t.Fatalf("probe() = %+v, want fallthrough past a failing nvidia-smi to rocm-smi", devs)
	}
}

func TestRunLookPathFailureNeverInvokesRunCmd(t *testing.T) {
	called := false
	d := deps{
		goos:     "linux",
		lookPath: func(string) (string, error) { return "", errors.New("no such file") },
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			called = true
			return nil, nil
		},
	}
	if out, ok := run(d, "nvidia-smi"); ok || out != "" {
		t.Errorf("run() = (%q, %v), want (\"\", false)", out, ok)
	}
	if called {
		t.Error("runCmd should never be called when lookPath fails")
	}
}

func TestRealProbeDoesNotPanicOnThisMachine(t *testing.T) {
	// One real end-to-end call through the actual PATH/subprocess wiring,
	// matching this repo's style for exercising live glue once without
	// asserting on machine-specific content (this CI runner may or may
	// not have any GPU tooling installed at all).
	_ = Probe()
}

func TestRunPlainTextGoesThroughRealProbeAndPrintReport(t *testing.T) {
	out := captureStdout(t, func() {
		if err := Run(false); err != nil {
			t.Fatalf("Run(false): %v", err)
		}
	})
	if out == "" {
		t.Error("Run(false) printed nothing")
	}
}

func TestRunJSONGoesThroughRealProbeAndEncodesDevices(t *testing.T) {
	out := captureStdout(t, func() {
		if err := Run(true); err != nil {
			t.Fatalf("Run(true): %v", err)
		}
	})
	if !strings.Contains(out, `"devices"`) {
		t.Errorf("Run(true) output = %q, want a devices JSON envelope", out)
	}
}
