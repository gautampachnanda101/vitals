package memhogs

import (
	"regexp"
	"strings"
)

// appFamily derives a process's application family from the operating system's
// own app-grouping convention, with no subprocess: the .app bundle on macOS,
// the systemd/flatpak/snap cgroup scope on Linux, the install directory on
// Windows. It returns "" when the OS signal doesn't identify an app, leaving
// the caller to fall back to the cross-app regex families.
//
// exePath is the process executable path (gopsutil Process.Exe); cgroup is the
// contents of /proc/<pid>/cgroup (Linux only, "" elsewhere).
func appFamily(goos, exePath, cgroup string) string {
	switch goos {
	case "darwin":
		return macAppFromPath(exePath)
	case "linux":
		return linuxAppFromCgroup(cgroup)
	case "windows":
		return windowsAppFromPath(exePath)
	default:
		return ""
	}
}

// macAppFromPath pulls the outermost .app bundle name from a path, so every
// helper under Google Chrome.app -> "Google Chrome" regardless of nesting:
// /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Helper.app/... -> "Google Chrome".
func macAppFromPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	for _, seg := range strings.Split(p, "/") {
		if strings.HasSuffix(seg, ".app") {
			return strings.TrimSuffix(seg, ".app")
		}
	}
	return ""
}

var (
	// app-<reverse.dns.id>[-<hash>].scope  /  app-flatpak-<id>[-<hash>].scope
	cgroupAppScope = regexp.MustCompile(`app-(?:flatpak-)?(.+?)(?:-[0-9a-fA-F]+)?\.(?:scope|service)`)
	// snap.<pkg>.<app>.<...>.scope
	cgroupSnapScope = regexp.MustCompile(`snap\.([A-Za-z0-9_-]+)\.`)
)

// linuxAppFromCgroup reads the app identity out of a /proc/<pid>/cgroup body.
func linuxAppFromCgroup(cgroup string) string {
	if m := cgroupSnapScope.FindStringSubmatch(cgroup); m != nil {
		return prettifyAppID(m[1])
	}
	if m := cgroupAppScope.FindStringSubmatch(cgroup); m != nil {
		id := m[1]
		// A bare "app-<user>@<n>.service" wrapper is not an application.
		if id == "" || strings.Contains(id, "@") {
			return ""
		}
		return prettifyAppID(id)
	}
	return ""
}

// windowsAppFromPath takes the app folder from a typical install layout:
// C:\Program Files\Microsoft VS Code\Code.exe          -> "Microsoft VS Code"
// C:\Users\x\AppData\Local\Programs\Slack\slack.exe    -> "Slack"
func windowsAppFromPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	segs := strings.Split(p, "/")
	if len(segs) < 2 {
		return ""
	}
	markers := map[string]bool{
		"program files": true, "program files (x86)": true, "programs": true,
	}
	for i := 0; i < len(segs)-1; i++ {
		if markers[strings.ToLower(segs[i])] {
			// the folder right after the marker is the vendor/app folder
			return segs[i+1]
		}
	}
	// Fall back to the immediate parent folder of the exe, if it isn't generic.
	parent := segs[len(segs)-2]
	switch strings.ToLower(parent) {
	case "bin", "system32", "windows", "":
		return ""
	default:
		return parent
	}
}

// prettifyAppID turns "org.mozilla.firefox" / "com.visualstudio.code" into a
// human label ("firefox", "code"); leaves single-token ids alone.
func prettifyAppID(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}
