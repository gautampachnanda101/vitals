package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func call(t *testing.T, method, params string) response {
	t.Helper()
	req := request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
	if params != "" {
		req.Params = json.RawMessage(params)
	}
	resp, skip := handle(req, Options{})
	if skip {
		t.Fatalf("method %q was treated as a notification", method)
	}
	return resp
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
	_, skip := handle(request{JSONRPC: "2.0", Method: "notifications/initialized"}, Options{})
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
