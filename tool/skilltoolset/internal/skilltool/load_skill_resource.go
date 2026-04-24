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

package skilltool

import (
	"io"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

const maxResourceSize = 10 * 1024 * 1024 // 10 MiB

// LoadSkillResourceArgs accepts the resource location under either path or
// resource_path. path is preferred for brevity; resource_path is tolerated for
// LLMs that reach for the longer name.
type LoadSkillResourceArgs struct {
	SkillName    string `json:"skill_name"              jsonschema:"The name of the skill."`
	Path         string `json:"path,omitempty"          jsonschema:"The relative path to the resource (e.g., 'references/x.md', 'assets/template.txt', 'scripts/setup.sh')."`
	ResourcePath string `json:"resource_path,omitempty" jsonschema:"Alias for path."`
}

// LoadSkillResource creates a tool.Tool that loads a resource file, with path
// normalization and did_you_mean enrichment on error paths.
func LoadSkillResource(source skill.Source) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "load_skill_resource",
			Description: "Loads a resource file (from references/, assets/, or scripts/) from within a skill.",
		},
		func(ctx tool.Context, args LoadSkillResourceArgs) (map[string]any, error) {
			return loadSkillResource(ctx, args, source)
		},
	)
}

func loadSkillResource(ctx tool.Context, args LoadSkillResourceArgs, source skill.Source) (map[string]any, error) {
	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	c := toolCtx(ctx)

	if _, err := source.LoadFrontmatter(c, skillName); err != nil {
		if isSkillNotFound(err) {
			return skillNotFoundPayload(c, source, skillName), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	resPath := strings.TrimSpace(args.Path)
	if resPath == "" {
		resPath = strings.TrimSpace(args.ResourcePath)
	}
	if resPath == "" {
		return emptyResourcePathPayload(c, source, skillName), nil
	}

	trialPath := normalizeBundledPath(resPath)
	trialPath, hadRedundant := collapseDoublePrefixes(trialPath)
	trialPath, aliasFrom := applyWrongPrefixAlias(trialPath)

	if isOnlySkillMDPath(trialPath) {
		return useLoadSkillPayload(skillName), nil
	}

	pre, key, hasPre := splitVirtualPrefix(trialPath)
	if !hasPre {
		return missingPrefixPayload(c, source, skillName, trialPath), nil
	}
	if isUnsafeVirtualKey(key) || key == "" {
		return junkResourceKeyPayload(c, source, skillName, pre), nil
	}

	rc, err := source.LoadResource(c, skillName, trialPath)
	if err != nil {
		if isResourceNotFound(err) {
			return resourceNotFoundPayload(c, source, skillName, trialPath, pre, key), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}
	defer func() { _ = rc.Close() }()

	content, err := io.ReadAll(io.LimitReader(rc, maxResourceSize+1))
	if err != nil {
		return map[string]any{"error": err.Error(), "error_code": "READ_ERROR"}, nil
	}
	if int64(len(content)) > maxResourceSize {
		return map[string]any{
			"error":      "Resource exceeds the maximum size limit.",
			"error_code": "RESOURCE_TOO_LARGE",
		}, nil
	}

	out := map[string]any{
		"skill_name": skillName,
		"path":       trialPath,
		"content":    string(content),
	}
	if aliasFrom != "" || hadRedundant {
		out["corrected_path"] = trialPath
	}
	return out, nil
}
