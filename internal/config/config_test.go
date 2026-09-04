package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolateConfigDir points os.UserConfigDir() at a fresh temp directory —
// Windows reads %AppData%, not $HOME, so both must be set. Matches the
// pattern used across this codebase (internal/doctor, internal/memhogs).
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestPathJoinsUserConfigDir(t *testing.T) {
	isolateConfigDir(t)
	path, ok := Path()
	if !ok {
		t.Fatal("Path() should succeed once os.UserConfigDir() is isolated")
	}
	if filepath.Base(path) != "config.toml" || filepath.Base(filepath.Dir(path)) != "vitals" {
		t.Errorf("Path() = %q, want .../vitals/config.toml", path)
	}
}

func TestLoadReadsAndParsesAnExistingFile(t *testing.T) {
	isolateConfigDir(t)
	path, ok := Path()
	if !ok {
		t.Fatal("Path() failed")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("disk_warn_percent = 95\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.DiskWarnPercent != 95 {
		t.Errorf("Load() with a real config.toml present = %+v, want DiskWarnPercent 95", got)
	}
}

func TestParseOverridesRecognisedKeys(t *testing.T) {
	cfg := Default()
	Parse(&cfg, []byte(`
# a build server runs hot all day on purpose
disk_warn_percent = 95
disk_critical_percent = 99
ram_warn_percent = 85
ram_high_percent = 95
cpu_oversubscribe_multiplier = 4
ollama_url = "http://gpu-box:11434"
`))

	if cfg.DiskWarnPercent != 95 {
		t.Errorf("DiskWarnPercent = %v, want 95", cfg.DiskWarnPercent)
	}
	if cfg.DiskCriticalPercent != 99 {
		t.Errorf("DiskCriticalPercent = %v, want 99", cfg.DiskCriticalPercent)
	}
	if cfg.RAMWarnPercent != 85 {
		t.Errorf("RAMWarnPercent = %v, want 85", cfg.RAMWarnPercent)
	}
	if cfg.RAMHighPercent != 95 {
		t.Errorf("RAMHighPercent = %v, want 95", cfg.RAMHighPercent)
	}
	if cfg.CPUOversubscribeMult != 4 {
		t.Errorf("CPUOversubscribeMult = %v, want 4", cfg.CPUOversubscribeMult)
	}
	if cfg.OllamaURL != "http://gpu-box:11434" {
		t.Errorf("OllamaURL = %q, want the quoted value unquoted", cfg.OllamaURL)
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

func TestPathFailsGracefullyWithNoHomeDefined(t *testing.T) {
	// os.UserConfigDir() itself errors when neither $HOME nor
	// $XDG_CONFIG_HOME (Linux) / $HOME (macOS) is defined — an empty
	// string, not just unset, reliably reproduces that cross-platform.
	t.Setenv("HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, ok := Path(); ok {
		t.Error("Path() should report ok=false when os.UserConfigDir() itself fails")
	}
	if got := Load(); got != Default() {
		t.Errorf("Load() with no resolvable config dir = %+v, want Default()", got)
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

func TestWriteDefaultCreatesAParseableCommentedOutTemplate(t *testing.T) {
	isolateConfigDir(t)
	path, _ := Path()

	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file should exist after WriteDefault: %v", err)
	}

	// Every key is commented out, so Load() against the freshly written
	// file must still report pure defaults — the whole point of writing
	// it commented is that its mere existence changes nothing.
	got := Load()
	want := Default()
	if got != want {
		t.Errorf("Load() after WriteDefault = %+v, want unchanged defaults %+v", got, want)
	}
}

func TestWriteDefaultCreatesTheParentDirectory(t *testing.T) {
	isolateConfigDir(t)
	path, _ := Path()
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Fatal("test setup: parent dir should not already exist")
	}

	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("WriteDefault should create the parent directory, got: %v", err)
	}
}

func TestWriteDefaultRefusesToOverwriteAnExistingFile(t *testing.T) {
	isolateConfigDir(t)
	path, _ := Path()
	if err := WriteDefault(path); err != nil {
		t.Fatalf("first WriteDefault: %v", err)
	}
	if err := os.WriteFile(path, []byte("ram_warn_percent = 55\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteDefault(path); err == nil {
		t.Error("WriteDefault should refuse to overwrite an existing file")
	}

	// The user's own edit must survive untouched.
	got := Load()
	if got.RAMWarnPercent != 55 {
		t.Errorf("WriteDefault's refusal should leave the existing file alone, RAMWarnPercent = %v, want 55", got.RAMWarnPercent)
	}
}

func TestWriteDefaultFailsWhenTheParentCannotBeCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission denial doesn't work the same way on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) // restore so t.TempDir() can clean up

	path := filepath.Join(dir, "vitals", "config.toml")
	if err := WriteDefault(path); err == nil {
		t.Error("WriteDefault should fail when its parent directory can't be created")
	}
}
