package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The complete set of Engine API paths this package will ever request.
// All GET, all read-only. Anything not on this list is a bug.
//
//	GET /_ping                      - is the daemon alive
//	GET /info                       - MemTotal (the runtime VM's RAM ceiling)
//	GET /containers/json?all=1      - every container: name, image, state, status, labels
//	GET /containers/{id}/json       - inspect (RestartCount, State.OOMKilled) for not-cleanly-running containers only
//	GET /containers/{id}/stats?stream=false - one CPU/mem sample, opt-in via Sample()
const (
	epPing    = "/_ping"
	epInfo    = "/info"
	epList    = "/containers/json?all=1"
	epInspect = "/containers/%s/json"
	epStats   = "/containers/%s/stats?stream=false"
)

// maxInspect bounds the follow-up inspect pass (RestartCount / OOMKilled
// need the inspect endpoint; the list endpoint doesn't carry them).
const maxInspect = 25

func probeDocker(ctx context.Context, t transport) (Report, bool) {
	socket := t.dockerEndpoint()
	if socket == "" {
		return Report{}, false
	}
	cl, base := newDockerHTTP(t, socket)

	pctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if _, err := dockerGET(pctx, cl, base, epPing); err != nil {
		// Socket present but daemon not answering — a real, common state.
		return Report{Runtime: "docker", Endpoint: socket, Note: "docker socket present but the daemon did not respond"}, true
	}

	dctx, cancel2 := context.WithTimeout(ctx, probeTimeout)
	defer cancel2()

	r := Report{Runtime: "docker", Reachable: true, Endpoint: socket}
	if body, err := dockerGET(dctx, cl, base, epInfo); err == nil {
		var info struct {
			MemTotal int64 `json:"MemTotal"`
		}
		if json.Unmarshal(body, &info) == nil && info.MemTotal > 0 {
			r.VMTotalBytes = uint64(info.MemTotal)
		}
	}

	body, err := dockerGET(dctx, cl, base, epList)
	if err != nil {
		r.Note = "could not list containers: " + err.Error()
		return r, true
	}
	r.Containers = parseContainerList(body)
	inspectNotRunning(dctx, cl, base, r.Containers)
	return r, true
}

// dockerGET issues one GET and returns the body, rejecting any non-2xx.
// It exists so every Engine API call in this package goes through one
// place that can never be made to use another method.
func dockerGET(ctx context.Context, cl *http.Client, base, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("engine API %s: %s", path, strings.TrimSpace(string(body)))
	}
	return body, nil
}

type apiContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

var statusExitRE = regexp.MustCompile(`\((\d+)\)`)

// parseContainerList turns /containers/json output into normalised
// Containers, newest-first and capped. Health and exit code are read out
// of the human Status blurb ("Up 2h (unhealthy)", "Exited (137) ...")
// since the list endpoint carries no structured field for either.
func parseContainerList(body []byte) []Container {
	var raw []apiContainer
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	out := make([]Container, 0, len(raw))
	for _, rc := range raw {
		if len(out) >= maxContainers {
			break
		}
		c := Container{
			ID:     shortID(rc.ID),
			Name:   sanitize(firstName(rc.Names)),
			Image:  sanitize(rc.Image),
			State:  sanitize(rc.State),
			Status: sanitize(rc.Status),
		}
		if p := rc.Labels["com.docker.compose.project"]; p != "" {
			c.ComposeProj = sanitize(p)
		}
		lower := strings.ToLower(rc.Status)
		switch {
		case strings.Contains(lower, "(unhealthy)"):
			c.Health = "unhealthy"
		case strings.Contains(lower, "(healthy)"):
			c.Health = "healthy"
		case strings.Contains(lower, "(health: starting)"):
			c.Health = "starting"
		}
		if strings.HasPrefix(rc.Status, "Exited ") || strings.HasPrefix(rc.Status, "Restarting ") {
			if m := statusExitRE.FindStringSubmatch(rc.Status); m != nil {
				c.ExitCode, _ = strconv.Atoi(m[1])
			}
		}
		out = append(out, c)
	}
	return out
}

