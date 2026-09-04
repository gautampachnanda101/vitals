// Package config loads per-machine overrides for vitals' alert thresholds.
//
// A fixed 90%-disk / 78%-RAM / 2x-load threshold is wrong for a lot of real
// machines: a build server legitimately idles at 90%+ CPU, a media NAS
// legitimately runs near-full, and both would otherwise get an endless stream
// of findings the owner has already decided not to act on. Configurable
// thresholds are the direct fix for that alert fatigue.
//
// The format is intentionally a flat `key = value` list, not TOML/YAML/JSON:
// a handful of numeric knobs don't justify a parsing dependency vitals
// otherwise has no use for (see AGENTS.md's "One dependency" principle —
// still the default for anything hand-rollable, even though a small,
// deliberate exception now exists for reliable terminal color/width
// detection). `#` starts a comment; blank lines are ignored.
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the subset of alert thresholds worth tuning per machine — the
// ones research and lived experience show vary legitimately by host role,
// not the latency/queueing thresholds that stay constant regardless of role.
type Config struct {
	DiskWarnPercent      float64 // vitals disk: nearly-full warning
	DiskCriticalPercent  float64 // vitals disk: nearly-full critical
	RAMWarnPercent       float64 // vitals mem: elevated warning
	RAMHighPercent       float64 // vitals mem: high / exhausted tier
	CPUOversubscribeMult float64 // vitals cpu: load1 >= this * cores triggers a finding
	OllamaURL            string  // default --ollama-url when the flag is not passed
}

// Default returns the values vitals has always used, unchanged by this
// package existing — a machine with no config file behaves exactly as before.
func Default() Config {
	return Config{
		DiskWarnPercent:      90,
		DiskCriticalPercent:  97,
		RAMWarnPercent:       78,
		RAMHighPercent:       90,
		CPUOversubscribeMult: 2.0,
	}
}

// Path returns where vitals looks for the config file, following the same
// os.UserConfigDir() convention as the families.json and disk-history files.
func Path() (string, bool) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, "vitals", "config.toml"), true
}

// DefaultTemplate is what WriteDefault writes: every key commented out at
// its actual current default, so opening the file shows exactly what's in
// effect and how to override it, without silently activating an override
// nobody asked for the moment the file exists (a commented-out line still
// parses as a comment, per Parse's own "# starts a comment" rule).
const DefaultTemplate = `# vitals config — every value below is commented out at its current
# default. Uncomment and edit a line to override it; a missing, unreadable,
# or malformed file is never an error, vitals just falls back to defaults.
# See vitals guide (the "Configuration file" section) for what each key does.

# disk_warn_percent = 90
# disk_critical_percent = 97
# ram_warn_percent = 78
# ram_high_percent = 90
# cpu_oversubscribe_multiplier = 2.0

# default --ollama-url when the flag is omitted, e.g.:
# ollama_url = "http://gpu-box:11434"
`

// WriteDefault writes DefaultTemplate to path, creating any missing parent
// directory (0o700 — this file can carry an internal hostname/URL in
// ollama_url once uncommented, so it gets the same non-world-readable
// treatment as any other per-user vitals state). Fails if path already
// exists — this only ever runs once, on the "no config file yet" path; it
// must never silently overwrite a file the user has since edited.
func WriteDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing config at %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultTemplate), 0o600)
}

// Load reads the config file if present, applying any recognised keys on top
// of Default(). A missing file, an unreadable file, or unknown/malformed
// lines are not errors — Load always returns a usable Config, silently
// ignoring what it can't parse (an unrecognised key is far more likely a
// user typo than something Load should fail the whole program over).
func Load() Config {
	cfg := Default()
	path, ok := Path()
	if !ok {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	Parse(&cfg, data)
	return cfg
}

// Parse applies every recognised `key = value` line in data onto cfg,
// skipping blank lines, comments, and anything it doesn't understand. Split
// out from Load so it's testable without touching the filesystem.
func Parse(cfg *Config, data []byte) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)

		switch key {
		case "disk_warn_percent":
			setFloat(&cfg.DiskWarnPercent, value)
		case "disk_critical_percent":
			setFloat(&cfg.DiskCriticalPercent, value)
		case "ram_warn_percent":
			setFloat(&cfg.RAMWarnPercent, value)
		case "ram_high_percent":
			setFloat(&cfg.RAMHighPercent, value)
		case "cpu_oversubscribe_multiplier":
			setFloat(&cfg.CPUOversubscribeMult, value)
		case "ollama_url":
			cfg.OllamaURL = value
		}
	}
}

func setFloat(dst *float64, raw string) {
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		*dst = v
	}
}
