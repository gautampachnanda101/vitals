package config

import "testing"

func TestParseOverridesRecognisedKeys(t *testing.T) {
	cfg := Default()
	Parse(&cfg, []byte(`
# a build server runs hot all day on purpose
disk_warn_percent = 95
cpu_oversubscribe_multiplier = 4
ollama_url = "http://gpu-box:11434"
`))

	if cfg.DiskWarnPercent != 95 {
		t.Errorf("DiskWarnPercent = %v, want 95", cfg.DiskWarnPercent)
	}
	if cfg.CPUOversubscribeMult != 4 {
		t.Errorf("CPUOversubscribeMult = %v, want 4", cfg.CPUOversubscribeMult)
	}
	if cfg.OllamaURL != "http://gpu-box:11434" {
		t.Errorf("OllamaURL = %q, want the quoted value unquoted", cfg.OllamaURL)
	}
	// Untouched keys keep their default.
	if cfg.DiskCriticalPercent != Default().DiskCriticalPercent {
		t.Errorf("DiskCriticalPercent changed unexpectedly: %v", cfg.DiskCriticalPercent)
	}
}

func TestParseIgnoresGarbage(t *testing.T) {
	cfg := Default()
	before := cfg
	Parse(&cfg, []byte("not a key value line\nunknown_key = 5\ndisk_warn_percent = not-a-number\n"))
	if cfg != before {
		t.Errorf("garbage input should leave Config unchanged, got %+v want %+v", cfg, before)
	}
}

func TestLoadWithNoFileReturnsDefaults(t *testing.T) {
	// os.UserConfigDir() derives from $HOME on macOS/Linux and %AppData% on
	// Windows; pointing both at an empty temp dir guarantees no config.toml
	// is present to load, on any OS the test might run on.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := Load(); got != Default() {
		t.Errorf("Load() with no file = %+v, want Default() %+v", got, Default())
	}
}
