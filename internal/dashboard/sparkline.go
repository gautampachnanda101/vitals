package dashboard

import (
	"fmt"
	"html/template"
	"strings"
)

// sparkline renders a series of values as a tiny inline SVG polyline —
// the Overview resource cards' trend strip (roadmap item 007). The
// domain is fixed by the caller (0..100 for a percentage) so a flat
// series near a real value doesn't get auto-scaled into looking
// dramatic. Returns "" for fewer than two points, so a card with no
// history just shows no sparkline rather than an empty box.
//
// severity picks the stroke colour, matching the card's bar/value:
// "" / "ok" -> accent, "warning" -> warn, "critical" -> crit.
func sparkline(vals []float64, dmin, dmax float64, severity string) template.HTML {
	if len(vals) < 2 {
		return ""
	}
	// cap the point count so a 2000-sample history file doesn't emit a
	// 2000-vertex polyline into every card; the most recent points are
	// the ones worth showing.
	const maxPts = 60
	if len(vals) > maxPts {
		vals = vals[len(vals)-maxPts:]
	}

	const w, h = 100.0, 24.0
	span := dmax - dmin
	if span <= 0 {
		span = 1
	}
	step := w / float64(len(vals)-1)

	var b strings.Builder
	for i, v := range vals {
		if v < dmin {
			v = dmin
		} else if v > dmax {
			v = dmax
		}
		x := float64(i) * step
		// SVG y grows downward — invert so a higher value sits higher.
		y := h - ((v-dmin)/span)*h
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}

	stroke := "var(--accent)"
	switch severity {
	case "warning":
		stroke = "var(--warn)"
	case "critical":
		stroke = "var(--crit)"
	}

	return template.HTML(fmt.Sprintf(
		`<svg class="spark" viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" aria-hidden="true">`+
			`<polyline fill="none" stroke="%s" stroke-width="1.5" vector-effect="non-scaling-stroke" points="%s"/></svg>`,
		w, h, stroke, b.String()))
}
