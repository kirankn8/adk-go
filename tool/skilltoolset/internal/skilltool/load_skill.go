// Copyright 2026 Google LLC
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

// Package skilltool provides the standard tools for the skill toolset.
package skilltool

import (
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

// LoadSkillArgs accepts the skill id under any of three keys. The primary
// spelling is name; skill and skill_name are aliases tolerated for LLMs that
// reach for the other names. Exactly one must be non-empty.
type LoadSkillArgs struct {
	Name      string `json:"name,omitempty"       jsonschema:"The skill id to load (preferred)."`
	Skill     string `json:"skill,omitempty"      jsonschema:"Alias for name (same as the skill id)."`
	SkillName string `json:"skill_name,omitempty" jsonschema:"Alias for name (matches load_skill_resource)."`
}

func resolveLoadSkillID(a LoadSkillArgs) string {
	if s := strings.TrimSpace(a.Name); s != "" {
		return s
	}
	if s := strings.TrimSpace(a.Skill); s != "" {
		return s
	}
	return strings.TrimSpace(a.SkillName)
}

// LoadSkill creates a tool.Tool that loads a skill's SKILL.md frontmatter and
// instructions, with did_you_mean enrichment when the skill id is mistyped.
func LoadSkill(source skill.Source) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "load_skill",
			Description: "Loads the SKILL.md instructions for a given skill.",
		},
		func(ctx tool.Context, args LoadSkillArgs) (map[string]any, error) {
			return loadSkill(ctx, args, source)
		},
	)
}

func loadSkill(ctx tool.Context, args LoadSkillArgs, source skill.Source) (map[string]any, error) {
	id := resolveLoadSkillID(args)
	if id == "" {
		return map[string]any{
			"error":      "Skill name is required (use name, skill, or skill_name).",
			"error_code": "MISSING_SKILL_NAME",
		}, nil
	}
	c := toolCtx(ctx)

	fm, err := source.LoadFrontmatter(c, id)
	if err != nil {
		if isSkillNotFound(err) {
			return skillNotFoundPayload(c, source, id), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	instructions, err := source.LoadInstructions(c, id)
	if err != nil {
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	return map[string]any{
		"skill_name":   fm.Name,
		"instructions": instructions,
		"frontmatter":  frontmatterToMap(fm),
	}, nil
}

// frontmatterToMap produces the upstream-compatible JSON object shape for a
// Frontmatter, with empty fields omitted.
func frontmatterToMap(fm *skill.Frontmatter) map[string]any {
	out := map[string]any{
		"name":        fm.Name,
		"description": fm.Description,
	}
	if fm.License != "" {
		out["license"] = fm.License
	}
	if fm.Compatibility != "" {
		out["compatibility"] = fm.Compatibility
	}
	if len(fm.Metadata) > 0 {
		md := make(map[string]any, len(fm.Metadata))
		for k, v := range fm.Metadata {
			md[k] = v
		}
		out["metadata"] = md
	}
	if len(fm.AllowedTools) > 0 {
		at := make([]any, len(fm.AllowedTools))
		for i, v := range fm.AllowedTools {
			at[i] = v
		}
		out["allowed-tools"] = at
	}
	return out
}
