// Package metrics renders a vitals snapshot as Prometheus text-exposition
// format, using OpenTelemetry semantic-convention names where a clean one
// exists (system.cpu.utilization -> system_cpu_utilization, 0..1) and a
// vitals_ prefix for the value-add signals (per-model GPU offload, the doctor
// verdict). No OTel SDK dependency: the text format is emitted directly, which
// is what Grafana Agent / Alloy and Prometheus scrape natively.
package metrics

import (
	"fmt"
	"sort"
	"strings"

	"vitals/internal/diag"
	"vitals/internal/doctor"
)

type sample struct {
	name   string
	help   string
	typ    string // gauge | counter
	labels map[string]string
	value  float64
}

func esc(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// Render turns a snapshot + verdict into Prometheus exposition text.
func Render(s doctor.Snapshot, r diag.Report) string {
	var m []sample
	add := func(name, help, typ string, val float64, kv ...string) {
		lbl := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			lbl[kv[i]] = kv[i+1]
		}
		m = append(m, sample{name, help, typ, lbl, val})
	}

	add("system_cpu_utilization", "CPU utilisation, 0..1", "gauge", s.CPU.UsedPct/100)
	add("system_cpu_iowait_ratio", "Fraction of CPU time in I/O wait, 0..1", "gauge", s.CPU.IOWaitPct/100)
	if s.CPU.StealPct > 0 {
		add("system_cpu_steal_ratio", "Fraction of CPU time stolen by the hypervisor, 0..1", "gauge", s.CPU.StealPct/100)
	}
	if s.CPU.Load1 > 0 {
		add("system_cpu_load_average_1m", "1-minute load average", "gauge", s.CPU.Load1)
	}
	if s.CPU.Cores > 0 {
		add("system_cpu_logical_count", "Logical CPU count", "gauge", float64(s.CPU.Cores))
	}

	add("system_memory_utilization", "Physical memory utilisation, 0..1", "gauge", s.Memory.UsedPct/100)
	add("system_paging_utilization", "Swap utilisation, 0..1", "gauge", s.Memory.SwapUsedPct/100)
	add("system_paging_out_bytes_per_second", "Swap-out rate", "gauge", s.Memory.SwapOutPerSec)
	add("system_paging_in_bytes_per_second", "Swap-in rate", "gauge", s.Memory.SwapInPerSec)

	for _, d := range s.Disks {
		add("system_filesystem_utilization", "Filesystem utilisation, 0..1", "gauge", d.UsedPct/100, "mountpoint", d.Mount)
		add("system_filesystem_free_bytes", "Filesystem free space", "gauge", float64(d.FreeBytes), "mountpoint", d.Mount)
		if d.UtilPct > 0 {
			add("system_disk_io_time_ratio", "Device busy fraction, 0..1", "gauge", d.UtilPct/100, "mountpoint", d.Mount)
		}
	}

	for _, n := range s.Net {
		add("system_network_io_bytes_per_second", "Network throughput", "gauge", n.RxBytesPerSec, "device", n.Name, "direction", "receive")
		add("system_network_io_bytes_per_second", "Network throughput", "gauge", n.TxBytesPerSec, "device", n.Name, "direction", "transmit")
	}

	for i, g := range s.GPUs {
		gl := g.Name
		if gl == "" {
			gl = fmt.Sprintf("gpu%d", i)
		}
		add("vitals_gpu_utilization_ratio", "GPU utilisation, 0..1", "gauge", g.UtilPct/100, "gpu", gl)
		if g.VRAMTotal > 0 {
			add("vitals_gpu_memory_utilization_ratio", "GPU VRAM utilisation, 0..1", "gauge", float64(g.VRAMUsed)/float64(g.VRAMTotal), "gpu", gl)
		}
		if g.TempC > 0 {
			add("vitals_gpu_temperature_celsius", "GPU temperature", "gauge", g.TempC, "gpu", gl)
		}
	}

	for _, mdl := range s.LLM {
		add("vitals_llm_gpu_offload_ratio", "Fraction of an LLM offloaded to GPU, 0..1", "gauge", mdl.OffloadPct/100, "model", mdl.Name)
	}

	if s.Power.Percent > 0 {
		add("vitals_battery_charge_ratio", "Battery charge, 0..1", "gauge", s.Power.Percent/100)
		onBat := 0.0
		if s.Power.OnBattery {
			onBat = 1
		}
		add("vitals_battery_on_battery", "1 when running on battery power", "gauge", onBat)
	}

	add("vitals_verdict", "Overall health verdict: 0 ok, 1 warning, 2 critical", "gauge", float64(r.Worst().ExitCode()))
	counts := map[diag.Severity]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	for _, sev := range []diag.Severity{diag.OK, diag.Warn, diag.Critical} {
		add("vitals_findings", "Number of findings by severity", "gauge", float64(counts[sev]), "severity", sev.String())
	}

	return format(m)
}

func format(m []sample) string {
	// Stable output: group by metric name, HELP/TYPE once, samples sorted.
	sort.SliceStable(m, func(i, j int) bool {
		if m[i].name != m[j].name {
			return m[i].name < m[j].name
		}
		return labelKey(m[i].labels) < labelKey(m[j].labels)
	})
	var b strings.Builder
	seen := map[string]bool{}
	for _, s := range m {
		if !seen[s.name] {
			seen[s.name] = true
			fmt.Fprintf(&b, "# HELP %s %s\n", s.name, s.help)
			fmt.Fprintf(&b, "# TYPE %s %s\n", s.name, s.typ)
		}
		b.WriteString(s.name)
		if len(s.labels) > 0 {
			keys := make([]string, 0, len(s.labels))
			for k := range s.labels {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, `%s="%s"`, k, esc(s.labels[k]))
			}
			b.WriteByte('}')
		}
		fmt.Fprintf(&b, " %s\n", trimFloat(s.value))
	}
	return b.String()
}

func labelKey(l map[string]string) string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+l[k])
	}
	return strings.Join(parts, ",")
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.6f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
