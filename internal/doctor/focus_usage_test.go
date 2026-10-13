package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vitals/internal/diskusage"
)

func TestFocusDiskScanJSONMatchesOutputAndExitState(t *testing.T) {
	for _, state := range []string{"complete", "partial", "failed", "critical"} {
		t.Run(state, func(t *testing.T) {
			options := diskusage.DefaultOptions()
			root := t.TempDir()
			output := filepath.Join(t.TempDir(), "report.json")
			scans, collects := 0, 0
			d := focusDeps{
				home: func() (string, error) { return root, nil },
				collect: func(Options) Snapshot {
					collects++
					if state == "critical" {
						return Snapshot{Disks: []Disk{{Mount: "/", UsedPct: 99}}}
					}
					return Snapshot{}
				},
				scan: func(ctx context.Context, got string, opts diskusage.Options) (diskusage.Result, error) {
					scans++
					if got != root || opts != options || ctx.Err() != nil {
						t.Fatalf("bad scan arguments: %q %+v %v", got, opts, ctx.Err())
					}
					res := diskusage.Result{Root: root, Options: opts, Complete: state == "complete" || state == "critical", ObservedBytes: 6000,
						Directories: []diskusage.Row{{Path: filepath.Join(root, "data"), Bytes: 6000, Complete: true}}}
					if state == "failed" {
						res.ErrorCount = 1
						res.Errors = []diskusage.Issue{{Path: root, Message: "permission denied"}}
						return res, errors.New("permission denied")
					}
					return res, nil
				},
			}
			var code int
			stdout := captureStdout(t, func() {
				code = runFocus("disk", RunOptions{DiskScan: &options, JSON: true, Output: output}, d)
			})
			want := map[string]int{"complete": 0, "partial": 1, "failed": 2, "critical": 2}[state]
			if code != want || scans != 1 || collects != 1 {
				t.Fatalf("code %d want %d, scans %d collects %d", code, want, scans, collects)
			}
			file, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var a, b JSONEnvelope
			if err := json.Unmarshal([]byte(stdout), &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(file, &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) || a.ExitCode != want || a.Resource != "disk" || a.DiskUsage == nil {
				t.Fatalf("stdout/file contract mismatch: %+v vs %+v", a, b)
			}
		})
	}
}

func TestFocusDiskScanFailsBeforeCollectOnInvalidOptionsOrHome(t *testing.T) {
	options := diskusage.DefaultOptions()
	for _, opts := range []RunOptions{
		{DiskScan: &options, Quiet: true},
		{DiskScan: &options, CI: true},
		{DiskScan: &diskusage.Options{}},
		{DiskScan: &options},
	} {
		code := runFocus("disk", opts, focusDeps{
			collect: func(Options) Snapshot { t.Fatal("unexpected collection"); return Snapshot{} },
			home:    func() (string, error) { return "", errors.New("home unavailable") },
		})
		if code != 2 {
			t.Fatalf("got %d for invalid options", code)
		}
	}
	code := runFocus("cpu", RunOptions{DiskScan: &options}, focusDeps{})
	if code != 2 {
		t.Fatalf("scan of non-disk resource returned %d", code)
	}
}

func TestFocusDiskNoScanPreservesOutputShapes(t *testing.T) {
	output := filepath.Join(t.TempDir(), "report.json")
	d := focusDeps{collect: func(Options) Snapshot { return Snapshot{} }}
	stdout := captureStdout(t, func() {
		if code := runFocus("disk", RunOptions{JSON: true, Output: output}, d); code != 0 {
			t.Fatalf("code %d", code)
		}
	})
	file, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"resource": "disk"`) || strings.Contains(string(file), `"resource"`) ||
		strings.Contains(stdout, `"disk_usage"`) || strings.Contains(string(file), `"disk_usage"`) {
		t.Fatalf("no-scan schema changed: stdout %s file %s", stdout, file)
	}
}

func TestPrintDiskUsageExplainsPartialEmptyErrorsAndScope(t *testing.T) {
	options := diskusage.DefaultOptions()
	res := diskusage.Result{Root: "/example", Options: options, StopReasons: []string{"entry limit"},
		Directories: []diskusage.Row{{Path: "/example/partial", Bytes: 100}},
		Files:       []diskusage.Row{{Path: "/example/file", Bytes: 50, Complete: true}},
		Errors:      []diskusage.Issue{{Path: "/example/private", Message: "permission denied"}}, ErrorCount: 1}
	text := captureStdout(t, func() { printDiskUsage(res, errors.New("unreadable")) })
	for _, want := range []string{"Partial", "at least", "Scan failed", "permission denied", "entry limit", "--root", "logical", "snapshots", "owning application", "Mount-boundary"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	res = diskusage.Result{Root: "/empty", Complete: true, BoundaryDetectionAvailable: true,
		RootFiles: []diskusage.Row{{Path: "/empty/zero", Complete: true}}}
	text = captureStdout(t, func() { printDiskUsage(res, nil) })
	if !strings.Contains(text, "No regular-file bytes") || !strings.Contains(text, "Files directly") || strings.Contains(text, "Scan failed") {
		t.Fatal(text)
	}
}

func TestFocusDiskScanOutputFailureStillEmitsResult(t *testing.T) {
	options := diskusage.DefaultOptions()
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d := focusDeps{
		collect: func(Options) Snapshot { return Snapshot{} },
		scan: func(context.Context, string, diskusage.Options) (diskusage.Result, error) {
			return diskusage.Result{Complete: true}, nil
		},
	}
	var code int
	stdout := captureStdout(t, func() {
		code = runFocus("disk", RunOptions{JSON: true, DiskScan: &options, DiskRoot: "/example", Output: filepath.Join(parent, "report.json")}, d)
	})
	var envelope JSONEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != 2 || envelope.ExitCode != 2 || envelope.DiskUsage == nil {
		t.Fatalf("failed output not surfaced: %d %s", code, stdout)
	}
}
