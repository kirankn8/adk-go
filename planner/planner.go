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

// Package planner provides optional planning strategies for LLM agents, aligned with
// Python ADK's google.adk.planners (BasePlanner, BuiltInPlanner, PlanReActPlanner).
package planner

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Planner is implemented by built-in planning strategies. Methods are no-ops where a
// given implementation does not apply (mirroring optional returns in Python BasePlanner).
type Planner interface {
	// ApplyThinkingConfig merges model-native thinking configuration into the request.
	// BuiltInPlanner sets ThinkingConfig; other planners leave req unchanged.
	ApplyThinkingConfig(req *model.LLMRequest)

	// BuildPlanningInstruction returns extra system instruction text to append before the model call.
	// Return empty string when there is nothing to add.
	BuildPlanningInstruction(ctx agent.ReadonlyContext, req *model.LLMRequest) (string, error)

	// ProcessPlanningResponse post-processes model output parts (thought tagging, plan/action tags).
	// Return (nil, nil) to leave the original parts unchanged.
	// Return a non-nil slice to replace resp.Content.Parts (may be empty).
	ProcessPlanningResponse(ctx agent.CallbackContext, parts []*genai.Part) ([]*genai.Part, error)
}
