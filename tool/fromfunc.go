package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// schemaFor builds a JSON Schema object for a struct type by reflecting its
// exported fields and their `json` / `jsonschema` tags.
func schemaFor(t reflect.Type) json.RawMessage {
	if t == nil || t.Kind() != reflect.Struct {
		return json.RawMessage(`{"type":"object"}`)
	}
	props := make(map[string]json.RawMessage)
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, omit := jsonFieldName(f)
		if omit {
			continue
		}
		prop, req := propertyFor(f)
		props[name] = prop
		if req {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	schema := map[string]any{
		"type":       "object",
		"properties": props, // encoding/json marshals map keys sorted
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	b, _ := json.Marshal(schema)
	return b
}

// jsonFieldName resolves the JSON key for a field. It returns omit=true for
// json:"-".
func jsonFieldName(f reflect.StructField) (name string, omit bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	name = f.Name
	if tag != "" {
		if first := strings.Split(tag, ",")[0]; first != "" {
			name = first
		}
	}
	return name, false
}

// propertyFor builds the schema fragment for a single field and reports whether
// it is required. The `jsonschema` tag supports `required` and a trailing
// `description=...` (which may contain commas because it is parsed as the
// remainder of the tag).
func propertyFor(f reflect.StructField) (json.RawMessage, bool) {
	prop := map[string]any{"type": jsonType(f.Type)}
	if prop["type"] == "array" {
		prop["items"] = map[string]any{"type": jsonType(f.Type.Elem())}
	}

	required := false
	tag := f.Tag.Get("jsonschema")
	if tag != "" {
		if idx := strings.Index(tag, "description="); idx >= 0 {
			prop["description"] = tag[idx+len("description="):]
			tag = tag[:idx] // directives before description
		}
		for _, part := range strings.Split(tag, ",") {
			if strings.TrimSpace(part) == "required" {
				required = true
			}
		}
	}

	b, _ := json.Marshal(prop)
	return b, required
}

// jsonType maps a Go type to a JSON Schema type keyword.
func jsonType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return "string"
	}
}

// FromFunc builds a Tool from a typed handler. The args type T is reflected to
// generate the input JSON Schema and to unmarshal the model's input before
// calling fn. T should be a struct.
func FromFunc[T any](name, description string, fn func(ctx context.Context, args T, tctx Context) (Result, error)) Tool {
	var zero T
	return &funcTool[T]{
		name:        name,
		description: description,
		schema:      schemaFor(reflect.TypeOf(zero)),
		fn:          fn,
	}
}

type funcTool[T any] struct {
	name        string
	description string
	schema      json.RawMessage
	fn          func(ctx context.Context, args T, tctx Context) (Result, error)
}

func (t *funcTool[T]) Name() string            { return t.name }
func (t *funcTool[T]) Description() string     { return t.description }
func (t *funcTool[T]) Schema() json.RawMessage { return t.schema }

func (t *funcTool[T]) Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error) {
	var args T
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			// Surface as an error Result (not a Go error) so the model can read
			// the message and retry with corrected arguments.
			return ErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
		}
	}
	return t.fn(ctx, args, tctx)
}
