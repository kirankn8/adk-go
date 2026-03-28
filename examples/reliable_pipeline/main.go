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

// Reliable pipeline example: fixed-order [normalize → model text → validate/repair].
//
// DeerFlow-style harnesses keep control in the runtime (graph, sandboxes, skills).
// Here the graph is a SequentialAgent; only the middle step would call an LLM in
// production. Validation and fallbacks stay in Go so a weak model cannot skip them.
//
// Replace weakModelAgent with llmagent.New(...) using output schema or a single
// constrained tool; keep the final validator step.
package main

import (
	"context"
	"log"
	"os"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
)

func main() {
	ctx := context.Background()
	broken := strings.EqualFold(os.Getenv("RELIABLE_PIPELINE_BROKEN_MODEL"), "1")

	root, err := BuildReliablePipeline(broken)
	if err != nil {
		log.Fatal(err)
	}

	cfg := &launcher.Config{AgentLoader: agent.NewSingleLoader(root)}
	l := full.NewLauncher()
	if err := l.Execute(ctx, cfg, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
