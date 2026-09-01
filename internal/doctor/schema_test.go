package doctor

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goldenPath = "testdata/schema_fields.golden"

// TestSchemaFieldsContract fails whenever the --json payload gains or loses a
// field without the golden being updated. Regenerate with:
//
//	go test ./internal/doctor -run TestSchemaFieldsContract -update
var update = flag.Bool("update", false, "update golden files")

func TestSchemaFieldsContract(t *testing.T) {
	got := strings.Join(SchemaFields(), "\n") + "\n"

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v — run with -update to create it", err)
	}
	if got != string(want) {
		t.Errorf("the --json payload shape changed.\n\n"+
			"If this is intentional: update SchemaVersion in schema.go, then\n"+
			"  go test ./internal/doctor -run TestSchemaFieldsContract -update\n\n"+
			"got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmbeddedSchemaIsValidJSON(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(Schema(), &m); err != nil {
		t.Fatalf("schema.json does not parse: %v", err)
	}
	if m["$schema"] == nil || m["title"] == nil {
		t.Errorf("schema.json missing $schema / title: %v", m)
	}
	if v, _ := m["x-schema-version"].(string); v != SchemaVersion {
		t.Errorf("schema.json x-schema-version = %q, want %q (keep it in sync with SchemaVersion)", v, SchemaVersion)
	}
}

func TestEmbeddedSchemaMentionsEveryField(t *testing.T) {
	body := string(Schema())
	for _, path := range SchemaFields() {
		leaf := path
		if i := strings.LastIndex(path, "."); i >= 0 {
			leaf = path[i+1:]
		}
		if !strings.Contains(body, `"`+leaf+`"`) {
			t.Errorf("schema.json does not mention field %q (from path %q)", leaf, path)
		}
	}
}

func TestJSONReportCarriesSchemaVersion(t *testing.T) {
	env := JSONReport(Snapshot{}, Analyze(Snapshot{}))
	if env.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, SchemaVersion)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"schema_version":"`+SchemaVersion+`"`) {
		t.Errorf("marshalled envelope missing schema_version: %s", b)
	}
}
