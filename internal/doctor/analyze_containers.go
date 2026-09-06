package doctor

import (
	"fmt"

	"vitals/internal/containers"
	"vitals/internal/diag"
	"vitals/internal/ui"
)

// k8sBackoffReasons are the container-status "waiting" reasons that mean
// a pod is stuck, not merely slow to start.
var k8sBackoffReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"InvalidImageName":           true,
}

// analyzeContainers turns the container-runtime picture into ranked
// findings. It never manages anything — every Fix is a `docker`/`kubectl`
// command the user runs. Nothing fires unless a runtime actually
// answered (rep.Reachable).
func analyzeContainers(r *diag.Report, s Snapshot) {
	rep := s.Containers
	if !rep.Reachable {
		return
	}
	kind := containerNoun(rep.Runtime)

	for _, c := range rep.Containers {
		label := ui.Sanitize(c.Name)
		switch {
		case c.OOMKilled:
			r.Add(diag.Finding{
				Severity: diag.Critical,
				Title:    fmt.Sprintf("%s %s was OOM-killed", kind, label),
				Detail:   "the kernel killed it for exceeding its memory limit" + exitCodeNote(c),
				Fixes:    inspectFixes(rep.Runtime, c, "raise its memory limit, or fix the leak that grows its working set"),
			})
		case k8sBackoffReasons[c.WaitingOn]:
			r.Add(diag.Finding{
				Severity: diag.Critical,
				Title:    fmt.Sprintf("Pod %s is stuck in %s", label, c.WaitingOn),
				Detail:   "it is not running and Kubernetes is backing off from retrying it",
				Fixes: []string{
					kubectlCmd("describe pod", c),
					kubectlCmd("logs --previous", c),
				},
			})
		case c.RestartCount >= 5:
			state := "and is not running"
			if c.Running() {
				state = "but is currently up"
			}
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("%s %s has restarted %d times", kind, label, c.RestartCount),
				Detail:   fmt.Sprintf("a restart loop %s — something makes it exit shortly after start", state),
				Fixes:    inspectFixes(rep.Runtime, c, "check its logs for the repeating failure"),
			})
		case c.Health == "unhealthy":
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("%s %s is unhealthy", kind, label),
				Detail:   "its healthcheck is failing while the container stays up — callers may be seeing errors",
				Fixes:    inspectFixes(rep.Runtime, c, "exercise the healthcheck command by hand to see why it fails"),
			})
		}
	}

	// The container runtime's own VM can hold gigabytes that never appear
	// in the host process list (its footprint is balloon-managed). When
	// the machine is already tight on RAM, say so — this is the gap a
	// host-only "top processes by memory" view can't show.
	if rep.VMTotalBytes >= 4<<30 && s.Memory.UsedPct >= 85 {
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "Container runtime VM is holding a large share of memory",
			Detail: fmt.Sprintf("the %s VM is sized at %s while system memory is %.0f%% used; that memory is real but won't show up ranked in the host process list",
				rep.Runtime, ui.HumanBytes(int64(rep.VMTotalBytes)), s.Memory.UsedPct),
			Fixes: []string{
				"lower the runtime VM's memory allocation in its settings if containers don't need it",
				"stop idle containers to let the VM reclaim, or `docker system prune`",
			},
		})
	}
}

func containerNoun(runtime string) string {
	if runtime == "kubernetes" {
		return "Pod"
	}
	return "Container"
}

func exitCodeNote(c containers.Container) string {
	if c.ExitCode != 0 {
		return fmt.Sprintf(" (exit %d)", c.ExitCode)
	}
	return ""
}

// inspectFixes builds the runtime-appropriate "go look" commands plus a
// tailored remediation line.
func inspectFixes(runtime string, c containers.Container, remedy string) []string {
	if runtime == "kubernetes" {
		return []string{kubectlCmd("describe pod", c), kubectlCmd("logs", c), remedy}
	}
	name := ui.Sanitize(c.Name)
	return []string{"docker logs " + name, "docker inspect " + name, remedy}
}

func kubectlCmd(verb string, c containers.Container) string {
	name := ui.Sanitize(c.Name)
	if c.Namespace != "" {
		return fmt.Sprintf("kubectl %s %s -n %s", verb, name, ui.Sanitize(c.Namespace))
	}
	return fmt.Sprintf("kubectl %s %s", verb, name)
}
