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

package llminternal

import (
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/internal/toolinternal/toolutils"
	"google.golang.org/adk/internal/utils"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

const (
	instructionForProcessor = "IMPORTANT: You have access to other tools, but you must provide " +
		"your final response using the set_model_response tool with the " +
		"required structured format. After using any other tools needed " +
		"to complete the task, always call set_model_response with your " +
		"final answer in the specified schema format."

	// maxSetModelResponseSchemaFailureAttempts is how many consecutive invalid
	// set_model_response results (e.g. output schema validation) are allowed before
	// emitting a terminal synthetic model message. Value 4 => one failed attempt
	// plus up to 3 automatic LLM retries in the same agent run.
	maxSetModelResponseSchemaFailureAttempts = 4
)

// outputSchemaRequestProcessor injects set_model_response when OutputSchema is used with tools/toolsets.
func outputSchemaRequestProcessor(ctx agent.InvocationContext, req *model.LLMRequest, f *Flow) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		llmAgent := asLLMAgent(ctx.Agent())
		if llmAgent == nil {
			return
		}

		state := llmAgent.internal()
		if state.OutputSchema == nil || !needOutputSchemaProcessor(state) {
			return
		}

		setResponseTool := &setModelResponseTool{schema: state.OutputSchema}
		if err := toolutils.PackTool(req, setResponseTool); err != nil {
			yield(nil, fmt.Errorf("failed to pack set_model_response tool: %w", err))
			return
		}

		utils.AppendInstructions(req, instructionForProcessor)
	}
}

// createFinalModelResponseEvent builds a model text event from the set_model_response payload JSON.
func createFinalModelResponseEvent(invocationContext agent.InvocationContext, response string) *session.Event {
	finalEvent := session.NewEvent(invocationContext.InvocationID())
	finalEvent.Author = invocationContext.Agent().Name()
	finalEvent.Branch = invocationContext.Branch()
	finalEvent.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: response}},
	}
	return finalEvent
}

// retrieveStructuredModelResponse returns JSON from a set_model_response function response, or empty if none.
func retrieveStructuredModelResponse(ev *session.Event) (string, error) {
	if ev == nil || ev.LLMResponse.Content == nil {
		return "", nil
	}

	for _, part := range ev.LLMResponse.Content.Parts {
		if part.FunctionResponse != nil && part.FunctionResponse.Name == "set_model_response" {
			bytes, err := json.Marshal(part.FunctionResponse.Response)
			if err != nil {
				return "", fmt.Errorf("failed to marshal set_model_response: %w", err)
			}
			return string(bytes), nil
		}
	}

	return "", nil
}

// schemaTypeShortLabel is a compact type name for error hints (not full JSON Schema).
func schemaTypeShortLabel(s *genai.Schema) string {
	if s == nil {
		return "ANY"
	}
	t := genai.Type(strings.ToUpper(string(s.Type)))
	switch t {
	case genai.TypeArray:
		if s.Items != nil {
			return "ARRAY[" + schemaTypeShortLabel(s.Items) + "]"
		}
		return "ARRAY"
	case genai.TypeObject:
		return "OBJECT"
	case "":
		return "ANY"
	default:
		return string(t)
	}
}

const maxOutputSchemaSummaryRunes = 1200

// shortOutputSchemaSummary builds a one-line hint of allowed top-level fields for set_model_response.
func shortOutputSchemaSummary(schema *genai.Schema) string {
	if schema == nil || len(schema.Properties) == 0 {
		return ""
	}
	required := make(map[string]bool, len(schema.Required))
	for _, k := range schema.Required {
		required[k] = true
	}
	keys := slices.Sorted(maps.Keys(schema.Properties))
	var b strings.Builder
	b.WriteString("Allowed fields: ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k)
		b.WriteByte('(')
		b.WriteString(schemaTypeShortLabel(schema.Properties[k]))
		if required[k] {
			b.WriteString(", required")
		}
		b.WriteByte(')')
	}
	s := b.String()
	r := []rune(s)
	if len(r) > maxOutputSchemaSummaryRunes {
		return string(r[:maxOutputSchemaSummaryRunes]) + "…"
	}
	return s
}

const setModelResponseSchemaFailureCountKey = session.KeyPrefixTemp + "adk_set_model_response_schema_failures"

func readSetModelResponseFailureCount(st session.State) int {
	if st == nil {
		return 0
	}
	v, err := st.Get(setModelResponseSchemaFailureCountKey)
	if err != nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

// setModelResponseHasErrorInEvent reports whether ev contains a set_model_response
// tool result with an "error" field (schema validation or other tool failure).
func setModelResponseHasErrorInEvent(ev *session.Event) bool {
	if ev == nil || ev.LLMResponse.Content == nil {
		return false
	}
	for _, part := range ev.LLMResponse.Content.Parts {
		fr := part.FunctionResponse
		if fr == nil || fr.Name != "set_model_response" {
			continue
		}
		if fr.Response == nil {
			continue
		}
		if _, hasErr := fr.Response["error"]; hasErr {
			return true
		}
	}
	return false
}

// prepareSetModelResponseSyntheticFinal updates temp session state for set_model_response
// validation failures and returns whether to emit the terminal synthetic model JSON event.
// When false, Flow.Run issues another LLM request so the model can fix the payload.
func prepareSetModelResponseSyntheticFinal(ctx agent.InvocationContext, toolResponseEv *session.Event) (emit bool, err error) {
	st := ctx.Session().State()
	key := setModelResponseSchemaFailureCountKey
	if !setModelResponseHasErrorInEvent(toolResponseEv) {
		if err := st.Set(key, 0); err != nil {
			return false, fmt.Errorf("session state set %q: %w", key, err)
		}
		return true, nil
	}
	n := readSetModelResponseFailureCount(st) + 1
	if err := st.Set(key, n); err != nil {
		return false, fmt.Errorf("session state set %q: %w", key, err)
	}
	if n < maxSetModelResponseSchemaFailureAttempts {
		return false, nil
	}
	if err := st.Set(key, 0); err != nil {
		return false, fmt.Errorf("session state set %q: %w", key, err)
	}
	return true, nil
}

// needOutputSchemaProcessor is true when OutputSchema is paired with tools or toolsets (set_model_response path).
func needOutputSchemaProcessor(state *State) bool {
	if state == nil || state.OutputSchema == nil {
		return false
	}
	return len(state.Tools) > 0 || len(state.Toolsets) > 0
}

// setModelResponseTool is the structured final-answer tool (tool.Tool, toolinternal.FunctionTool).
type setModelResponseTool struct {
	schema *genai.Schema
}

func (t *setModelResponseTool) Name() string {
	return "set_model_response"
}

func (t *setModelResponseTool) Description() string {
	return "Set your final response using the required output schema. Use this tool to provide your final structured answer instead of outputting text directly."
}

func (t *setModelResponseTool) IsLongRunning() bool {
	return false
}

func (t *setModelResponseTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:                 t.Name(),
		Description:          t.Description(),
		ParametersJsonSchema: t.schema,
	}
}

func (t *setModelResponseTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	m, ok := args.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected args type for set_model_response: %T", args)
	}
	coerced := utils.ShallowCopyMap(m)
	utils.CoerceFlexibleOutputArgs(coerced, t.schema)
	if err := utils.ValidateMapOnSchema(coerced, t.schema, false); err != nil {
		if hint := shortOutputSchemaSummary(t.schema); hint != "" {
			return nil, fmt.Errorf("invalid output schema: %w. %s", err, hint)
		}
		return nil, fmt.Errorf("invalid output schema: %w", err)
	}
	return coerced, nil
}
