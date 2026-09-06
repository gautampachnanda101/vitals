package containers

import (
	"context"
	"encoding/json"
	"net"
	"strings"
)

// probeKubernetes returns a Report built from `kubectl get pods` — but
// only when kubectl is installed AND its current context points at a
// local API server (loopback / private range / .local). A context
// aimed at a cloud cluster is skipped entirely: vitals makes no network
// call and touches no credentials.
func probeKubernetes(ctx context.Context, t transport) (Report, bool) {
	if _, err := t.lookPath("kubectl"); err != nil {
		return Report{}, false
	}
	kctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	name, err := t.run(kctx, "kubectl", "config", "current-context")
	if err != nil || len(strings.TrimSpace(string(name))) == 0 {
		return Report{}, false
	}
	ctxName := sanitize(string(name))

	server, err := t.run(kctx, "kubectl", "config", "view", "--minify", "-o",
		"jsonpath={.clusters[0].cluster.server}")
	if err != nil || !isLocalAPIServer(string(server)) {
		return Report{}, false // no server, or a non-local (cloud) context — ignore
	}

	out, err := t.run(kctx, "kubectl", "get", "pods", "--all-namespaces",
		"-o", "json", "--request-timeout=2s")
	if err != nil {
		return Report{Runtime: "kubernetes", Endpoint: ctxName,
			Note: "kubectl could not list pods: " + firstLine(err.Error())}, true
	}
	return Report{
		Runtime:    "kubernetes",
		Reachable:  true,
		Endpoint:   ctxName,
		Containers: parsePods(out),
	}, true
}

// isLocalAPIServer is the gate that keeps this feature off cloud
// clusters: the kube context's server URL host must be a loopback
// address, an RFC1918 / CGNAT / link-local address, or an obviously
// local hostname.
func isLocalAPIServer(rawURL string) bool {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return false
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if i := strings.IndexAny(s, "/"); i >= 0 {
		s = s[:i]
	}
	host := s
	if h, _, err := net.SplitHostPort(s); err == nil {
		host = h
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "kubernetes.docker.internal" ||
		strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	// 100.64.0.0/10 (CGNAT, used by k3s/some VMs) isn't covered by IsPrivate.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}

type podList struct {
	Items []struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct {
				Image string `json:"image"`
			} `json:"containers"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Name         string `json:"name"`
				Image        string `json:"image"`
				Ready        bool   `json:"ready"`
				RestartCount int    `json:"restartCount"`
				State        map[string]struct {
					Reason string `json:"reason"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

// parsePods normalises `kubectl get pods -o json` into Containers: one
// row per pod, State from the pod phase, WaitingOn from the first
// container stuck in a waiting reason (CrashLoopBackOff, ImagePullBackOff,
// ...), OOMKilled from a terminated reason, RestartCount as the max
// across the pod's containers.
func parsePods(body []byte) []Container {
	var pl podList
	if err := json.Unmarshal(body, &pl); err != nil {
		return nil
	}
	out := make([]Container, 0, len(pl.Items))
	for _, p := range pl.Items {
		if len(out) >= maxContainers {
			break
		}
		c := Container{
			Name:      sanitize(p.Metadata.Name),
			Namespace: sanitize(p.Metadata.Namespace),
			State:     sanitize(p.Status.Phase),
			Status:    sanitize(p.Status.Phase),
		}
		if proj := p.Metadata.Labels["app.kubernetes.io/name"]; proj != "" {
			c.ComposeProj = sanitize(proj)
		}
		if len(p.Spec.Containers) > 0 {
			c.Image = sanitize(p.Spec.Containers[0].Image)
		}
		ready := 0
		for _, cs := range p.Status.ContainerStatuses {
			if cs.RestartCount > c.RestartCount {
				c.RestartCount = cs.RestartCount
			}
			if cs.Ready {
				ready++
			}
			if w, ok := cs.State["waiting"]; ok && w.Reason != "" && c.WaitingOn == "" {
				c.WaitingOn = sanitize(w.Reason)
			}
			if tm, ok := cs.State["terminated"]; ok {
				if strings.EqualFold(tm.Reason, "OOMKilled") {
					c.OOMKilled = true
				}
			}
			if c.Image == "" && cs.Image != "" {
				c.Image = sanitize(cs.Image)
			}
		}
		total := len(p.Status.ContainerStatuses)
		if total > 0 {
			c.Status = phaseWithReady(p.Status.Phase, ready, total)
			if ready < total && strings.EqualFold(p.Status.Phase, "Running") {
				c.Health = "unhealthy" // Running phase but a container isn't Ready
			}
		}
		out = append(out, c)
	}
	return out
}

// State normalises to lowercase docker-style words so downstream
// severity logic (Running()) works the same for both runtimes.
func phaseWithReady(phase string, ready, total int) string {
	return phase + " (" + itoa(ready) + "/" + itoa(total) + " ready)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
