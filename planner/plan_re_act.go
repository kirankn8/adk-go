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

package planner

import (
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Tag constants match Python google.adk.planners.plan_re_act_planner.
const (
	PlanningTag    = "/*PLANNING*/"
	ReplanningTag  = "/*REPLANNING*/"
	ReasoningTag   = "/*REASONING*/"
	ActionTag      = "/*ACTION*/"
	FinalAnswerTag = "/*FINAL_ANSWER*/"
)

// PlanReActPlanner constrains the model to emit a natural-language plan before tool use,
// matching Python's PlanReActPlanner (no built-in thinking features required).
type PlanReActPlanner struct{}

// NewPlanReActPlanner returns a new Plan-Re-Act planner instance.
func NewPlanReActPlanner() *PlanReActPlanner {
	return &PlanReActPlanner{}
}

// ApplyThinkingConfig implements Planner.
func (*PlanReActPlanner) ApplyThinkingConfig(*model.LLMRequest) {}

// BuildPlanningInstruction implements Planner.
func (*PlanReActPlanner) BuildPlanningInstruction(agent.ReadonlyContext, *model.LLMRequest) (string, error) {
	return buildNLPlannerInstruction(), nil
}

// ProcessPlanningResponse implements Planner, following Python PlanReActPlanner.process_planning_response
// (including the post-loop index behavior for mixed text / function-call parts).
func (*PlanReActPlanner) ProcessPlanningResponse(_ agent.CallbackContext, responseParts []*genai.Part) ([]*genai.Part, error) {
	if len(responseParts) == 0 {
		return nil, nil
	}
	var preserved []*genai.Part
	firstFCIndex := -1
	i := 0
	for i = 0; i < len(responseParts); i++ {
		part := responseParts[i]
		if part.FunctionCall != nil {
			if part.FunctionCall.Name == "" {
				continue
			}
			preserved = append(preserved, part)
			firstFCIndex = i
			break
		}
	}
	handleIdx := i
	if firstFCIndex < 0 {
		handleIdx = len(responseParts) - 1
	}
	handleNonFunctionCallParts(responseParts[handleIdx], &preserved)
	if firstFCIndex > 0 {
		for j := firstFCIndex + 1; j < len(responseParts); j++ {
			if responseParts[j].FunctionCall != nil {
				preserved = append(preserved, responseParts[j])
			} else {
				break
			}
		}
	}
	return preserved, nil
}

func splitByLastPattern(text, separator string) (beforeWithSep, after string) {
	idx := strings.LastIndex(text, separator)
	if idx == -1 {
		return text, ""
	}
	end := idx + len(separator)
	return text[:end], text[end:]
}

func handleNonFunctionCallParts(responsePart *genai.Part, preserved *[]*genai.Part) {
	if responsePart == nil {
		return
	}
	text := responsePart.Text
	if text == "" {
		return
	}
	if strings.Contains(text, FinalAnswerTag) {
		reasoningText, finalAnswerText := splitByLastPattern(text, FinalAnswerTag)
		if reasoningText != "" {
			rp := &genai.Part{Text: reasoningText}
			markAsThought(rp)
			*preserved = append(*preserved, rp)
		}
		if finalAnswerText != "" {
			*preserved = append(*preserved, &genai.Part{Text: finalAnswerText})
		}
		return
	}
	if strings.HasPrefix(text, PlanningTag) ||
		strings.HasPrefix(text, ReasoningTag) ||
		strings.HasPrefix(text, ActionTag) ||
		strings.HasPrefix(text, ReplanningTag) {
		cp := *responsePart
		markAsThought(&cp)
		*preserved = append(*preserved, &cp)
		return
	}
	*preserved = append(*preserved, responsePart)
}

func markAsThought(responsePart *genai.Part) {
	if responsePart != nil && responsePart.Text != "" {
		responsePart.Thought = true
	}
}

func buildNLPlannerInstruction() string {
	highLevelPreamble := `
When answering the question, try to leverage the available tools to gather the information instead of your memorized knowledge.

Follow this process when answering the question: (1) first come up with a plan in natural language text format; (2) Then use tools to execute the plan and provide reasoning between tool code snippets to make a summary of current state and next step. Tool code snippets and reasoning should be interleaved with each other. (3) In the end, return one final answer.

Follow this format when answering the question: (1) The planning part should be under ` + PlanningTag + `. (2) The tool code snippets should be under ` + ActionTag + `, and the reasoning parts should be under ` + ReasoningTag + `. (3) The final answer part should be under ` + FinalAnswerTag + `.
`

	planningPreamble := `
Below are the requirements for the planning:
The plan is made to answer the user query if following the plan. The plan is coherent and covers all aspects of information from user query, and only involves the tools that are accessible by the agent. The plan contains the decomposed steps as a numbered list where each step should use one or multiple available tools. By reading the plan, you can intuitively know which tools to trigger or what actions to take.
If the initial plan cannot be successfully executed, you should learn from previous execution results and revise your plan. The revised plan should be under ` + ReplanningTag + `. Then use tools to follow the new plan.
`

	reasoningPreamble := `
Below are the requirements for the reasoning:
The reasoning makes a summary of the current trajectory based on the user query and tool outputs. Based on the tool outputs and plan, the reasoning also comes up with instructions to the next steps, making the trajectory closer to the final answer.
`

	finalAnswerPreamble := `
Below are the requirements for the final answer:
The final answer should be precise and follow query formatting requirements. Some queries may not be answerable with the available tools and information. In those cases, inform the user why you cannot process their query and ask for more information.
`

	toolCodePreamble := `
Below are the requirements for tool use:

**Custom Tools:** The available tools are described in the context and can be directly used.
- You cannot use any parameters or fields that are not explicitly defined in the tool declarations in the context.
- Tool calls should be directly relevant to the user query and reasoning steps.
- NEVER invent tool names or parameters that are not provided in the context.
`

	userInputPreamble := `
VERY IMPORTANT instruction that you MUST follow in addition to the above instructions:

You should ask for clarification if you need more information to answer the question.
You should prefer using the information available in the context instead of repeated tool use.
`

	return strings.Join([]string{
		strings.TrimSpace(highLevelPreamble),
		strings.TrimSpace(planningPreamble),
		strings.TrimSpace(reasoningPreamble),
		strings.TrimSpace(finalAnswerPreamble),
		strings.TrimSpace(toolCodePreamble),
		strings.TrimSpace(userInputPreamble),
	}, "\n\n")
}
