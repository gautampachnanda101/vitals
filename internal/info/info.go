// Package info reports what's actually running: the binary's own build
// details, the machine vitals is running on, and which config file (if
// any) is in effect — the "about this install" answer to "what exactly
// am I looking at," distinct from doctor's resource-health verdict.
package info

import (
	"fmt"
	"os"
	"runtime"
	"strings"

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

// ConfigStatus is which config file (if any) is in effect, and — the part
// that actually matters to a reader — whether it's changing any behaviour.
// A scaffolded config.toml with every line commented out exists on disk but
// is byte-for-byte equivalent to having no file at all; "loaded" alone
// would imply it's doing something. Overrides lists the TOML keys whose
// effective value differs from the built-in default, so the reader knows
// exactly what their file is (or isn't) doing.
type ConfigStatus struct {
	Path      string   `json:"path"`
	Exists    bool     `json:"exists"`
	Overrides []string `json:"overrides"` // TOML keys whose value differs from the built-in default; empty means "defaults are in effect"
	Active    Config_  `json:"active"`
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
		Path:      path,
		Exists:    statErr == nil,
		Overrides: overriddenKeys(cfg, config.Default()),
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

// overriddenKeys returns the config-file TOML keys whose effective value
// differs from the built-in default — i.e. what the user's config.toml is
// actually changing. Empty means the defaults are in effect (whether or not
// a file exists), which is the common case for a freshly scaffolded,
// all-commented-out template.
func overriddenKeys(got, def config.Config) []string {
	var k []string
	if got.DiskWarnPercent != def.DiskWarnPercent {
		k = append(k, "disk_warn_percent")
	}
	if got.DiskCriticalPercent != def.DiskCriticalPercent {
		k = append(k, "disk_critical_percent")
	}
	if got.RAMWarnPercent != def.RAMWarnPercent {
		k = append(k, "ram_warn_percent")
	}
	if got.RAMHighPercent != def.RAMHighPercent {
		k = append(k, "ram_high_percent")
	}
	if got.CPUOversubscribeMult != def.CPUOversubscribeMult {
		k = append(k, "cpu_oversubscribe_multiplier")
	}
	if got.OllamaURL != def.OllamaURL {
		k = append(k, "ollama_url")
	}
	return k
}

// homeDirFn is os.UserHomeDir, injectable so abbrevHome is testable.
var homeDirFn = os.UserHomeDir

// abbrevHome shortens a path under the user's home directory to a leading
// "~", the way a shell prompt would — purely for display, so a long config
// path doesn't wrap awkwardly in a narrow terminal. The full path stays
// in the --json output untouched.
func abbrevHome(p string) string {
	home, err := homeDirFn()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
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

	overridden := make(map[string]bool, len(r.Config.Overrides))
	for _, k := range r.Config.Overrides {
		overridden[k] = true
	}

	line("file", abbrevHome(r.Config.Path))
	switch n := len(r.Config.Overrides); {
	case !r.Config.Exists:
		line("status", ui.Key("not created yet — run `vitals doctor` to create it"))
	case n == 1:
		line("status", ui.Green+"in use"+ui.Reset+" — 1 value changed from its default (below)")
	case n > 1:
		line("status", ui.Green+"in use"+ui.Reset+fmt.Sprintf(" — %d values changed from their defaults (below)", n))
	default:
		line("status", ui.Green+"in use"+ui.Reset+" — every value still at its built-in default")
	}
	line("change", "edit any line in that file, then re-run — see `vitals guide` › \"Configuration file\"")

	// Show every tunable, its value in effect, and whether that value is the
	// built-in default or one the user set in the file. A bare "loaded" /
	// "not found" told the reader none of that.
	b = append(b, '\n')
	type row struct {
		key, val string
		fromFile bool
	}
	ollama := r.Config.Active.OllamaURL
	if ollama == "" {
		ollama = "(unset)"
	}
	rows := []row{
		{"disk_warn_percent", fmt.Sprintf("%.0f", r.Config.Active.DiskWarnPercent), overridden["disk_warn_percent"]},
		{"disk_critical_percent", fmt.Sprintf("%.0f", r.Config.Active.DiskCriticalPercent), overridden["disk_critical_percent"]},
		{"ram_warn_percent", fmt.Sprintf("%.0f", r.Config.Active.RAMWarnPercent), overridden["ram_warn_percent"]},
		{"ram_high_percent", fmt.Sprintf("%.0f", r.Config.Active.RAMHighPercent), overridden["ram_high_percent"]},
		{"cpu_oversubscribe_multiplier", fmt.Sprintf("%.1f", r.Config.Active.CPUOversubscribeMult), overridden["cpu_oversubscribe_multiplier"]},
		{"ollama_url", ollama, overridden["ollama_url"]},
	}
	b = append(b, []byte(fmt.Sprintf("  %-30s %-22s %s\n", "key", "value", "source"))...)
	for _, rw := range rows {
		src := ui.Key("built-in default")
		if rw.fromFile {
			src = ui.Green + "set in file" + ui.Reset
		}
		b = append(b, []byte(fmt.Sprintf("  %-30s %-22s %s\n", rw.key, rw.val, src))...)
	}

	return string(b)
}
