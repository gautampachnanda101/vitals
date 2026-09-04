// Package mcp is a minimal Model Context Protocol server over stdio, so an MCP
// client (Claude Code, Claude Desktop, ...) can call vitals' diagnostics while
// it works. Newline-delimited JSON-RPC 2.0; no SDK dependency. The dispatch is
// pure (handle) and unit-tested; tools call the doctor engine.
package mcp

import (
	"bufio"
	"encoding/json"
	"io"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/gpu"
	"vitals/internal/llm"
)

const protocolVersion = "2024-11-05"

// Options configures the server.
type Options struct {
	OllamaURL string
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	name        string
	description string
	run         func(o Options) (string, error)
}

// deps is the live diagnostic surface each tool's run closure reads from,
// pulled out so a test can substitute fakes and drive a real tools/call
// through handle()/Serve() end to end — same shape as internal/tools'/
// internal/dupes'/internal/metrics'/internal/guide's deps structs.
// defaultDeps wires the real calls; production always goes through it via
// Serve. This is the "accept doctor.Assess/etc. as injectable
// dependencies of each Handler closure" option from item 009's own open
// design question for this package, chosen over unit-testing the JSON-RPC
// plumbing against a fake result only: it proves the exact same dispatch
// path (tools/call -> find tool -> run -> toolText) a real MCP client
// exercises, not a parallel one.
type deps struct {
	assess        func(doctor.RunOptions) (doctor.Snapshot, diag.Report)
	ollamaModels  func(ollamaURL string) []llm.ModelState
	scanProcesses func() []llm.ProcSnapshot
	gpuProbe      func() []gpu.Device
}

var defaultDeps = deps{
	assess:        doctor.Assess,
	ollamaModels:  llm.OllamaModels,
	scanProcesses: llm.ScanProcesses,
	gpuProbe:      gpu.Probe,
}

func tools(d deps) []toolDef {
	return []toolDef{
		{
			name:        "system_health",
			description: "Full machine health check: the ranked findings (what is wrong and the fix) plus the overall verdict and exit code. Same schema as `vitals doctor --json`.",
			run: func(o Options) (string, error) {
				snap, rep := d.assess(doctor.RunOptions{OllamaURL: o.OllamaURL})
				return jsonString(doctor.JSONReport(snap, rep))
			},
		},
		{
			name:        "diagnose_bottleneck",
			description: "The single most severe finding right now — the current bottleneck and its fix — or a healthy verdict.",
			run: func(o Options) (string, error) {
				_, rep := d.assess(doctor.RunOptions{OllamaURL: o.OllamaURL})
				sorted := rep.SortedBySeverity()
				if len(sorted) == 0 || sorted[0].Severity == diag.OK {
					return jsonString(map[string]any{"bottleneck": nil, "verdict": "ok"})
				}
				return jsonString(map[string]any{"bottleneck": sorted[0], "verdict": rep.Worst().String()})
			},
		},
		{
			name:        "llm_status",
			description: "Local and cloud LLM endpoints, loaded models, and per-model GPU offload percentage.",
			run: func(o Options) (string, error) {
				return jsonString(map[string]any{
					"models":    d.ollamaModels(o.OllamaURL),
					"processes": d.scanProcesses(),
				})
			},
		},
		{
			name:        "gpu_status",
			description: "Per-GPU VRAM, utilisation, temperature, power and the processes holding VRAM.",
			run: func(o Options) (string, error) {
				return jsonString(map[string]any{"devices": d.gpuProbe()})
			},
		},
	}
}

// Serve runs the JSON-RPC loop until in is exhausted.
func Serve(in io.Reader, out io.Writer, opts Options) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{-32700, "parse error"}})
			continue
		}
		resp, skip := handle(req, opts, defaultDeps)
		if skip {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

// handle dispatches one request. skip is true for notifications (no reply).
func handle(req request, opts Options, d deps) (response, bool) {
	base := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "vitals", "version": "dev"},
		}
		return base, false

	case "notifications/initialized", "notifications/cancelled":
		return response{}, true

	case "ping":
		base.Result = map[string]any{}
		return base, false

	case "tools/list":
		var list []map[string]any
		for _, t := range tools(d) {
			list = append(list, map[string]any{
				"name":        t.name,
				"description": t.description,
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			})
		}
		base.Result = map[string]any{"tools": list}
		return base, false

	case "tools/call":
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &p)
		for _, t := range tools(d) {
			if t.name == p.Name {
				text, err := t.run(opts)
				if err != nil {
					base.Result = toolText("error: "+err.Error(), true)
					return base, false
				}
				base.Result = toolText(text, false)
				return base, false
			}
		}
		base.Error = &rpcError{-32602, "unknown tool: " + p.Name}
		return base, false

	default:
		base.Error = &rpcError{-32601, "method not found: " + req.Method}
		return base, false
	}
}

func toolText(s string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": s}},
		"isError": isErr,
	}
}

func jsonString(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ToolNames is exposed for docs/tests.
func ToolNames() []string {
	var n []string
	for _, t := range tools(defaultDeps) {
		n = append(n, t.name)
	}
	return n
}
