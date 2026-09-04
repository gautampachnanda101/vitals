package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/gpu"
	"vitals/internal/llm"
)

// fakeDeps is a deps struct wired entirely to canned data — no real
// doctor.Assess/llm/gpu call, so tests can drive a real tools/call through
// handle() end to end without ever touching live system state.
func fakeDeps() deps {
	return deps{
		assess: func(doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, diag.Report{Findings: []diag.Finding{
				{Severity: diag.Warn, Title: "fake finding", Fixes: []string{"fake fix"}},
			}}
		},
		ollamaModels:  func(string) []llm.ModelState { return []llm.ModelState{{Name: "fake-model"}} },
		scanProcesses: func() []llm.ProcSnapshot { return nil },
		gpuProbe:      func() []gpu.Device { return []gpu.Device{{Name: "fake-gpu"}} },
	}
}

func call(t *testing.T, method, params string) response {
	t.Helper()
	req := request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
	if params != "" {
		req.Params = json.RawMessage(params)
	}
	resp, skip := handle(req, Options{}, fakeDeps())
	if skip {
		t.Fatalf("method %q was treated as a notification", method)
	}
	return resp
}

func TestToolText(t *testing.T) {
	got := toolText("hello", false)
	content, _ := got["content"].([]map[string]any)
	if len(content) != 1 || content[0]["text"] != "hello" || content[0]["type"] != "text" {
		t.Errorf("toolText content = %+v", content)
	}
	if got["isError"] != false {
		t.Errorf("toolText isError = %v, want false", got["isError"])
	}
	if errGot := toolText("boom", true); errGot["isError"] != true {
		t.Errorf("toolText(isErr=true) isError = %v, want true", errGot["isError"])
	}
}

func TestJSONString(t *testing.T) {
	got, err := jsonString(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("jsonString: %v", err)
	}
	if !strings.Contains(got, `"a": 1`) {
		t.Errorf("jsonString = %q, want indented JSON containing \"a\": 1", got)
	}
	if _, err := jsonString(make(chan int)); err == nil {
		t.Error("jsonString(unmarshalable) should return an error")
	}
}

func TestToolNamesListsEveryRegisteredTool(t *testing.T) {
	names := ToolNames()
	for _, want := range []string{"system_health", "diagnose_bottleneck", "llm_status", "gpu_status"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ToolNames() = %v, missing %q", names, want)
		}
	}
}

func TestInitialize(t *testing.T) {
	r := call(t, "initialize", "")
	if r.Error != nil {
		t.Fatalf("initialize errored: %+v", r.Error)
	}
	m := r.Result.(map[string]any)
	if m["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", m["protocolVersion"])
	}
	if _, ok := m["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("no tools capability advertised: %+v", m)
	}
}

func TestNotificationsAreSilent(t *testing.T) {
	_, skip := handle(request{JSONRPC: "2.0", Method: "notifications/initialized"}, Options{}, fakeDeps())
	if !skip {
		t.Error("notifications/initialized should produce no reply")
	}
}

func TestToolsList(t *testing.T) {
	r := call(t, "tools/list", "")
	tools := r.Result.(map[string]any)["tools"].([]map[string]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl["name"].(string)] = true
		if tl["description"] == "" || tl["inputSchema"] == nil {
			t.Errorf("tool %v is missing description or schema", tl["name"])
		}
	}
	for _, want := range []string{"system_health", "diagnose_bottleneck", "llm_status", "gpu_status"} {
		if !names[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}
}

func TestToolsCallSystemHealthReturnsTheDoctorJSONReport(t *testing.T) {
	r := call(t, "tools/call", `{"name":"system_health"}`)
	if r.Error != nil {
		t.Fatalf("system_health errored: %+v", r.Error)
	}
	text := toolCallText(t, r)
	if !strings.Contains(text, "fake finding") || !strings.Contains(text, "fake fix") {
		t.Errorf("system_health should carry the fake assess() finding, got: %s", text)
	}
}

