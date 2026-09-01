package memhogs

import "testing"

func TestAppFamilyDarwin(t *testing.T) {
	cases := map[string]string{
		// A helper nested inside Chrome.app groups under the outer bundle.
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Helper (GPU).app/Contents/MacOS/x": "Google Chrome",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome":                                        "Google Chrome",
		"/Applications/Visual Studio Code.app/Contents/MacOS/Electron":                                        "Visual Studio Code",
		"/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder":                                       "Finder",
		"/usr/bin/ssh": "",
		"":             "",
	}
	for path, want := range cases {
		if got := appFamily("darwin", path, ""); got != want {
			t.Errorf("appFamily(darwin, %q) = %q, want %q", path, got, want)
		}
	}
}

func TestAppFamilyLinux(t *testing.T) {
	cases := []struct {
		name, cgroup, want string
	}{
		{
			"systemd app scope",
			"0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.mozilla.firefox-1234.scope",
			"firefox",
		},
		{
			"flatpak scope",
			"0::/user.slice/.../app.slice/app-flatpak-com.visualstudio.code-9931.scope",
			"code",
		},
		{
			"snap scope",
			"0::/user.slice/.../snap.spotify.spotify.abc123.scope",
			"spotify",
		},
		{
			"plain system service is not an app",
			"0::/system.slice/sshd.service",
			"",
		},
		{
			"session wrapper is not an app",
			"0::/user.slice/user-1000.slice/user@1000.service/init.scope",
			"",
		},
	}
	for _, c := range cases {
		if got := appFamily("linux", "", c.cgroup); got != c.want {
			t.Errorf("%s: appFamily(linux) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAppFamilyWindows(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\Microsoft VS Code\Code.exe`:                 "Microsoft VS Code",
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`: "Google",
		`C:\Users\sam\AppData\Local\Programs\Slack\slack.exe`:         "Slack",
		`C:\Windows\System32\svchost.exe`:                             "",
		``:                                                            "",
	}
	for path, want := range cases {
		if got := appFamily("windows", path, ""); got != want {
			t.Errorf("appFamily(windows, %q) = %q, want %q", path, got, want)
		}
	}
}

func TestAppFamilyUnknownOS(t *testing.T) {
	if got := appFamily("plan9", "/bin/rc", ""); got != "" {
		t.Errorf("unknown OS should yield no family, got %q", got)
	}
}
