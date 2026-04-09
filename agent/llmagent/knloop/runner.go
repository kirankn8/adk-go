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
	"os/exec"
	"strings"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/session"
)

// investigatorSchema is the structured output schema for the investigator.
// The investigator must call set_model_response with a "script" field
// containing a self-contained bash script that prints evidence to stdout.
var investigatorSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"script": {
			Type:        genai.TypeString,
			Description: "A self-contained bash script that prints evidence for the current task to stdout. Must be runnable as 'bash -c <script>'.",
		},
	},
	Required: []string{"script"},
}

// runTask implements the outer ralph loop of ReAct loop for a single task (Step 3).
//
// For each outer iteration:
//  1. The investigator ReAct loop explores freely with tools.
//  2. When ready it calls set_model_response with a bash "script".
//  3. The harness executes the script (bash -c, timeout cfg.TestTimeout).
//  4. exit 0 + non-empty stdout → evidence captured, loop exits.
//  5. failure or empty stdout  → inject reason, outer loop continues.
//
// Returns (task with Evidence filled, true) on success or exhaustion.
// Returns (task, false) if the consumer stopped early.
func runTask(ctx agent.InvocationContext, base llmagent.BaseAgentConfig, t Task, cfg *Config, yield func(*session.Event, error) bool) (Task, bool) {
	// Inject per-task state for template substitution.
	stateSet(ctx, stateCurrentTask, t.Question)
	stateSet(ctx, stateScriptFailure, "")
	stateSet(ctx, stateEvidScript, "")

	for i := 0; i < cfg.MaxIterationsPerTask; i++ {
		ag, err := llmagent.New(llmagent.Config{
			Name:                    "knloop_investigator",
			Model:                   base.Model,
			Tools:                   base.Tools,
			Toolsets:                base.Toolsets,
			Instruction:             investigatorPrompt,
			OutputSchema:            investigatorSchema,
			OutputKey:               stateEvidScript,
			IncludeContents:         llmagent.IncludeContentsNone,
			DisallowTransferToParent: true,
			DisallowTransferToPeers: true,
		})
		if err != nil {
			yield(nil, fmt.Errorf("knloop: create investigator agent: %w", err))
			return t, false
		}

		if !drain(ag, ctx, yield) {
			return t, false
		}

		// Extract the bash script from state.
		scriptJSON := stateGetString(ctx, stateEvidScript)
		script, extractErr := extractEvidScript(scriptJSON)
		if extractErr != nil || strings.TrimSpace(script) == "" {
			stateSet(ctx, stateScriptFailure,
				"No script was produced or the 'script' field was empty. "+
					"Call set_model_response with a non-empty 'script' field containing a runnable bash script.")
			continue
		}

		// Run the script and capture evidence.
		stdout, ok := executeScript(script, cfg.TestTimeout)
		if !ok {
			failMsg := fmt.Sprintf("Script failed (non-zero exit or empty stdout).\nScript:\n%s\nOutput:\n%s", script, stdout)
			if !emitText(fmt.Sprintf("    ↻ script failed (attempt %d/%d)\n", i+1, cfg.MaxIterationsPerTask), yield) {
				return t, false
			}
			stateSet(ctx, stateScriptFailure, "Fix the script and try again.\n"+failMsg)
			continue
		}

		// Evidence collected successfully.
		t.Evidence = stdout
		return t, true
	}

	// All iterations exhausted — move on with empty evidence.
	return t, true
}


// extractEvidScript unpacks the "script" field from the investigator's OutputKey JSON.
func extractEvidScript(jsonStr string) (string, error) {
	if strings.TrimSpace(jsonStr) == "" {
		return "", fmt.Errorf("empty")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	s, _ := m["script"].(string)
	return s, nil
}

// executeScript runs script as "bash -c <script>" with the given timeout.
// It returns (trimmed stdout, true) on success (exit 0 + non-empty output).
// It returns (partial stdout, false) on non-zero exit, timeout, or empty output.
func executeScript(script string, timeout time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())

	if err != nil || ctx.Err() != nil {
		return out, false
	}
	return out, out != ""
}
