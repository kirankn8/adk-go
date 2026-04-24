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
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

// RunSkillScriptArgs accepts the script location under either script_path or
// script, and argv under either args_list or a single args string.
type RunSkillScriptArgs struct {
	SkillName  string   `json:"skill_name"            jsonschema:"The name of the skill."`
	ScriptPath string   `json:"script_path,omitempty" jsonschema:"Relative path to the script under the skill (e.g. scripts/discover-environment.sh)."`
	Script     string   `json:"script,omitempty"      jsonschema:"Alias for script_path."`
	ArgsList   []string `json:"args_list,omitempty"   jsonschema:"Optional argv tokens for the script."`
	Args       string   `json:"args,omitempty"        jsonschema:"Alias for args_list: space-separated arguments as a single string."`
}

// RunSkillScript creates a tool.Tool that executes a script from a skill's
// scripts/ directory via the provided code executor. skillsRoot is the OS
// filesystem path containing skill subdirectories; it is resolved alongside
// the source's logical path when invoking the executor.
func RunSkillScript(source skill.Source, skillsRoot string, executor code_executors.CodeExecutor) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "run_skill_script",
			Description: "Executes a script from a skill's scripts/ directory. Prefer skill_name + script_path (or alias script) + optional args_list or a single args string.",
		},
		func(ctx tool.Context, args RunSkillScriptArgs) (map[string]any, error) {
			return runSkillScript(ctx, args, source, skillsRoot, executor)
		},
	)
}

func runSkillScript(ctx tool.Context, args RunSkillScriptArgs, source skill.Source, skillsRoot string, executor code_executors.CodeExecutor) (map[string]any, error) {
	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	scriptPath := strings.TrimSpace(args.ScriptPath)
	if scriptPath == "" {
		scriptPath = strings.TrimSpace(args.Script)
	}
	if scriptPath == "" {
		return map[string]any{"error": "Script path is required (use script_path or script).", "error_code": "MISSING_SCRIPT_PATH"}, nil
	}
	var execArgs []string
	if len(args.ArgsList) > 0 {
		execArgs = append(execArgs, args.ArgsList...)
	} else if line := strings.TrimSpace(args.Args); line != "" {
		execArgs = append(execArgs, strings.Fields(line)...)
	}

	c := toolCtx(ctx)

	if _, err := source.LoadFrontmatter(c, skillName); err != nil {
		if isSkillNotFound(err) {
			return skillNotFoundPayload(c, source, skillName), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	sp := normalizeBundledPath(scriptPath)
	sp, redundant := collapseDoublePrefixes(sp)

	if isOnlySkillMDPath(sp) {
		return useLoadSkillPayload(skillName), nil
	}
	if isDocLikeForRunScript(sp) {
		return useLoadSkillResourcePayload(skillName, sp), nil
	}

	key, errPayload := secureScriptKey(sp)
	if errPayload != nil {
		return errPayload, nil
	}

	canonical := "scripts/" + key

	rc, err := source.LoadResource(c, skillName, canonical)
	if err != nil {
		if isResourceNotFound(err) {
			return scriptNotFoundPayload(c, source, skillName, canonical, key), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}
	_ = rc.Close()

	if executor == nil {
		return map[string]any{
			"error":      "No code executor configured. A code executor is required to run scripts.",
			"error_code": "NO_CODE_EXECUTOR",
		}, nil
	}
	if skillsRoot == "" {
		return map[string]any{
			"error":      "No skills root directory configured. A skillsRoot is required to run scripts.",
			"error_code": "NO_SKILLS_ROOT",
		}, nil
	}

	osScriptPath := filepath.Join(skillsRoot, skillName, "scripts", filepath.FromSlash(key))
	invID := ""
	if ctx != nil {
		invID = ctx.InvocationID()
	}
	result, err := executor.ExecuteCode(nil, code_executors.CodeExecutionInput{
		Args:        execArgs,
		ScriptPath:  osScriptPath,
		InputFiles:  nil,
		ExecutionID: invID,
	})
	if err != nil {
		return map[string]any{
			"error":      fmt.Sprintf("Failed to execute script '%s':\n%s", canonical, err.Error()),
			"error_code": "EXECUTION_ERROR",
		}, nil
	}

	out := map[string]any{
		"skill_name":  skillName,
		"script_path": canonical,
		"stdout":      result.StdOut,
		"stderr":      result.StdErr,
		"status":      "success",
	}
	if redundant {
		out["corrected_path"] = canonical
	}
	return out, nil
}
