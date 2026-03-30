// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"iter"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

// Session keys use a single identifier after "temp:" so they work in llmagent
// Instruction templates, e.g. {temp:reliable_normalized_text}.
const (
	stateNormalized = session.KeyPrefixTemp + "reliable_normalized_text"
	stateRawModel   = session.KeyPrefixTemp + "reliable_raw_model_output"
)

type intentPayload struct {
	Intent string `json:"intent"`
	Query  string `json:"query"`
}

func textFromUserContent(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range c.Parts {
		if p == nil {
			continue
		}
		if p.Text != "" {
			b.WriteString(p.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// normalizeAgent: deterministic bounds on user input (no LLM).
type normalizeAgent struct{}

func (normalizeAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		raw := textFromUserContent(ctx.UserContent())
		if len(raw) > 4000 {
			raw = raw[:4000]
		}
		raw = strings.TrimSpace(raw)
		if err := ctx.Session().State().Set(stateNormalized, raw); err != nil {
			yield(nil, fmt.Errorf("state set: %w", err))
			return
		}
		ev := session.NewEvent(ctx.InvocationID())
		ev.Author = "normalize"
		ev.LLMResponse = model.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{{Text: "[pipeline] Input bounded and stored.\n"}}},
		}
		yield(ev, nil)
	}
}

// weakModelAgent simulates a small model: extra prose + optional broken JSON.
type weakModelAgent struct{ brokenJSON bool }

func (a weakModelAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		q, err := ctx.Session().State().Get(stateNormalized)
		if err != nil {
			yield(nil, fmt.Errorf("read normalized: %w", err))
			return
		}
		qs, _ := q.(string)
		var modelOut string
		if a.brokenJSON {
			modelOut = fmt.Sprintf(`Okay so the user said %q and I think intent is search but here is incomplete { "intent":`, qs)
		} else {
			modelOut = fmt.Sprintf(
				`Sure! Here's what I understood:\n`+
					`{"intent":"echo","query":%q}\n`+
					`Hope that helps!`,
				qs)
		}
		if err := ctx.Session().State().Set(stateRawModel, modelOut); err != nil {
			yield(nil, err)
			return
		}
		ev := session.NewEvent(ctx.InvocationID())
		ev.Author = "weak_model_sim"
		ev.LLMResponse = model.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{{Text: modelOut + "\n"}}},
		}
		yield(ev, nil)
	}
}

// validateAgent: parse JSON from raw model output; deterministic fallback if parse fails.
type validateAgent struct{}

func (validateAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		rawAny, err := ctx.Session().State().Get(stateRawModel)
		if err != nil {
			yield(nil, err)
			return
		}
		raw, _ := rawAny.(string)
		normAny, _ := ctx.Session().State().Get(stateNormalized)
		norm, _ := normAny.(string)

		var p intentPayload
		parseErr := ParseIntentJSON(raw, &p)
		var reply string
		if parseErr != nil || p.Intent == "" {
			reply = fmt.Sprintf(
				"[fallback] Model output failed validation (%v). Using safe echo of normalized input: %q",
				parseErr, norm)
		} else {
			reply = fmt.Sprintf("[ok] Parsed intent=%q query=%q", p.Intent, p.Query)
		}
		ev := session.NewEvent(ctx.InvocationID())
		ev.Author = "validate"
		ev.LLMResponse = model.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{{Text: reply + "\n"}}},
		}
		yield(ev, nil)
	}
}

// BuildReliablePipeline returns the sequential root agent (normalize → weak model sim → validate).
func BuildReliablePipeline(brokenModel bool) (agent.Agent, error) {
	n1, err := agent.New(agent.Config{Name: "normalize", Description: "Bound user text", Run: normalizeAgent{}.Run})
	if err != nil {
		return nil, err
	}
	n2, err := agent.New(agent.Config{
		Name:        "weak_model",
		Description: "Simulated small LLM output",
		Run:         weakModelAgent{brokenJSON: brokenModel}.Run,
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
			Name:        "reliable_pipeline",
			Description: "Normalize → model → validate (DeerFlow-style control outside the LLM)",
			SubAgents:   []agent.Agent{n1, n2, n3},
		},
	})
}
