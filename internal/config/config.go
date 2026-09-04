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
	LMStudioURL          string  // default --lmstudio-url when the flag is not passed
	LlamaCppURL          string  // default --llamacpp-url when the flag is not passed
	VLLMURL              string  // default --vllm-url when the flag is not passed
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

// DefaultFileContents renders a ready-to-use config file: every tunable
// written out at its current built-in default, uncommented, so the file is
// functional the moment it exists and changing a threshold is a one-line
// edit with nothing to un-comment first. It is generated from Default() so
// the written values can never drift from the code's actual defaults.
// ollama_url and its LM Studio / llama.cpp / vLLM counterparts have no
// default (each is opt-in — vitals already probes their well-known local
// ports without any config at all), so they're the commented examples
// rather than written-out empty strings.
func DefaultFileContents() string {
	d := Default()
	return fmt.Sprintf(`# vitals configuration — these are the built-in defaults, written out so you
# can edit them in place. A missing, unreadable, or malformed file is never
# an error; vitals falls back to these same values. Run `+"`vitals guide`"+` and
# read the "Configuration file" section for what each key controls.

disk_warn_percent = %.0f
disk_critical_percent = %.0f
ram_warn_percent = %.0f
ram_high_percent = %.0f
cpu_oversubscribe_multiplier = %.1f

# The four local-LLM runtime URLs below have no default — vitals already
# probes each one's well-known local port without any config at all.
# Uncomment to override the --*-url flag's default for a runtime that
# isn't on its usual port (e.g. a GPU box on the LAN):
# ollama_url = "http://gpu-box:11434"
# lmstudio_url = "http://gpu-box:1234"
# llamacpp_url = "http://gpu-box:8080"
# vllm_url = "http://gpu-box:8000"
`, d.DiskWarnPercent, d.DiskCriticalPercent, d.RAMWarnPercent, d.RAMHighPercent, d.CPUOversubscribeMult)
}

// WriteDefault writes DefaultFileContents() to path, creating any missing
// parent directory (0o700 — this file can carry an internal hostname/URL in
// ollama_url once set, so it gets the same non-world-readable treatment as
// any other per-user vitals state). Fails if path already exists — it must
// never silently overwrite a file the user has since edited.
func WriteDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing config at %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultFileContents()), 0o600)
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
		case "lmstudio_url":
			cfg.LMStudioURL = value
		case "llamacpp_url":
			cfg.LlamaCppURL = value
		case "vllm_url":
			cfg.VLLMURL = value
		}
	}
}

func setFloat(dst *float64, raw string) {
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		*dst = v
	}
}
