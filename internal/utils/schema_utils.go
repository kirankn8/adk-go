// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"

	"google.golang.org/genai"
)

// ShallowCopyMap returns a shallow copy of m (new map, same value references).
func ShallowCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// normalizedSchemaType matches matchType: schema.Type may be lower/mixed case from APIs.
func normalizedSchemaType(t genai.Type) genai.Type {
	return genai.Type(strings.ToUpper(string(t)))
}

// CoerceFlexibleOutputArgs normalizes common LLM mistakes before ValidateMapOnSchema:
//   - schema string + model sent []any / []string → join with newlines
//   - schema array of strings + model sent one string → split on newlines or wrap as single element
//
// It mutates m in place. Unknown or already-valid shapes are left unchanged.
func CoerceFlexibleOutputArgs(m map[string]any, schema *genai.Schema) {
	if m == nil || schema == nil || schema.Properties == nil {
		return
	}
	for key, propSchema := range schema.Properties {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch normalizedSchemaType(propSchema.Type) {
		case genai.TypeString:
			if _, isStr := v.(string); isStr {
				continue
			}
			if s, ok := joinSliceAsNewlines(v); ok {
				m[key] = s
			}
		case genai.TypeArray:
			if propSchema.Items == nil || normalizedSchemaType(propSchema.Items.Type) != genai.TypeString {
				continue
			}
			if matchSliceOfStrings(v) {
				continue
			}
			if s, ok := v.(string); ok {
				m[key] = stringToStringSliceItems(s)
			}
		}
	}
}

func joinSliceAsNewlines(v any) (string, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < rv.Len(); i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprint(&b, rv.Index(i).Interface())
	}
	return b.String(), true
}

func matchSliceOfStrings(v any) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if _, ok := rv.Index(i).Interface().(string); !ok {
			return false
		}
	}
	return true
}

func stringToStringSliceItems(s string) []any {
	s = strings.TrimSpace(s)
	if s == "" {
		return []any{}
	}
	if strings.Contains(s, "\n") {
		var items []any
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				items = append(items, line)
			}
		}
		return items
	}
	return []any{s}
}

// matchType checks if the value matches the schema type.
func matchType(value any, schema *genai.Schema, isInput bool) (bool, error) {
	if schema == nil {
		return false, fmt.Errorf("schema is nil")
	}

	if value == nil {
		return false, nil
	}

	// Convert type to upper case to match the type in the schema.
	switch genai.Type(strings.ToUpper(string(schema.Type))) {
	case genai.TypeString:
		_, ok := value.(string)
		return ok, nil
	case genai.TypeInteger:
		f, ok := value.(float64)
		if !ok {
			return false, nil
		}
		return f == math.Trunc(f), nil
	case genai.TypeBoolean:
		_, ok := value.(bool)
		return ok, nil
	case genai.TypeNumber:
		_, ok := value.(float64)
		return ok, nil
	case genai.TypeArray:
		val := reflect.ValueOf(value)
		if val.Kind() != reflect.Slice {
			return false, nil
		}
		if schema.Items == nil {
			return false, fmt.Errorf("array schema missing items definition")
		}
		for i := 0; i < val.Len(); i++ {
			ok, err := matchType(val.Index(i).Interface(), schema.Items, isInput)
			if err != nil {
				return false, fmt.Errorf("array item %d: %w", i, err)
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	case genai.TypeObject:
		obj, ok := value.(map[string]any)
		if !ok {
			return false, nil
		}
		err := ValidateMapOnSchema(obj, schema, isInput)
		return err == nil, err
	default:
		return false, fmt.Errorf("unsupported type: %s", schema.Type)
	}
}

// ValidateMapOnSchema validates a map against a schema.
func ValidateMapOnSchema(args map[string]any, schema *genai.Schema, isInput bool) error {
	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	properties := schema.Properties
	if properties == nil {
		properties = make(map[string]*genai.Schema)
	}

	argType := "input"
	if !isInput {
		argType = "output"
	}

	for key, value := range args {
		propSchema, exists := properties[key]
		if !exists {
			// Note: OpenAPI schemas can allow additional properties. This implementation assumes strictness.
			return fmt.Errorf("%s arg: '%q' does not exist in schema properties", argType, key)
		}
		ok, err := matchType(value, propSchema, isInput)
		if err != nil {
			return fmt.Errorf("%s arg: '%q' validation failed: %w", argType, key, err)
		}
		if !ok {
			return fmt.Errorf("%s arg: '%q' type mismatch, expected schema type %s, got value %v of type %T", argType, key, propSchema.Type, value, value)
		}
	}

	for _, requiredKey := range schema.Required {
		if _, exists := args[requiredKey]; !exists {
			return fmt.Errorf("%q args does not contain required key: '%q'", argType, requiredKey)
		}
	}
	return nil
}

// ValidateOutputSchema validates an output JSON string against a schema.
func ValidateOutputSchema(output string, schema *genai.Schema) (map[string]any, error) {
	if schema == nil {
		return nil, fmt.Errorf("schema cannot be nil")
	}
	var outputMap map[string]any
	err := json.Unmarshal([]byte(output), &outputMap)
	if err != nil {
		return nil, fmt.Errorf("failed to parse output JSON: %w", err)
	}

	if err := ValidateMapOnSchema(outputMap, schema, false); err != nil { // isInput = false
		return nil, err
	}
	return outputMap, nil
}
