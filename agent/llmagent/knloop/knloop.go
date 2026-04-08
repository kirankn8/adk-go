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

// Package knloop implements a hypothesis-driven, multi-step investigation
// architecture for LLM agents.
//
// When set as the Architecture on an llmagent.Config, knloop replaces the
// default ReAct loop with a four-step flow:
//
//  1. Skill Resolution — the LLM surveys available tools and skills to build
//     an understanding of what can be observed in the environment.
//  2. Planning (ralph loop) — the LLM generates up to 100 small, focused
//     discovery tasks about the environment. A deterministic validator
//     retries the loop until the plan is valid or max iterations are reached.
//  3. Per-task Investigation (ralph loop of ReAct loop) — for each task, an
//     inner ReAct loop explores freely and writes a bash evidence script.
//     The harness runs the script; if it succeeds, the stdout is captured as
//     evidence. On failure the script is rejected and the inner loop retries.
//  4. Synthesis — a single LLM call reads all task evidence and produces a
//     root-cause report that is streamed back to the user.
package knloop

import (
	"encoding/json"
	"iter"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/session"
)

// Session state keys used to pass data between knloop steps.
// All keys are prefixed with "knloop_" to avoid collisions.
const (
	stateSkillContext  = "knloop_skill_context"
	statePlan          = "knloop_plan"
	statePlanFailures  = "knloop_plan_failures"
	stateCurrentTask   = "knloop_current_task"
	stateEvidScript    = "knloop_evidence_script"
	stateScriptFailure = "knloop_script_failure"
	stateAllEvidence   = "knloop_all_evidence"
)

// Config holds tuning parameters for the knloop investigation architecture.
// Use New() to create a Config with production defaults.
type Config struct {
	// MaxResolutionIterations caps the number of ReAct steps the resolver agent
	// may take before it must produce its final skill-context output.
	MaxResolutionIterations int

	// MaxPlanIterations is the maximum number of times the planner ralph loop
	// reruns the planning LLM call when the generated plan fails validation.
	MaxPlanIterations int

	// MinPlanTasks is the minimum number of tasks the planner must produce for
	// the plan to be considered valid.
	MinPlanTasks int

	// MaxIterationsPerTask is the maximum number of outer ralph-loop iterations
	// per task before knloop moves on with empty evidence for that task.
	MaxIterationsPerTask int

	// MaxReActIterationsPerTask is not directly enforced by knloop today but is
	// retained for future use limiting the inner investigator ReAct loop.
	MaxReActIterationsPerTask int

	// TestTimeout is the maximum duration allowed for each evidence script
	// execution.
	TestTimeout time.Duration
}

// New returns a *Config populated with production defaults.
func New() *Config {
	return &Config{
		MaxResolutionIterations:   10,
		MaxPlanIterations:         3,
		MinPlanTasks:              10,
		MaxIterationsPerTask:      5,
		MaxReActIterationsPerTask: 8,
		TestTimeout:               30 * time.Second,
	}
}

// ConfigForHost returns a *Config when host is non-empty, or nil which leaves
// the parent llmagent running its default ReAct loop. This is the idiomatic
// way to wire knloop: enable it only when an external LLM host is configured.
//
//	Architecture: knloop.ConfigForHost(c.LLMHost),
func ConfigForHost(host string) *Config {
	if host == "" {
		return nil
	}
	return New()
}

// Run implements llmagent.Architecture.
// It orchestrates the four-step knloop investigation flow.
func (c *Config) Run(ctx agent.InvocationContext, base llmagent.BaseAgentConfig) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// Step 1: Skill Resolution.
		if ok := runResolution(ctx, base, yield); !ok {
			return
		}

		// Step 2: Planning ralph loop.
		plan, ok := generatePlan(ctx, base, c, yield)
		if !ok {
			return
		}

		// Step 3: Per-task ralph(ReAct) investigation.
		for i := range plan.Tasks {
			t, ok := runTask(ctx, base, plan.Tasks[i], c, yield)
			if !ok {
				return
			}
			plan.Tasks[i] = t
		}

		// Step 4: Synthesis.
		runSynthesis(ctx, base, plan, yield)
	}
}

// drain runs ag and forwards every event through yield.
// It returns false if the outer consumer stopped early (yield returned false).
func drain(ag agent.Agent, ctx agent.InvocationContext, yield func(*session.Event, error) bool) bool {
	for ev, err := range ag.Run(ctx) {
		if !yield(ev, err) {
			return false
		}
	}
	return true
}

// stateGetString reads a session-state value as a string.
// If the key is absent or the value is not a string, it JSON-marshals the
// value or returns "".
func stateGetString(ctx agent.InvocationContext, key string) string {
	v, err := ctx.Session().State().Get(key)
	if err != nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// stateSet writes a value directly to the session state.
// This is used for transient knloop control keys (plan failures, script
// failures, current task) that need to be visible to the next sub-agent's
// instruction template substitution but do not need to be event-persisted.
func stateSet(ctx agent.InvocationContext, key string, value any) {
	_ = ctx.Session().State().Set(key, value)
}
