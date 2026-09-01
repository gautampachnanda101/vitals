package memhogs

import "testing"

func TestStopCommand(t *testing.T) {
	cases := []struct {
		name    string
		kind    stopKind
		pattern string
		pid     int32
		goos    string
		want    string
	}{
		{"kill on linux", stopKill, "", 42, "linux", "kill 42"},
		{"kill on macos", stopKill, "", 42, "darwin", "kill 42"},
		{"kill on windows", stopKill, "", 42, "windows", "Stop-Process -Id 42"},

		{"pattern on linux", stopPattern, "firefox", 42, "linux", "pkill -f 'firefox'"},
		{"pattern on macos", stopPattern, "Safari", 7, "darwin", "pkill -f 'Safari'"},
		{"pattern falls back to pid on windows", stopPattern, "firefox", 42, "windows", "Stop-Process -Id 42"},

		{"quit app on macos", stopQuitApp, "", 42, "darwin", "quit the app, or kill 42"},
		{"quit app on windows", stopQuitApp, "", 42, "windows", "close the app, or Stop-Process -Id 42"},

		{"docker on linux", stopDockerAll, "", 0, "linux", "docker stop $(docker ps -q)"},
		{"docker on windows", stopDockerAll, "", 0, "windows", "docker ps -q | ForEach-Object { docker stop $_ }"},

		{"ollama on linux", stopOllama, "", 42, "linux", "ollama stop <model>, or kill 42"},
		{"ollama on windows", stopOllama, "", 42, "windows", "ollama stop <model>, or Stop-Process -Id 42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stopCommand(c.kind, c.pattern, c.pid, c.goos); got != c.want {
				t.Errorf("stopCommand(%d, %q, %d, %q) = %q, want %q",
					c.kind, c.pattern, c.pid, c.goos, got, c.want)
			}
		})
	}
}

func TestFamiliesCompile(t *testing.T) {
	fams := families()
	if len(fams) == 0 {
		t.Fatal("families() returned nothing")
	}
	for _, f := range fams {
		if f.name == "" || f.re == nil {
			t.Errorf("family %+v is missing a name or regexp", f)
		}
		// Every family must render a non-empty action on every supported OS.
		for _, goos := range []string{"linux", "darwin", "windows"} {
			if got := stopCommand(f.kind, f.pattern, 1, goos); got == "" {
				t.Errorf("family %q yields an empty action on %s", f.name, goos)
			}
		}
	}
}
