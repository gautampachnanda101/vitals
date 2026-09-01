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

func TestEmbeddedFamiliesJSONIsValid(t *testing.T) {
	fams, err := parseFamilies(defaultFamiliesJSON)
	if err != nil {
		t.Fatalf("embedded families.json does not parse: %v", err)
	}
	names := map[string]bool{}
	for _, f := range fams {
		names[f.name] = true
	}
	for _, want := range []string{"Google Chrome", "Docker", "Ollama", "VS Code"} {
		if !names[want] {
			t.Errorf("embedded families missing %q", want)
		}
	}
	// Chrome pattern must actually match a real Chrome helper command line.
	var chrome family
	for _, f := range fams {
		if f.name == "Google Chrome" {
			chrome = f
		}
	}
	if !chrome.re.MatchString("Google Chrome Helper (Renderer)") {
		t.Error("Chrome family regex no longer matches a Chrome helper")
	}
}

func TestParseFamiliesErrors(t *testing.T) {
	if _, err := parseFamilies([]byte(`[{"name":"X","pattern":"(","stop":"kill"}]`)); err == nil {
		t.Error("expected an error for an invalid regexp")
	}
	if _, err := parseFamilies([]byte(`[{"name":"X","pattern":"x","stop":"nope"}]`)); err == nil {
		t.Error("expected an error for an unknown stop kind")
	}
	if _, err := parseFamilies([]byte(`not json`)); err == nil {
		t.Error("expected an error for non-JSON")
	}
}

func TestMergeFamilies(t *testing.T) {
	base, _ := parseFamilies([]byte(`[
		{"name":"Chrome","pattern":"chrome","stop":"quit-app"},
		{"name":"Docker","pattern":"docker","stop":"docker-all"}
	]`))
	user, _ := parseFamilies([]byte(`[
		{"name":"Docker","pattern":"my-docker","stop":"kill"},
		{"name":"MyApp","pattern":"myapp","stop":"kill"}
	]`))

	merged := mergeFamilies(base, user)
	if len(merged) != 3 {
		t.Fatalf("want 3 families after merge, got %d", len(merged))
	}
	byName := map[string]family{}
	for _, f := range merged {
		byName[f.name] = f
	}
	if byName["Docker"].kind != stopKill {
		t.Error("user override of Docker did not take effect")
	}
	if _, ok := byName["MyApp"]; !ok {
		t.Error("new user family MyApp was not appended")
	}
	if byName["Chrome"].kind != stopQuitApp {
		t.Error("untouched base family Chrome was altered")
	}
}
