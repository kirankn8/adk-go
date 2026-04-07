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

package planner_test

import (
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/planner"
)

func TestPlanReActPlannerProcessPlanningResponse(t *testing.T) {
	t.Parallel()
	p := planner.NewPlanReActPlanner()

	t.Run("empty", func(t *testing.T) {
		out, err := p.ProcessPlanningResponse(nil, nil)
		if err != nil || out != nil {
			t.Fatalf("got (%v, %v), want (nil, nil)", out, err)
		}
	})

	t.Run("planning_tag_marks_thought", func(t *testing.T) {
		out, err := p.ProcessPlanningResponse(nil, []*genai.Part{
			{Text: planner.PlanningTag + " step one"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || !out[0].Thought || out[0].Text != planner.PlanningTag+" step one" {
			t.Fatalf("got %+v", out)
		}
	})

	t.Run("final_answer_split", func(t *testing.T) {
		out, err := p.ProcessPlanningResponse(nil, []*genai.Part{
			{Text: "reasoning " + planner.FinalAnswerTag + "done"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 {
			t.Fatalf("len=%d parts %+v", len(out), out)
		}
		if !out[0].Thought || out[0].Text != "reasoning "+planner.FinalAnswerTag {
			t.Fatalf("thought part: %+v", out[0])
		}
		if out[1].Thought || out[1].Text != "done" {
			t.Fatalf("answer part: %+v", out[1])
		}
	})

	t.Run("function_call_only", func(t *testing.T) {
		out, err := p.ProcessPlanningResponse(nil, []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "x", Args: map[string]any{}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || out[0].FunctionCall == nil {
			t.Fatalf("got %+v", out)
		}
	})

	// Matches Python PlanReActPlanner: leading tagged text before the first tool call is not preserved.
	t.Run("text_then_chained_function_calls", func(t *testing.T) {
		out, err := p.ProcessPlanningResponse(nil, []*genai.Part{
			{Text: planner.ReasoningTag + " ok"},
			{FunctionCall: &genai.FunctionCall{Name: "a", Args: map[string]any{}}},
			{FunctionCall: &genai.FunctionCall{Name: "b", Args: map[string]any{}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 {
			t.Fatalf("len=%d %+v", len(out), out)
		}
		if out[0].FunctionCall.Name != "a" || out[1].FunctionCall.Name != "b" {
			t.Fatalf("got %+v", out)
		}
	})
}

func TestBuiltInPlannerApplyThinkingConfig(t *testing.T) {
	t.Parallel()
	cfg := &genai.ThinkingConfig{IncludeThoughts: true}
	b := planner.NewBuiltInPlanner(cfg)
	req := &model.LLMRequest{}
	b.ApplyThinkingConfig(req)
	if req.Config == nil || req.Config.ThinkingConfig != cfg {
		t.Fatalf("ThinkingConfig not applied: %+v", req.Config)
	}
}
