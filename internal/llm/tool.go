package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

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
	return Tool{
		Name:        name,
		Description: description,
		Schema:      schemaFor[In](),
		Execute: func(ctx context.Context, input json.RawMessage) (Result, error) {
			var in In
			if len(input) > 0 {
				if err := json.Unmarshal(input, &in); err != nil {
					return Result{}, fmt.Errorf("unmarshal input for tool %q: %w", name, err)
				}
			}
			return fn(ctx, in)
		},
	}
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
