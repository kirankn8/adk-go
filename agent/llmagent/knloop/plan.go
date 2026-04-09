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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/session"
)

// Task is a single discovery question with its collected evidence.
type Task struct {
	Question string `json:"question"`
	Evidence string `json:"evidence,omitempty"`
}

// Plan holds the staged workflow produced by the planner.
// Each inner slice is a parallel stage; stages run in order.
type Plan struct {
	Stages [][]Task
}

// workflowSchema is the output schema for the planner agent.
// The planner calls set_model_response({"script": "..."}) when done.
var workflowSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"script": {
			Type: genai.TypeString,
			Description: "Bash script that echoes task questions. " +
				"Blank lines between echo groups define parallel stages.",
		},
	},
	Required: []string{"script"},
}

// generatePlan runs the workflow-generation ralph loop (Step 2).
//
// Each iteration: planner LLM writes a bash workflow script →
// static validation (syntax + safety) → execute script → parse stages →
// task-level validation. On any failure the error is injected into
// statePlanFailures and the LLM retries up to cfg.MaxPlanIterations times.
//
// Returns (plan, true) on success or when max iterations are exceeded.
// Returns (Plan{}, false) only if the consumer (yield) stopped early.
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
			OutputSchema:             workflowSchema,
			OutputKey:                stateNavigatorScript,
			IncludeContents:          llmagent.IncludeContentsNone,
			DisallowTransferToParent: true,
			DisallowTransferToPeers:  true,
		})
		if err != nil {
			yield(nil, fmt.Errorf("knloop: create planner agent: %w", err))
			return Plan{}, false
		}

		if !drain(ag, ctx, yield) {
			return Plan{}, false
		}

		// Extract the script from state (written by the planner via OutputKey).
		scriptJSON := stateGetString(ctx, stateNavigatorScript)
		script, err := extractScript(scriptJSON)
		if err != nil {
			stateSet(ctx, statePlanFailures,
				"Could not extract script: "+err.Error()+
					`. Call set_model_response({"script": "..."}) with the complete bash script.`)
			continue
		}

		// Static script validation (no execution yet).
		if scriptFailures := validateScript(script); len(scriptFailures) > 0 {
			msg := strings.Join(scriptFailures, "; ")
			if !emitText("  ✗ script invalid: "+msg+"\n", yield) {
				return Plan{}, false
			}
			stateSet(ctx, statePlanFailures, strings.Join(scriptFailures, "\n"))
			continue
		}

		// Execute the script to get the workflow definition.
		stdout, execErr := executeWorkflowScript(script, cfg.ScriptTimeout)
		if execErr != nil {
			if !emitText("  ✗ script execution failed: "+execErr.Error()+"\n", yield) {
				return Plan{}, false
			}
			stateSet(ctx, statePlanFailures, "Script execution failed: "+execErr.Error()+
				". Ensure the script only uses echo and exits 0.")
			continue
		}

		plan := parseWorkflow(stdout)

		// Task-level validation.
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

	// Max iterations reached — return empty plan; synthesis runs with no evidence.
	stateSet(ctx, statePlanFailures, "")
	return Plan{}, true
}

// extractScript unpacks {"script":"..."} from the planner's OutputKey state value.
func extractScript(jsonStr string) (string, error) {
	if strings.TrimSpace(jsonStr) == "" {
		return "", fmt.Errorf("empty output")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	s, ok := m["script"].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("missing or empty \"script\" field")
	}
	return s, nil
}

// validateScript runs static checks on the bash script before execution.
func validateScript(script string) []string {
	var failures []string
	if err := checkBashSyntax(script); err != nil {
		failures = append(failures, "bash syntax error: "+err.Error())
	}
	if cmds := findDestructiveCommands(script); len(cmds) > 0 {
		failures = append(failures,
			"script contains destructive commands ("+strings.Join(cmds, ", ")+
				"); the workflow script must only use echo and comments")
	}
	return failures
}

// checkBashSyntax validates the script using "bash -n" (parse-only, no execution).
func checkBashSyntax(script string) error {
	f, err := os.CreateTemp("", "knloop_plan_*.sh")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-n", f.Name())
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(errBuf.String()))
	}
	return nil
}

// destructivePatterns lists command patterns that must not appear in the workflow script.
var destructivePatterns = []string{
	"rm -rf", "rm -r", "mkfs", "dd ", ":(){ :|:& };",
	"> /dev/", "shred ", "wipefs", "fdisk", "parted ",
}

// findDestructiveCommands returns any destructive patterns found in the script.
func findDestructiveCommands(script string) []string {
	lower := strings.ToLower(script)
	var found []string
	for _, p := range destructivePatterns {
		if strings.Contains(lower, p) {
			found = append(found, p)
		}
	}
	return found
}

// executeWorkflowScript runs the workflow script with no arguments and returns stdout.
// The script is expected to only echo task questions and exit 0.
func executeWorkflowScript(script string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exit error: %w\nstderr: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// parseWorkflow converts the workflow script's stdout into a Plan.
//
// Blank lines separate parallel stages. Lines beginning with "#" are ignored.
// Tasks within a stage can run in parallel (goroutine parallelism is a follow-up);
// stages themselves run in order.
func parseWorkflow(stdout string) Plan {
	var stages [][]Task
	for _, chunk := range strings.Split(stdout, "\n\n") {
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
// It returns a list of failure descriptions; empty means the plan is valid.
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
