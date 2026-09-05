package dashboard

import (
	"fmt"
	"html/template"

	"vitals/internal/info"
	"vitals/internal/tools"
)

func init() {
	Register(Module{Slug: "system", NavLabel: "System", Group: "System", Icon: iconSystem, Order: 100, Available: Always, Render: renderSystem})
}

// renderSystem shows what `vitals info` reports (binary/machine/config)
// plus every companion tool's install status (internal/tools' own
// registry, `vitals tools install <name>`'s own catalog) — read-only.
// Installing a tool from here would be a new dashboard write action
// (running a real package-manager command) and deliberately isn't part
// of this page: per this repo's own standing rule that every destructive
// or system-mutating action needs its own confirm-gated design on both
// the CLI and the dashboard, that's a separate, separately-reviewed
// piece of work, not a button added in passing.
func renderSystem(ctx PageContext) string {
	r := info.Collect(ctx.Version)

	body := `<div class="sectiontitle">Machine</div><div class="card">`
	body += row("vitals version", r.Binary.Version)
	body += row("OS / architecture", r.Binary.OS+" / "+r.Binary.Arch)
	if r.Machine.Hostname != "" {
		body += row("Hostname", r.Machine.Hostname)
	}
	body += row("Config file", r.Config.Path)
	body += `</div>`

	body += `<div class="sectiontitle">` + configSectionTitleText(r.Config.Exists, len(r.Config.Overrides)) + `</div><div class="card">`
	body += configRow("disk_warn_percent", fmt.Sprintf("%.0f", r.Config.Active.DiskWarnPercent), r.Config.Overrides)
	body += configRow("disk_critical_percent", fmt.Sprintf("%.0f", r.Config.Active.DiskCriticalPercent), r.Config.Overrides)
	body += configRow("ram_warn_percent", fmt.Sprintf("%.0f", r.Config.Active.RAMWarnPercent), r.Config.Overrides)
	body += configRow("ram_high_percent", fmt.Sprintf("%.0f", r.Config.Active.RAMHighPercent), r.Config.Overrides)
	body += configRow("cpu_oversubscribe_multiplier", fmt.Sprintf("%.1f", r.Config.Active.CPUOversubscribeMult), r.Config.Overrides)
	if r.Config.Active.OllamaURL != "" {
		body += configRow("ollama_url", r.Config.Active.OllamaURL, r.Config.Overrides)
	}
	body += `</div>`

	installedCount := 0
	for _, t := range tools.Registry {
		if tools.Installed(t) {
			installedCount++
		}
	}
	body += fmt.Sprintf(`<div class="sectiontitle">Companion tools — %d of %d installed</div><div class="toolgrid">`, installedCount, len(tools.Registry))
	for _, t := range tools.Registry {
		body += toolCard(t, tools.Installed(t))
	}
	body += `</div>`
	return body
}

// configSectionTitleText mirrors info.Render's own status line, condensed
// to a section heading: "not created yet" / "defaults" / "N value(s)
// changed" — the same three states `vitals info`'s terminal view shows.
func configSectionTitleText(exists bool, overrideCount int) string {
	switch {
	case !exists:
		return "Config — not created yet"
	case overrideCount == 0:
		return "Config — in use, every value still at its default"
	case overrideCount == 1:
		return "Config — in use, 1 value changed from default"
	default:
		return fmt.Sprintf("Config — in use, %d values changed from default", overrideCount)
	}
}

var configRowTmpl = template.Must(template.New("configRow").Parse(
	`<div class="row"><span class="k">{{.Key}}</span><span>{{.Value}}&nbsp;&nbsp;<span class="pill {{if .Set}}ok{{end}}">{{if .Set}}set in file{{else}}built-in default{{end}}</span></span></div>`))

func configRow(key, value string, overrides []string) string {
	set := false
	for _, o := range overrides {
		if o == key {
			set = true
			break
		}
	}
	return mustExecute(configRowTmpl, struct {
		Key, Value string
		Set        bool
	}{key, value, set})
}

var toolCardTmpl = template.Must(template.New("toolCard").Parse(
	`<div class="toolcard"><div class="toolhead"><span class="toolname mono">{{.Name}}</span>` +
		`<span class="pill {{if .Installed}}ok{{end}}">{{if .Installed}}installed{{else}}not installed{{end}}</span></div>` +
		`<div class="toolcat">{{.Category}}</div><div class="tooldesc">{{.Description}}</div></div>`))

func toolCard(t tools.Tool, installedNow bool) string {
	return mustExecute(toolCardTmpl, struct {
		Name, Category, Description string
		Installed                   bool
	}{t.Name, t.Category, t.Description, installedNow})
}
