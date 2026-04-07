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
	"iter"

	"google.golang.org/adk/agent"
	icontext "google.golang.org/adk/internal/context"
	"google.golang.org/adk/internal/utils"
	"google.golang.org/adk/model"
	"google.golang.org/adk/planner"
	"google.golang.org/adk/session"
)

// nlPlanningRequestProcessor mirrors Python _nl_planning._NlPlanningRequestProcessor.
func nlPlanningRequestProcessor(ctx agent.InvocationContext, req *model.LLMRequest, _ *Flow) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		llmAgent := asLLMAgent(ctx.Agent())
		if llmAgent == nil {
			return
		}
		p := llmAgent.internal().Planner
		if p == nil {
			return
		}
		p.ApplyThinkingConfig(req)
		inst, err := p.BuildPlanningInstruction(icontext.NewReadonlyContext(ctx), req)
		if err != nil {
			yield(nil, err)
			return
		}
		if inst != "" {
			utils.AppendInstructions(req, inst)
		}
		removeThoughtFromRequest(req)
	}
}

func nlPlanningResponseProcessor(ctx agent.InvocationContext, _ *model.LLMRequest, resp *model.LLMResponse) error {
	if resp == nil || resp.Partial {
		return nil
	}
	if resp.Content == nil || len(resp.Content.Parts) == 0 {
		return nil
	}
	llmAgent := asLLMAgent(ctx.Agent())
	if llmAgent == nil {
		return nil
	}
	p := llmAgent.internal().Planner
	if p == nil {
		return nil
	}
	if _, ok := p.(*planner.BuiltInPlanner); ok {
		return nil
	}
	cctx := icontext.NewCallbackContextWithDelta(ctx, nil, nil)
	processed, err := p.ProcessPlanningResponse(cctx, resp.Content.Parts)
	if err != nil {
		return err
	}
	if processed != nil {
		resp.Content.Parts = processed
	}
	return nil
}

func removeThoughtFromRequest(req *model.LLMRequest) {
	if req == nil {
		return
	}
	for _, c := range req.Contents {
		if c == nil {
			continue
		}
		for _, part := range c.Parts {
			if part == nil {
				continue
			}
			part.Thought = false
		}
	}
}
