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

package llmagent

import (
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/planner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// Architecture replaces the default ReAct loop of an LLMAgent when non-nil.
//
// When Config.Architecture is nil the agent runs the standard ReAct loop
// unchanged. When set (e.g. knloop.New()) the Architecture's Run method is
// invoked instead, receiving the agent's resolved model, tools, toolsets, and
// instruction via BaseAgentConfig.
//
// Implementations must not import the llmagent package to avoid import cycles.
type Architecture interface {
	Run(ctx agent.InvocationContext, base BaseAgentConfig) iter.Seq2[*session.Event, error]
}

// BaseAgentConfig carries the llmagent's resolved configuration into an
// Architecture implementation. All fields are read-only; the implementation
// should create its own sub-agents rather than mutating these values.
type BaseAgentConfig struct {
	// Model is the LLM backing the agent.
	Model model.LLM
	// Tools are the individual tools declared on the agent.
	Tools []tool.Tool
	// Toolsets are the toolsets declared on the agent.
	Toolsets []tool.Toolset
	// Instruction is the agent's system prompt.
	Instruction string
	// Planner is the optional planning strategy attached to the agent.
	Planner planner.Planner
}
