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

// Task is a single discovery question with its collected evidence.
type Task struct {
	Question string
	Evidence string
}

// Plan holds the staged workflow produced by the planner.
// Each inner slice is a parallel stage; stages run in order.
type Plan struct {
	Stages [][]Task
}

// generatePlan runs the workflow-generation ralph loop (Step 2).
//
// The planner LLM outputs plain text: task questions separated by newlines,
// with blank lines between parallel stages. The harness captures the text,
// parses it into stages, and validates it. On failure the error is injected
// into statePlanFailures and the LLM retries up to cfg.MaxPlanIterations times.
func generatePlan(ctx agent.InvocationContext, base llmagent.BaseAgentConfig, cfg *Config, yield func(*session.Event, error) bool) (Plan, bool) {
	for i := 0; i < cfg.MaxPlanIterations; i++ {
		if i > 0 {
			if !emitText(fmt.Sprintf("  ↻ plan attempt %d/%d\n", i+1, cfg.MaxPlanIterations), yield) {
				return Plan{}, false
			}
		}

		ag, err := llmagent.New(llmagent.Config{
			Name:                     "knloop_planner",
			Model:                    base.Model,
			Tools:                    base.Tools,
			Toolsets:                 base.Toolsets,
			Instruction:              plannerPrompt,
			IncludeContents:          llmagent.IncludeContentsNone,
			DisallowTransferToParent: true,
			DisallowTransferToPeers:  true,
		})
		if err != nil {
			yield(nil, fmt.Errorf("knloop: create planner agent: %w", err))
			return Plan{}, false
		}

		text, ok := drainCapture(ag, ctx, yield)
		if !ok {
			return Plan{}, false
		}

		plan := parseWorkflow(text)

		skillCtx := stateGetString(ctx, stateSkillContext)
		if failures := validateTasks(plan, skillCtx, cfg.MinPlanTasks); len(failures) > 0 {
			msg := strings.Join(failures, "; ")
			if !emitText("  ✗ plan invalid: "+msg+"\n", yield) {
				return Plan{}, false
			}
			stateSet(ctx, statePlanFailures, strings.Join(failures, "\n"))
			continue
		}

		stateSet(ctx, statePlanFailures, "")
		return plan, true
	}

	stateSet(ctx, statePlanFailures, "")
	return Plan{}, true
}

// parseWorkflow parses the planner's plain-text output into a Plan.
//
// Blank lines (\n\n) separate parallel stages.
// Lines within a stage are individual task questions.
// Lines beginning with "#" are ignored (comments).
func parseWorkflow(text string) Plan {
	var stages [][]Task
	for _, chunk := range strings.Split(text, "\n\n") {
		var tasks []Task
		for _, line := range strings.Split(chunk, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			tasks = append(tasks, Task{Question: line})
		}
		if len(tasks) > 0 {
			stages = append(stages, tasks)
		}
	}
	return Plan{Stages: stages}
}

// validateTasks runs deterministic checks on all tasks across all stages.
// Returns a list of failure descriptions; empty means the plan is valid.
func validateTasks(plan Plan, skillContext string, minTasks int) []string {
	var failures []string

	total := 0
	for _, stage := range plan.Stages {
		total += len(stage)
	}
	if total < minTasks {
		failures = append(failures, fmt.Sprintf(
			"plan has only %d tasks but at least %d are required; add more discovery questions",
			total, minTasks,
		))
	}

	seen := make(map[string]bool)
	taskNum := 0
	for _, stage := range plan.Stages {
		for _, t := range stage {
			taskNum++
			q := strings.TrimSpace(t.Question)
			if q == "" {
				failures = append(failures, fmt.Sprintf("task %d has an empty question", taskNum))
				continue
			}
			lower := strings.ToLower(q)
			for _, conj := range []string{" and ", " or ", " but ", " also "} {
				if strings.Contains(lower, conj) {
					failures = append(failures, fmt.Sprintf(
						"task %d is compound (contains %q): %q — split into separate tasks",
						taskNum, strings.TrimSpace(conj), q,
					))
					break
				}
			}
			if seen[lower] {
				failures = append(failures, fmt.Sprintf("task %d is a duplicate: %q", taskNum, q))
			}
			seen[lower] = true
		}
	}

	// Soft: at least some questions should reference terms from the skill context.
	if skillContext != "" && total > 0 {
		lowerCtx := strings.ToLower(skillContext)
		anyMatch := false
	outer:
		for _, stage := range plan.Stages {
			for _, t := range stage {
				for _, w := range strings.Fields(strings.ToLower(t.Question)) {
					if len(w) > 3 && strings.Contains(lowerCtx, w) {
						anyMatch = true
						break outer
					}
				}
			}
		}
		if !anyMatch {
			failures = append(failures,
				"none of the questions reference terms from the skill context; "+
					"ensure questions are grounded in the observable environment")
		}
	}

	return failures
}
