package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"vitals/internal/containers"
	"vitals/internal/ui"
)

// containerStatsSampler is the opt-in per-container CPU/mem sample
// `vitals containers` adds on top of the snapshot's cheap list — pulled
// out so a test drives it without a daemon.
var containerStatsSampler = func(r containers.Report) containers.Report {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return containers.Sample(ctx, r)
}

// RunContainers is `vitals containers` — the local container runtime /
// Kubernetes picture plus only its findings. Unlike a snapshot's cheap
// container list, this also samples per-container CPU/memory (a ~1s
// server-side call per running container, run concurrently). Nothing but
// the empty "no runtime" line when none is reachable.
func RunContainers(opts RunOptions) int {
	snap := Collect(Options{OllamaURL: opts.OllamaURL})
	snap.Containers = containerStatsSampler(snap.Containers)
	report := AnalyzeResource(snap, "containers")

	if err := maybeWriteOutput(opts.Output, snap, report); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write --output file: %v\n", err)
	}
	if opts.Quiet {
		return report.ExitCode()
	}
	if opts.CI {
		fmt.Println(renderCI(report))
		return report.ExitCode()
	}
	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Resource string `json:"resource"`
			JSONEnvelope
		}{"containers", JSONReport(snap, report)})
		return report.ExitCode()
	}

	ui.Header("CONTAINERS")
	renderContainers(snap.Containers, opts.Verbose)
	if len(report.Findings) == 0 {
		fmt.Println()
		if snap.Containers.Reachable {
			ui.Okf("no container issues detected")
		}
		return 0
	}
	fmt.Println()
	PrintFindings(report.SortedBySeverity(), false)
	return report.ExitCode()
}

func renderContainers(rep containers.Report, verbose bool) {
	if rep.Runtime == "" {
		fmt.Println(ui.Key("  no local container runtime or Kubernetes detected"))
		return
	}
	if !rep.Reachable {
		fmt.Printf("  %s\n", ui.Key(rep.Runtime+": "+nz2(rep.Note, "not reachable")))
		return
	}
	run, stopped, unhealthy := rep.Counts()
	line := fmt.Sprintf("%s via %s — %d running, %d stopped", rep.Runtime, nz2(rep.Endpoint, "local"), run, stopped)
	if unhealthy > 0 {
		line += fmt.Sprintf(", %s", ui.Grade(fmt.Sprintf("%d unhealthy", unhealthy), float64(unhealthy), 1, 1))
	}
	if rep.VMTotalBytes > 0 {
		line += fmt.Sprintf("  (runtime VM: %s RAM)", ui.HumanBytes(int64(rep.VMTotalBytes)))
	}
	row("runtime", line)

	if len(rep.Containers) == 0 {
		fmt.Println(ui.Key("  (no containers)"))
		return
	}
	fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-24s %-10s %6s %22s  %s", "NAME", "STATE", "CPU", "MEM", "STATUS")))
	shown := 0
	for _, c := range rep.Containers {
		if !verbose && !c.Running() && shown >= 15 {
			continue
		}
		cpu, mem := "-", "-"
		if c.CPUPct > 0 {
			cpu = fmt.Sprintf("%.0f%%", c.CPUPct)
		}
		if c.MemBytes > 0 {
			mem = ui.HumanBytes(int64(c.MemBytes))
			if c.MemLimitBytes > 0 {
				mem += "/" + ui.HumanBytes(int64(c.MemLimitBytes))
			}
		}
		status := c.Status
		if c.WaitingOn != "" {
			status = c.WaitingOn
		}
		if c.OOMKilled {
			status += " OOMKilled"
		}
		name := c.Name
		if c.Namespace != "" {
			name = c.Namespace + "/" + name
		}
		fmt.Printf("  %-24s %-10s %6s %22s  %s\n",
			ui.Truncate(name, 24), ui.Truncate(strings.ToLower(c.State), 10), cpu, mem, ui.Truncate(status, 40))
		shown++
	}
}

func nz2(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
