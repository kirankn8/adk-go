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

package planner

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// BuiltInPlanner uses the model's built-in thinking features (see Python google.adk.planners.BuiltInPlanner).
type BuiltInPlanner struct {
	ThinkingConfig *genai.ThinkingConfig
}

// NewBuiltInPlanner returns a planner that applies cfg to each LLM request.
func NewBuiltInPlanner(cfg *genai.ThinkingConfig) *BuiltInPlanner {
	return &BuiltInPlanner{ThinkingConfig: cfg}
}

// ApplyThinkingConfig sets ThinkingConfig on the request, overriding any existing value.
func (b *BuiltInPlanner) ApplyThinkingConfig(req *model.LLMRequest) {
	if b == nil || b.ThinkingConfig == nil || req == nil {
		return
	}
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	req.Config.ThinkingConfig = b.ThinkingConfig
}

// BuildPlanningInstruction implements Planner.
func (*BuiltInPlanner) BuildPlanningInstruction(agent.ReadonlyContext, *model.LLMRequest) (string, error) {
	return "", nil
}

// ProcessPlanningResponse implements Planner.
func (*BuiltInPlanner) ProcessPlanningResponse(agent.CallbackContext, []*genai.Part) ([]*genai.Part, error) {
	return nil, nil
}
