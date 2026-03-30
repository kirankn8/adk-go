// SPDX-License-Identifier: Apache-2.0

// Reliable pipeline example: fixed-order [normalize → model text → validate/repair].
//
// DeerFlow-style harnesses keep control in the runtime (graph, sandboxes, skills).
// Here the graph is a SequentialAgent; the middle step is either a simulated weak
// model (default) or a real llmagent (see below). Validation and fallbacks stay in
// Go so a weak model cannot skip them.
//
// Runs:
//   - Default: simulated model (RELIABLE_PIPELINE_BROKEN_MODEL=1 for messy output).
//   - Real LLM: RELIABLE_PIPELINE_USE_LLM=1 and Gemini client env (see
//     examples/workflowagents/sequentialCode); optional GEMINI_MODEL (default
//     gemini-2.5-flash). Uses BuildReliablePipelineWithLLM in pipeline_llm.go.
package main

import (
	"context"
	"log"
	"os"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	var root agent.Agent
	var err error

	if strings.EqualFold(strings.TrimSpace(os.Getenv("RELIABLE_PIPELINE_USE_LLM")), "1") {
		modelName := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
		if modelName == "" {
			modelName = "gemini-2.5-flash"
		}
		m, gerr := gemini.NewModel(ctx, modelName, &genai.ClientConfig{})
		if gerr != nil {
			log.Fatalf("gemini model (set GOOGLE_API_KEY / auth as for other ADK examples): %v", gerr)
		}
		root, err = BuildReliablePipelineWithLLM(m)
	} else {
		broken := strings.EqualFold(os.Getenv("RELIABLE_PIPELINE_BROKEN_MODEL"), "1")
		root, err = BuildReliablePipeline(broken)
	}
	if err != nil {
		log.Fatal(err)
	}

	cfg := &launcher.Config{AgentLoader: agent.NewSingleLoader(root)}
	l := full.NewLauncher()
	if err := l.Execute(ctx, cfg, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