// inspectNotRunning fills RestartCount and OOMKilled (list endpoint has
// neither) for containers that aren't cleanly running — exited,
// restarting, dead, or unhealthy — capped at maxInspect so a host with
// many dead containers can't fan this out.
func inspectNotRunning(ctx context.Context, cl *http.Client, base string, cs []Container) {
	n := 0
	for i := range cs {
		c := &cs[i]
		if c.Running() && c.Health != "unhealthy" {
			continue
		}
		if n >= maxInspect {
			return
		}
		n++
		body, err := dockerGET(ctx, cl, base, fmt.Sprintf(epInspect, c.ID))
		if err != nil {
			continue
		}
		var ins struct {
			RestartCount int `json:"RestartCount"`
			State        struct {
				OOMKilled bool `json:"OOMKilled"`
				ExitCode  int  `json:"ExitCode"`
			} `json:"State"`
		}
		if json.Unmarshal(body, &ins) != nil {
			continue
		}
		c.RestartCount = ins.RestartCount
		c.OOMKilled = ins.State.OOMKilled
		if c.ExitCode == 0 && ins.State.ExitCode != 0 {
			c.ExitCode = ins.State.ExitCode
		}
	}
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

// Sample fetches one CPU/mem stats sample for the running containers in
// r and returns r with those fields filled. Opt-in (each call blocks
// ~1s per container server-side, run concurrently under budget) — it is
// NOT part of the doctor snapshot path; `vitals containers` and the
// dashboard Containers page call it.
func Sample(ctx context.Context, r Report) Report {
	return sampleStats(ctx, defaultTransport, r)
}

func sampleStats(ctx context.Context, t transport, r Report) Report {
	if r.Runtime != "docker" || !r.Reachable || len(r.Containers) == 0 {
		return r
	}
	socket := r.Endpoint
	if socket == "" {
		socket = t.dockerEndpoint()
	}
	cl, base := newDockerHTTP(t, socket)

	sctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	type result struct {
		idx           int
		cpu           float64
		mem, memLimit uint64
	}
	ch := make(chan result)
	want := 0
	for i := range r.Containers {
		if !r.Containers[i].Running() {
			continue
		}
		want++
		go func(idx int, id string) {
			cpu, mem, lim := oneStat(sctx, cl, base, id)
			ch <- result{idx, cpu, mem, lim}
		}(i, r.Containers[i].ID)
	}
	for ; want > 0; want-- {
		res := <-ch
		r.Containers[res.idx].CPUPct = res.cpu
		r.Containers[res.idx].MemBytes = res.mem
		r.Containers[res.idx].MemLimitBytes = res.memLimit
	}
	return r
}

// oneStat reads a single /stats?stream=false sample and computes the
// CPU% the way `docker stats` does (delta CPU / delta system * ncpu).
func oneStat(ctx context.Context, cl *http.Client, base, id string) (cpuPct float64, mem, memLimit uint64) {
	body, err := dockerGET(ctx, cl, base, fmt.Sprintf(epStats, id))
	if err != nil {
		return 0, 0, 0
	}
	var s struct {
		CPUStats struct {
			CPUUsage struct {
				Total  uint64   `json:"total_usage"`
				PerCPU []uint64 `json:"percpu_usage"`
			} `json:"cpu_usage"`
			System     uint64 `json:"system_cpu_usage"`
			OnlineCPUs int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemStats struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if json.Unmarshal(body, &s) != nil {
		return 0, 0, 0
	}
	mem = s.MemStats.Usage
	if cache := s.MemStats.Stats["cache"]; cache > 0 && cache <= mem {
		mem -= cache // cgroup v1 counts page cache in usage; docker stats subtracts it
	}
	memLimit = s.MemStats.Limit

	cpuDelta := float64(s.CPUStats.CPUUsage.Total) - float64(s.PreCPUStats.CPUUsage.Total)
	sysDelta := float64(s.CPUStats.System) - float64(s.PreCPUStats.System)
	ncpu := float64(s.CPUStats.OnlineCPUs)
	if ncpu == 0 {
		ncpu = float64(len(s.CPUStats.CPUUsage.PerCPU))
	}
	if cpuDelta > 0 && sysDelta > 0 && ncpu > 0 {
		cpuPct = (cpuDelta / sysDelta) * ncpu * 100
	}
	return cpuPct, mem, memLimit
}
