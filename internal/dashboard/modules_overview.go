package dashboard

import (
	"fmt"

	"vitals/internal/doctor"
)

func init() {
	Register(Module{Slug: "", NavLabel: "Overview", Order: 0, Available: Always, Render: renderOverview})
}

// renderOverview is the cross-resource verdict — the same correlation
// `vitals doctor` prints in a terminal, as a page.
func renderOverview(ctx PageContext) string {
	headline := "Healthy — nothing needs attention"
	if len(ctx.Report.Findings) > 0 {
		headline = ctx.Report.SortedBySeverity()[0].Title
	}
	body := verdictBanner(headline, summaryLine(ctx.Snapshot), ctx.Report.Worst())
	body += `<div class="card">` + findingsList(ctx.Report.SortedBySeverity()) + `</div>`
	return body
}

// summaryLine mirrors doctor's own terminal summary — the at-a-glance
// numbers a verdict has to be checkable against, not just asserted.
func summaryLine(s doctor.Snapshot) string {
	out := fmt.Sprintf("cpu %.0f%%   mem %.0f%%", s.CPU.UsedPct, s.Memory.UsedPct)
	if d, ok := fullestDisk(s.Disks); ok {
		out += fmt.Sprintf("   disk %.0f%% (%s)", d.UsedPct, d.Mount)
	}
	if s.Power.OnBattery && s.Power.Percent > 0 {
		out += fmt.Sprintf("   battery %.0f%%", s.Power.Percent)
	}
	return out
}

// fullestDisk returns the real mount with the highest usage — same
// selection doctor.Run's own summary line uses.
func fullestDisk(ds []doctor.Disk) (doctor.Disk, bool) {
	var best doctor.Disk
	found := false
	for _, d := range ds {
		if !found || d.UsedPct > best.UsedPct {
			best, found = d, true
		}
	}
	return best, found
}