func TestToolsCallDiagnoseBottleneckReturnsTheWorstFinding(t *testing.T) {
	r := call(t, "tools/call", `{"name":"diagnose_bottleneck"}`)
	if r.Error != nil {
		t.Fatalf("diagnose_bottleneck errored: %+v", r.Error)
	}
	text := toolCallText(t, r)
	if !strings.Contains(text, "fake finding") || !strings.Contains(text, `"verdict": "warning"`) {
		t.Errorf("diagnose_bottleneck should report the fake finding as the bottleneck, got: %s", text)
	}
}

func TestToolsCallDiagnoseBottleneckReportsOKWhenHealthy(t *testing.T) {
	d := fakeDeps()
	d.assess = func(doctor.RunOptions) (doctor.Snapshot, diag.Report) { return doctor.Snapshot{}, diag.Report{} }
	resp, _ := handle(request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: json.RawMessage(`{"name":"diagnose_bottleneck"}`)}, Options{}, d)
	text := toolCallText(t, resp)
	if !strings.Contains(text, `"bottleneck": null`) || !strings.Contains(text, `"verdict": "ok"`) {
		t.Errorf("a healthy report should give a null bottleneck and verdict ok, got: %s", text)
	}
}

func TestToolsCallLLMStatusReturnsModelsAndProcesses(t *testing.T) {
	r := call(t, "tools/call", `{"name":"llm_status"}`)
	if r.Error != nil {
		t.Fatalf("llm_status errored: %+v", r.Error)
	}
	text := toolCallText(t, r)
	if !strings.Contains(text, "fake-model") {
		t.Errorf("llm_status should carry the fake ollamaModels() result, got: %s", text)
	}
}

func TestToolsCallGPUStatusReturnsDevices(t *testing.T) {
	r := call(t, "tools/call", `{"name":"gpu_status"}`)
	if r.Error != nil {
		t.Fatalf("gpu_status errored: %+v", r.Error)
	}
	text := toolCallText(t, r)
	if !strings.Contains(text, "fake-gpu") {
		t.Errorf("gpu_status should carry the fake gpuProbe() result, got: %s", text)
	}
}

// toolCallText extracts the text content jsonString/toolText wrapped a
// tools/call result in.
func toolCallText(t *testing.T, r response) string {
	t.Helper()
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("tools/call result is not a map: %+v", r.Result)
	}
	content, ok := m["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("tools/call result has no single content entry: %+v", m)
	}
	text, _ := content[0]["text"].(string)
	return text
}

func TestToolsCallUnknown(t *testing.T) {
	r := call(t, "tools/call", `{"name":"nope"}`)
	if r.Error == nil || r.Error.Code != -32602 {
		t.Errorf("unknown tool should give -32602, got %+v", r.Error)
	}
}

func TestUnknownMethod(t *testing.T) {
	r := call(t, "resources/list", "")
	if r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("unknown method should give -32601, got %+v", r.Error)
	}
}

func TestServeRoundTrip(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if err := Serve(in, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 responses (init + tools/list), got %d:\n%s", len(lines), out.String())
	}
	var initResp response
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil || initResp.Error != nil {
		t.Errorf("bad initialize response: %s", lines[0])
	}
}

func TestServeSkipsBlankLinesAndRepliesParseErrorToMalformedJSON(t *testing.T) {
	in := strings.NewReader("\n" +
		`not json at all` + "\n" +
		`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var out bytes.Buffer
	if err := Serve(in, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 responses (parse-error + ping), got %d:\n%s", len(lines), out.String())
	}
	var parseErrResp response
	if err := json.Unmarshal([]byte(lines[0]), &parseErrResp); err != nil {
		t.Fatalf("first response isn't valid JSON: %s", lines[0])
	}
	if parseErrResp.Error == nil || parseErrResp.Error.Code != -32700 {
		t.Errorf("malformed input should get a -32700 parse error, got: %+v", parseErrResp.Error)
	}
}

func TestPing(t *testing.T) {
	r := call(t, "ping", "")
	if r.Error != nil {
		t.Fatalf("ping errored: %+v", r.Error)
	}
	if _, ok := r.Result.(map[string]any); !ok {
		t.Errorf("ping result = %+v, want an empty object", r.Result)
	}
}
