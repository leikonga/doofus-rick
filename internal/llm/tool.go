package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

// ExpandedStruct is left off: invopop/jsonschema panics on it for anonymous struct types.
var reflector = &jsonschema.Reflector{
	DoNotReference:             true,
	RequiredFromJSONSchemaTags: true,
}

// NewTool derives a JSON Schema from In's struct tags, so each tool declares
// its input shape exactly once.
func NewTool[In any](name, description string, fn func(context.Context, In) (Result, error)) Tool {
	schema := schemaFor[In]()
	params := schemaPropertyNames(schema)
	return Tool{
		Name:        name,
		Description: description,
		Schema:      schema,
		Execute: func(ctx context.Context, input json.RawMessage) (Result, error) {
			var in In
			if len(input) > 0 {
				dec := json.NewDecoder(bytes.NewReader(input))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&in); err != nil {
					return Result{}, fmt.Errorf("invalid input for tool %q: %w; expected parameters: %s", name, err, strings.Join(params, ", "))
				}
			}
			return fn(ctx, in)
		},
	}
}

func schemaPropertyNames(schema map[string]any) []string {
	props, _ := schema["properties"].(map[string]any)
	names := slices.Collect(maps.Keys(props))
	slices.Sort(names)
	return names
}

func schemaFor[In any]() map[string]any {
	var zero In
	s := reflector.ReflectFromType(reflect.TypeOf(zero))
	data, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("llm: marshal schema for %T: %v", zero, err))
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(fmt.Sprintf("llm: unmarshal schema for %T: %v", zero, err))
	}
	delete(m, "$schema")
	delete(m, "$id")
	return m
}
