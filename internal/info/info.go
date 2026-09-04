// Package info reports what's actually running: the binary's own build
// details, the machine vitals is running on, and which config file (if
// any) is in effect — the "about this install" answer to "what exactly
// am I looking at," distinct from doctor's resource-health verdict.
package info

import (
	"fmt"
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v4/host"

	"vitals/internal/config"
	"vitals/internal/ui"
)

// Options configures Run.
type Options struct {
	JSON bool
}

// hostInfoFn is the live gopsutil call Collect makes, injected so a test
// can substitute a fake — same pattern as internal/monitor's and
// internal/memcheck's source structs.
var hostInfoFn = host.Info

// executableFn is the live os.Executable call, injected for the same
// reason: it can fail in ways a test wants to simulate without actually
// needing a broken environment.
var executableFn = os.Executable

// Binary is the running binary's own build/runtime details.
type Binary struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Path      string `json:"path,omitempty"` // empty if os.Executable() failed
}

// Machine is the host vitals is currently running on.
type Machine struct {
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	Kernel     string `json:"kernel"`
	UptimeSecs uint64 `json:"uptime_seconds"`
	Cores      int    `json:"cores"`
}

// ConfigStatus is which config file (if any) is in effect.
type ConfigStatus struct {
	Path   string  `json:"path"`
	Exists bool    `json:"exists"`
	Active Config_ `json:"active"`
}

// Config_ mirrors internal/config.Config's fields — a separate type (not
// a re-export) so this package's JSON shape doesn't silently change if
// internal/config's own struct grows a field vitals info hasn't decided
// to show yet.
type Config_ struct {
	DiskWarnPercent      float64 `json:"disk_warn_percent"`
	DiskCriticalPercent  float64 `json:"disk_critical_percent"`
	RAMWarnPercent       float64 `json:"ram_warn_percent"`
	RAMHighPercent       float64 `json:"ram_high_percent"`
	CPUOversubscribeMult float64 `json:"cpu_oversubscribe_multiplier"`
	OllamaURL            string  `json:"ollama_url,omitempty"`
}

// Report is everything `vitals info` shows or emits as --json.
type Report struct {
	Binary  Binary       `json:"binary"`
	Machine Machine      `json:"machine"`
	Config  ConfigStatus `json:"config"`
}

// Collect gathers Report — the one live-glue function in this package
// (a gopsutil call, a filesystem stat, os.Executable); everything else
// here is pure and directly testable.
func Collect(version string) Report {
	r := Report{
		Binary: Binary{
			Version:   version,
			GoVersion: runtime.Version(),
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
		},
		Machine: Machine{Cores: runtime.NumCPU()},
	}

	if exe, err := executableFn(); err == nil {
		r.Binary.Path = exe
	}

	if hi, err := hostInfoFn(); err == nil {
		r.Machine.Hostname = hi.Hostname
		r.Machine.OS = fmt.Sprintf("%s %s", hi.Platform, hi.PlatformVersion)
		r.Machine.Kernel = hi.KernelArch
		r.Machine.UptimeSecs = hi.Uptime
	}

	cfg := config.Load()
	path, _ := config.Path()
	_, statErr := os.Stat(path)
	r.Config = ConfigStatus{
		Path:   path,
		Exists: statErr == nil,
		Active: Config_{
			DiskWarnPercent:      cfg.DiskWarnPercent,
			DiskCriticalPercent:  cfg.DiskCriticalPercent,
			RAMWarnPercent:       cfg.RAMWarnPercent,
			RAMHighPercent:       cfg.RAMHighPercent,
			CPUOversubscribeMult: cfg.CPUOversubscribeMult,
			OllamaURL:            cfg.OllamaURL,
		},
	}
	return r
}

// humanUptime renders seconds as "2d 3h 14m" — coarsest-first, dropping
// any leading zero units so "45m" doesn't render as "0d 0h 45m".
func humanUptime(secs uint64) string {
	d := secs / 86400
	h := (secs % 86400) / 3600
	m := (secs % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh %dm", d, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// Render formats Report for the terminal.
func Render(r Report) string {
	var b []byte
	line := func(k, v string) { b = append(b, []byte(fmt.Sprintf("  %-18s %s\n", k, v))...) }

	b = append(b, []byte(fmt.Sprintf("%sBinary%s\n", ui.Bold, ui.Reset))...)
	line("version", r.Binary.Version)
	line("go version", r.Binary.GoVersion)
	line("platform", r.Binary.OS+"/"+r.Binary.Arch)
	if r.Binary.Path != "" {
		line("path", r.Binary.Path)
	}

	b = append(b, []byte(fmt.Sprintf("\n%sMachine%s\n", ui.Bold, ui.Reset))...)
	if r.Machine.Hostname != "" {
		line("hostname", r.Machine.Hostname)
	}
	if r.Machine.OS != "" {
		line("os", r.Machine.OS)
	}
	if r.Machine.Kernel != "" {
		line("kernel", r.Machine.Kernel)
	}
	if r.Machine.UptimeSecs > 0 {
		line("uptime", humanUptime(r.Machine.UptimeSecs))
	}
	line("cores", fmt.Sprintf("%d", r.Machine.Cores))

	b = append(b, []byte(fmt.Sprintf("\n%sConfig%s\n", ui.Bold, ui.Reset))...)
	line("path", r.Config.Path)
	if r.Config.Exists {
		line("status", ui.Green+"loaded"+ui.Reset)
	} else {
		line("status", ui.Key("not found — using built-in defaults"))
	}
	line("disk warn / crit", fmt.Sprintf("%.0f%% / %.0f%%", r.Config.Active.DiskWarnPercent, r.Config.Active.DiskCriticalPercent))
	line("ram warn / high", fmt.Sprintf("%.0f%% / %.0f%%", r.Config.Active.RAMWarnPercent, r.Config.Active.RAMHighPercent))
	line("cpu oversubscribe", fmt.Sprintf("%.1fx", r.Config.Active.CPUOversubscribeMult))
	if r.Config.Active.OllamaURL != "" {
		line("ollama url", r.Config.Active.OllamaURL)
	}

	return string(b)
}
