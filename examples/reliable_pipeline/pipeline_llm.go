// SPDX-License-Identifier: Apache-2.0

package main

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
)

const intentLLMInstruction = `You classify a single user request. The runtime has already normalized and bounded the text.

Normalized user text:
{temp:reliable_normalized_text}

Rules:
- Output one JSON object only. You may wrap it in markdown code fences if you must, but prefer raw JSON.
- Shape: {"intent":"<non-empty label>","query":"<string>"}
- query should reflect what the user wants (you may use the normalized text verbatim).`

// BuildReliablePipelineWithLLM is the production-shaped pipeline: normalize → real LLM → validate/fallback.
//
// Wire-up notes:
//   - normalizeAgent writes session key temp:reliable_normalized_text (see stateNormalized).
//   - The llmagent reads that key via the Instruction template above and writes the full model
//     reply text to temp:reliable_raw_model_output (OutputKey), same as the simulated weak model.
//   - validateAgent still runs ParseIntentJSON + safe fallback; weak or malformed JSON cannot skip it.
//
// Swap model.LLM for any ADK-backed model (Gemini, etc.); keep GenerateContentConfig (temperature,
// etc.) on llmagent.Config if needed.
func BuildReliablePipelineWithLLM(m model.LLM) (agent.Agent, error) {
	n1, err := agent.New(agent.Config{Name: "normalize", Description: "Bound user text", Run: normalizeAgent{}.Run})
	if err != nil {
		return nil, err
	}
	n2, err := llmagent.New(llmagent.Config{
		Name:                     "intent_llm",
		Model:                    m,
		Description:              "Emit intent JSON (may be messy; validator repairs/fallbacks).",
		Instruction:              intentLLMInstruction,
		OutputKey:                stateRawModel,
		DisallowTransferToParent: true,
		DisallowTransferToPeers:  true,
	})
	if err != nil {
		return nil, err
	}
	n3, err := agent.New(agent.Config{Name: "validate", Description: "Parse or fallback", Run: validateAgent{}.Run})
	if err != nil {
		return nil, err
	}
	return sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "reliable_pipeline_llm",
			Description: "Normalize → llmagent → validate (harness control outside the LLM)",
			SubAgents:   []agent.Agent{n1, n2, n3},
		},
	})
}
