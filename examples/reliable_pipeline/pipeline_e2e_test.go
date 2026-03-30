// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/internal/testutil"
)

// E2E: real runner.Run + in-memory session + sequential custom agents + temp state keys.
func TestReliablePipelineRunnerE2E_simulatedGoodModel(t *testing.T) {
	ag, err := BuildReliablePipeline(false)
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewTestAgentRunner(t, ag)
	texts, err := testutil.CollectTextParts(r.Run(t, t.Name(), "  hello e2e  "))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, `[ok] Parsed intent="echo"`) {
		t.Fatalf("expected successful parse in output, got:\n%s", joined)
	}
	if !strings.Contains(joined, "hello e2e") {
		t.Fatalf("expected normalized query echoed in parse line, got:\n%s", joined)
	}
	if !strings.Contains(joined, "[pipeline] Input bounded") {
		t.Fatalf("expected normalize event, got:\n%s", joined)
	}
}

func TestReliablePipelineRunnerE2E_simulatedBrokenModelUsesFallback(t *testing.T) {
	ag, err := BuildReliablePipeline(true)
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewTestAgentRunner(t, ag)
	texts, err := testutil.CollectTextParts(r.Run(t, t.Name(), "user question"))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "[fallback]") {
		t.Fatalf("expected fallback path, got:\n%s", joined)
	}
	if !strings.Contains(joined, "user question") {
		t.Fatalf("expected fallback to mention normalized input, got:\n%s", joined)
	}
	if strings.Contains(joined, `[ok] Parsed intent=`) {
		t.Fatalf("did not expect successful parse on broken model, got:\n%s", joined)
	}
}

func TestReliablePipelineRunnerE2E_longInputTruncatedStillFallbackSafe(t *testing.T) {
	ag, err := BuildReliablePipeline(true)
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewTestAgentRunner(t, ag)
	long := strings.Repeat("x", 5000)
	texts, err := testutil.CollectTextParts(r.Run(t, t.Name(), long))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "[fallback]") {
		t.Fatalf("expected fallback, got:\n%s", joined)
	}
	// normalizeAgent caps at 4000; fallback uses %q so a long run of x must still appear.
	if !strings.Contains(joined, strings.Repeat("x", 300)) {
		t.Fatalf("expected truncated normalized input inside fallback echo, got len=%d", len(joined))
	}
}

// E2E: normalize → llmagent (mock) → validate; same state keys as production BuildReliablePipelineWithLLM.
func TestReliablePipelineWithLLM_MockModel_goodJSON(t *testing.T) {
	mm := &testutil.MockModel{
		Responses: []*genai.Content{
			genai.NewContentFromText(`Sure — {"intent":"search","query":"find docs"}`+"\n", genai.RoleModel),
		},
	}
	ag, err := BuildReliablePipelineWithLLM(mm)
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewTestAgentRunner(t, ag)
	texts, err := testutil.CollectTextParts(r.Run(t, t.Name(), "  hello  "))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, `[ok] Parsed intent="search"`) {
		t.Fatalf("expected successful parse from llm path, got:\n%s", joined)
	}
	if !strings.Contains(joined, "find docs") {
		t.Fatalf("expected query in parse line, got:\n%s", joined)
	}
}

func TestReliablePipelineWithLLM_MockModel_badJSONUsesFallback(t *testing.T) {
	mm := &testutil.MockModel{
		Responses: []*genai.Content{
			genai.NewContentFromText("not json at all\n", genai.RoleModel),
		},
	}
	ag, err := BuildReliablePipelineWithLLM(mm)
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewTestAgentRunner(t, ag)
	texts, err := testutil.CollectTextParts(r.Run(t, t.Name(), "user ask"))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "[fallback]") {
		t.Fatalf("expected fallback, got:\n%s", joined)
	}
	if strings.Contains(joined, `[ok] Parsed intent=`) {
		t.Fatalf("did not expect successful parse, got:\n%s", joined)
	}
}
