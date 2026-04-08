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

package knloop

import (
	"fmt"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/session"
)

// resolverSchema defines the structured output for the skill resolution step.
// The resolver LLM must produce all four fields via set_model_response.
var resolverSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"available_commands": {
			Type:        genai.TypeString,
			Description: "Comma-separated list of shell commands available for investigation (e.g. kubectl, journalctl, systemctl, crictl).",
		},
		"ok_signals": {
			Type:        genai.TypeString,
			Description: "What a healthy/working system looks like for this domain (e.g. 'Active: active (running)', 'api_healthy: true').",
		},
		"err_signals": {
			Type:        genai.TypeString,
			Description: "What a failing system looks like for this domain (e.g. 'CrashLoopBackOff', 'OOMKilled', 'failed to apply kubeadm init').",
		},
		"domain_context": {
			Type:        genai.TypeString,
			Description: "A concise paragraph describing the technology stack, key boot phases, and best investigation approach for this issue.",
		},
	},
	Required: []string{"available_commands", "ok_signals", "err_signals", "domain_context"},
}

// runResolution runs Step 1 of the knloop investigation: skill resolution.
//
// The resolver agent performs progressive skill disclosure:
//  1. Calls list_skills to enumerate available skills.
//  2. Loads and reads the skills most relevant to the reported issue.
//  3. Optionally runs read-only discovery scripts from those skills.
//  4. Produces a structured summary (available_commands, ok_signals,
//     err_signals, domain_context) saved to stateSkillContext.
//
// Returns false if the consumer stopped early.
func runResolution(ctx agent.InvocationContext, base llmagent.BaseAgentConfig, yield func(*session.Event, error) bool) bool {
	ag, err := llmagent.New(llmagent.Config{
		Name:                    "knloop_resolver",
		Model:                   base.Model,
		Tools:                   base.Tools,
		Toolsets:                base.Toolsets,
		Instruction:             resolverPrompt,
		OutputSchema:            resolverSchema,
		OutputKey:               stateSkillContext,
		IncludeContents:         llmagent.IncludeContentsNone,
		DisallowTransferToParent: true,
		DisallowTransferToPeers: true,
	})
	if err != nil {
		yield(nil, fmt.Errorf("knloop: create resolver agent: %w", err))
		return false
	}
	return drain(ag, ctx, yield)
}
