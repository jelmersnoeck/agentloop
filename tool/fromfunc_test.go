package tool

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jelmersnoeck/agentloop/llm"
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

type echoArgs struct {
	Message string `json:"message" jsonschema:"required,description=text to echo"`
	Times   int    `json:"times"`
}

func TestFromFuncDispatch(t *testing.T) {
	var seen echoArgs
	tool := FromFunc("echo", "echoes a message",
		func(ctx context.Context, a echoArgs, tctx Context) (Result, error) {
			seen = a
			return TextResult(a.Message), nil
		})

	if tool.Name() != "echo" || tool.Description() != "echoes a message" {
		t.Fatalf("name/desc wrong: %q / %q", tool.Name(), tool.Description())
	}
	// Schema is generated from echoArgs.
	if !json.Valid(tool.Schema()) {
		t.Fatalf("schema not valid JSON: %s", tool.Schema())
	}

	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"message":"hi","times":3}`), Context{})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Message != "hi" || seen.Times != 3 {
		t.Fatalf("handler received %+v, want {hi 3}", seen)
	}
	if got := res.Content[0].(llm.TextBlock).Text; got != "hi" {
		t.Fatalf("result = %q, want hi", got)
	}
}

func TestFromFuncBadInputIsErrorResult(t *testing.T) {
	tool := FromFunc("echo", "",
		func(ctx context.Context, a echoArgs, tctx Context) (Result, error) {
			return TextResult("should not run"), nil
		})
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"times": "not a number"}`), Context{})
	if err != nil {
		t.Fatalf("bad input should not be a Go error, got %v", err)
	}
	if !res.IsError {
		t.Fatal("bad input should yield an error Result the model can react to")
	}
}
