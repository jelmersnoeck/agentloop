package tool

import (
	"encoding/json"
	"reflect"
	"testing"
)

type schemaArgs struct {
	Path    string   `json:"path" jsonschema:"required,description=File to read"`
	Offset  int      `json:"offset" jsonschema:"description=Start line, 1-indexed"`
	Verbose bool     `json:"verbose"`
	Tags    []string `json:"tags"`
	Ignored string   `json:"-"`
}

func TestSchemaForBuildsObject(t *testing.T) {
	raw := schemaFor(reflect.TypeOf(schemaArgs{}))

	var got struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("schema is not valid JSON: %v\n%s", err, raw)
	}

	if got.Type != "object" {
		t.Fatalf("type = %q, want object", got.Type)
	}
	// Ignored field must be absent.
	if _, ok := got.Properties["Ignored"]; ok {
		t.Fatal("json:\"-\" field should be omitted")
	}
	if _, ok := got.Properties["ignored"]; ok {
		t.Fatal("ignored field leaked into schema")
	}
	// Required contains only path.
	if len(got.Required) != 1 || got.Required[0] != "path" {
		t.Fatalf("required = %v, want [path]", got.Required)
	}

	// Field types.
	assertProp(t, got.Properties["path"], "string", "File to read")
	// Description with a comma survives because description is the last directive.
	assertProp(t, got.Properties["offset"], "integer", "Start line, 1-indexed")
	assertProp(t, got.Properties["verbose"], "boolean", "")

	// Array carries an items type.
	var tagsProp struct {
		Type  string          `json:"type"`
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(got.Properties["tags"], &tagsProp); err != nil {
		t.Fatal(err)
	}
	if tagsProp.Type != "array" {
		t.Fatalf("tags type = %q, want array", tagsProp.Type)
	}
	var items struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(tagsProp.Items, &items); err != nil {
		t.Fatal(err)
	}
	if items.Type != "string" {
		t.Fatalf("tags items type = %q, want string", items.Type)
	}
}

func assertProp(t *testing.T, raw json.RawMessage, wantType, wantDesc string) {
	t.Helper()
	var p struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("bad property JSON %s: %v", raw, err)
	}
	if p.Type != wantType {
		t.Fatalf("type = %q, want %q", p.Type, wantType)
	}
	if p.Description != wantDesc {
		t.Fatalf("description = %q, want %q", p.Description, wantDesc)
	}
}
