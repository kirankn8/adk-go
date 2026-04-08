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
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/session"
)

// runSynthesis implements Step 4 of the knloop investigation.
//
// It builds a combined evidence string from all tasks, saves it to
// stateAllEvidence so the synthesizer prompt can read it via template
// substitution, then runs a single LLM call that streams a root-cause report.
//
// Returns false if the consumer stopped early.
func runSynthesis(ctx agent.InvocationContext, base llmagent.BaseAgentConfig, plan Plan, yield func(*session.Event, error) bool) bool {
	// Build combined evidence block.
	var sb strings.Builder
	for i, t := range plan.Tasks {
		sb.WriteString(fmt.Sprintf("--- Task %d ---\nQuestion: %s\nEvidence:\n%s\n\n", i+1, t.Question, t.Evidence))
	}
	stateSet(ctx, stateAllEvidence, sb.String())

	// Synthesizer: no tools, no output schema — streams plain-text root-cause report.
	ag, err := llmagent.New(llmagent.Config{
		Name:                    "knloop_synthesizer",
		Model:                   base.Model,
		Instruction:             synthesizerPrompt,
		IncludeContents:         llmagent.IncludeContentsNone,
		DisallowTransferToParent: true,
		DisallowTransferToPeers: true,
	})
	if err != nil {
		yield(nil, fmt.Errorf("knloop: create synthesizer agent: %w", err))
		return false
	}

	return drain(ag, ctx, yield)
}
