package doctor

import (
	_ "embed"
	"reflect"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is the semantic version of the `--json` payload shape — the
// JSONEnvelope: schema_version, timestamp, verdict, exit_code, findings[],
// snapshot{}. Bump the MINOR for an additive field, the MAJOR for a rename or
// removal, and update testdata/schema_fields.golden in the same commit — the
// contract test fails otherwise.
const SchemaVersion = "1.4.0"

//go:embed schema.json
var schemaJSON []byte

// Schema returns the embedded JSON Schema for the `--json` payload.
func Schema() []byte { return schemaJSON }

// SchemaFields returns every JSON field path in the payload, sorted. It walks
// the JSONEnvelope and the Snapshot struct reflectively so the contract test
// can detect any field added or removed without a schema bump.
func SchemaFields() []string {
	var out []string
	jsonFieldPaths(reflect.TypeOf(JSONEnvelope{}), "", &out)
	sort.Strings(out)
	// de-dup (repeated element types under different slices collapse to the
	// same dotted path, which is what we want)
	uniq := out[:0]
	var prev string
	for _, p := range out {
		if p != prev {
			uniq = append(uniq, p)
			prev = p
		}
	}
	return uniq
}

func jsonFieldPaths(t reflect.Type, prefix string, out *[]string) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft != reflect.TypeOf(time.Time{}) {
			jsonFieldPaths(ft, path, out)
		} else {
			*out = append(*out, path)
		}
	}
}
