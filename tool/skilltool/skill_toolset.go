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

package skilltool

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/model"
	"google.golang.org/adk/skills"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	DEFAULT_SKILL_SYSTEM_INSTRUCTION = "" +
		"You can use specialized 'skills' to help you with complex tasks. You MUST use the skill tools to interact with these skills.\n\n" +
		"Skills are folders of instructions and resources that extend your capabilities for specialized tasks. Each skill folder contains:\n" +
		"- **SKILL.md** (required): The main instruction file with skill metadata and detailed markdown instructions.\n" +
		"- **references/** (Optional): Additional documentation or examples for skill usage.\n" +
		"- **assets/** (Optional): Templates, scripts or other resources used by the skill.\n" +
		"- **scripts/** (Optional): Executable scripts that can be run via bash.\n\n" +
		"This is very important:\n\n" +
		"1. If a skill seems relevant to the current user query, you MUST use the `load_skill` tool with `name=\"<SKILL_NAME>\"` (or the same value in `skill_name` or `skill`) to read its full instructions before proceeding.\n" +
		"2. Once you have read the instructions, follow them exactly as documented before replying to the user. For example, If the instruction lists multiple steps, please make sure you complete all of them in order.\n" +
		"3. The `load_skill_resource` tool is for viewing files within a skill's directory (e.g., `references/*`, `assets/*`, `scripts/*`). Pass the relative path as `path` (or the same value in `resource_path`). Do NOT use other tools to access these files.\n" +
		"4. Use `run_skill_script` to run scripts from a skill's `scripts/` directory. Use `load_skill_resource` to view script content first if needed.\n"
)

// SkillToolset A toolset for managing and interacting with agent skills.
type SkillToolset struct {
	skills       map[string]*skills.Skill
	tools        []tool.Tool
	codeExecutor code_executors.CodeExecutor
}

func NewSkillToolset(skillList []*skills.Skill, codeExecutor code_executors.CodeExecutor) (*SkillToolset, error) {
	m := make(map[string]*skills.Skill, len(skillList))
	for _, s := range skillList {
		if _, dup := m[s.Name()]; dup {
			return nil, fmt.Errorf("duplicate skill name '%s'", s.Name())
		}
		m[s.Name()] = s
	}
	st := &SkillToolset{
		skills:       m,
		codeExecutor: codeExecutor,
	}
	st.tools = []tool.Tool{
		st.listSkillsTool(),
		st.loadSkillTool(),
		st.loadSkillResourceTool(),
		st.runSkillScriptTool(),
	}
	return st, nil
}

func (s *SkillToolset) Name() string {
	return "SkillToolset"
}

func (s *SkillToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	return s.tools, nil
}

func (s *SkillToolset) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	skillList := s.listSkills()
	skillXML := skills.FormatSkillsAsXML(skillList)
	instruction := []string{DEFAULT_SKILL_SYSTEM_INSTRUCTION, skillXML}
	if req.Config.SystemInstruction == nil {
		req.Config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{
					Text: strings.Join(instruction, "\n\n"),
				},
			},
			Role: "user",
		}
	} else {
		req.Config.SystemInstruction.Parts = append(req.Config.SystemInstruction.Parts,
			&genai.Part{
				Text: strings.Join(instruction, "\n\n"),
			},
		)
	}
	systemInstructionStr, _ := json.Marshal(req.Config.SystemInstruction)
	log.Printf("SkillToolset After ProcessRequest SystemInstruction is %s", systemInstructionStr)
	return nil
}

func (s *SkillToolset) getSkill(name string) (*skills.Skill, bool) {
	sk, ok := s.skills[name]
	return sk, ok
}

func (s *SkillToolset) listSkills() []*skills.Skill {
	out := make([]*skills.Skill, 0, len(s.skills))
	for _, v := range s.skills {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

type listSkillsArgs struct{}

func (s *SkillToolset) listSkillsToolHandler(ctx tool.Context, args listSkillsArgs) (map[string]any, error) {
	xml := skills.FormatSkillsAsXML(s.listSkills())
	return map[string]any{"result": xml}, nil
}

// listSkillsTool Tool to list all available skills.
func (s *SkillToolset) listSkillsTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "list_skills",
		Description: "Lists all available skills with their names and descriptions.",
	}, s.listSkillsToolHandler)
	return t
}

type loadSkillArgs struct {
	Name      string `json:"name,omitempty" jsonschema:"The skill id to load (preferred)."`
	Skill     string `json:"skill,omitempty" jsonschema:"Alias for name (same as the skill id)."`
	SkillName string `json:"skill_name,omitempty" jsonschema:"Alias for name (matches load_skill_resource)."`
}

func resolveLoadSkillID(a loadSkillArgs) string {
	if s := strings.TrimSpace(a.Name); s != "" {
		return s
	}
	if s := strings.TrimSpace(a.Skill); s != "" {
		return s
	}
	return strings.TrimSpace(a.SkillName)
}

func (s *SkillToolset) loadSkillToolHandler(ctx tool.Context, args loadSkillArgs) (map[string]any, error) {
	id := resolveLoadSkillID(args)
	if id == "" {
		return map[string]any{
			"error":      "Skill name is required (use name, skill, or skill_name).",
			"error_code": "MISSING_SKILL_NAME",
		}, nil
	}
	sk, ok := s.getSkill(id)
	if !ok {
		return s.skillNotFoundPayload(id), nil
	}

	return map[string]any{
		"skill_name":   sk.Name(),
		"instructions": sk.Instructions,
		"frontmatter":  sk.Frontmatter.ToJsonString(),
	}, nil
}

// loadSkillTool Tool to load a skill's instructions."""
func (s *SkillToolset) loadSkillTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "load_skill",
		Description: "Loads the SKILL.md instructions for a given skill.",
	}, s.loadSkillToolHandler)
	return t
}

type loadSkillResourceArgs struct {
	SkillName string `json:"skill_name" jsonschema:"The name of the skill."`
	Path      string `json:"path" jsonschema:"The relative path to the resource (e.g., 'references/x.md', 'assets/template.txt', 'scripts/setup.sh')."`
	// ResourcePath is an alias used by some models instead of path.
	ResourcePath string `json:"resource_path,omitempty" jsonschema:"Alias for path."`
}

func (s *SkillToolset) loadSkillResourceToolHandler(ctx tool.Context, args loadSkillResourceArgs) (map[string]any, error) {
	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	sk, ok := s.getSkill(skillName)
	if !ok {
		return s.skillNotFoundPayload(skillName), nil
	}
	resPath := strings.TrimSpace(args.Path)
	if resPath == "" {
		resPath = strings.TrimSpace(args.ResourcePath)
	}
	if resPath == "" {
		return s.emptyResourcePathPayload(sk), nil
	}

	trialPath := normalizeBundledPath(resPath)
	trialPath, hadRedundant := collapseDoublePrefixes(trialPath)
	trialPath, aliasFrom := applyWrongPrefixAlias(trialPath)

	if isOnlySkillMDPath(trialPath) {
		return useLoadSkillPayload(sk.Name()), nil
	}

	if pre, key, hasPre := splitVirtualPrefix(trialPath); hasPre {
		if isUnsafeVirtualKey(key) || key == "" {
			return s.junkResourceKeyPayload(sk, pre), nil
		}
		if content, ok := lookupVirtual(sk, trialPath); ok {
			out := map[string]any{
				"skill_name": sk.Name(),
				"path":       trialPath,
				"content":    content,
			}
			if aliasFrom != "" || hadRedundant {
				out["corrected_path"] = trialPath
			}
			return out, nil
		}
		return s.resourceNotFoundPayload(sk, trialPath, pre, key), nil
	}

	return s.missingPrefixPayload(sk, skillName, trialPath), nil
}

// loadSkillResourceTool Tool to load resources (references, assets, or scripts) from a skill."""
func (s *SkillToolset) loadSkillResourceTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "load_skill_resource",
		Description: "Loads a resource file (from references/, assets/, or scripts/) from within a skill.",
	}, s.loadSkillResourceToolHandler)
	return t
}

type runSkillScriptArgs struct {
	SkillName string `json:"skill_name" jsonschema:"The name of the skill."`
	// ScriptPath is canonical (e.g. scripts/discover-environment.sh).
	ScriptPath string `json:"script_path,omitempty" jsonschema:"Relative path to the script under the skill (e.g. scripts/discover-environment.sh)."`
	// Script is an alias used by some models instead of script_path.
	Script string `json:"script,omitempty" jsonschema:"Alias for script_path."`
	// Args is the canonical list form.
	Args []string `json:"args_list,omitempty" jsonschema:"Optional argv tokens for the script."`
	// ArgsLine is an alias: one string split on spaces (common model output).
	ArgsLine string `json:"args,omitempty" jsonschema:"Alias for args_list: space-separated arguments as a single string."`
}

func (s *SkillToolset) runSkillScriptToolHandler(ctx tool.Context, args runSkillScriptArgs) (map[string]any, error) {
	scriptPath := strings.TrimSpace(args.ScriptPath)
	if scriptPath == "" {
		scriptPath = strings.TrimSpace(args.Script)
	}
	execArgs := append([]string(nil), args.Args...)
	if line := strings.TrimSpace(args.ArgsLine); line != "" {
		execArgs = append(execArgs, strings.Fields(line)...)
	}

	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	if scriptPath == "" {
		return map[string]any{"error": "Script path is required (use script_path or script).", "error_code": "MISSING_SCRIPT_PATH"}, nil
	}
	sk, ok := s.getSkill(skillName)
	if !ok {
		return s.skillNotFoundPayload(skillName), nil
	}

	sp := normalizeBundledPath(scriptPath)
	sp, redundant := collapseDoublePrefixes(sp)

	if isOnlySkillMDPath(sp) {
		return useLoadSkillPayload(sk.Name()), nil
	}

	if isDocLikeForRunScript(sp) {
		return useLoadSkillResourcePayload(sk.Name(), sp), nil
	}

	key, errPayload := secureScriptKeyUnderSkill(sk, sp)
	if errPayload != nil {
		return errPayload, nil
	}

	canonical := "scripts/" + key
	if sk.Resources == nil {
		return s.scriptNotFoundPayload(sk, canonical, key), nil
	}
	if scr, ok := sk.Resources.GetScript(key); !ok || scr == nil {
		return s.scriptNotFoundPayload(sk, canonical, key), nil
	}
	if s.codeExecutor == nil {
		return map[string]any{
			"error":      "No code executor configured. A code executor is required to run scripts.",
			"error_code": "NO_CODE_EXECUTOR",
		}, nil
	}
	argsStr, _ := json.Marshal(args)
	log.Printf("runSkillScriptToolHandler args is %s", string(argsStr))
	codeExecutorResult, err := s.codeExecutor.ExecuteCode(nil, code_executors.CodeExecutionInput{
		Args:        execArgs,
		ScriptPath:  filepath.Join(sk.GetSkillPath(), "scripts", key),
		InputFiles:  nil,
		ExecutionID: ctx.InvocationID(),
	})
	resultStr, _ := json.Marshal(codeExecutorResult)
	log.Printf("codeExecutor result is %s", string(resultStr))
	if err != nil {
		return map[string]any{
			"error":      fmt.Sprintf("Failed to execute script '%s':\n%s", canonical, err.Error()),
			"error_code": "EXECUTION_ERROR",
		}, nil
	}
	status := "success"

	out := map[string]any{
		"skill_name":  sk.Name(),
		"script_path": canonical,
		"stdout":      codeExecutorResult.StdOut,
		"stderr":      codeExecutorResult.StdErr,
		"status":      status,
	}
	if redundant {
		out["corrected_path"] = canonical
	}
	return out, nil
}

// runSkillScriptTool Tool to execute scripts from a skill's scripts/ directory."""
func (s *SkillToolset) runSkillScriptTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "run_skill_script",
		Description: "Executes a script from a skill's scripts/ directory. Prefer skill_name + script_path (or alias script) + optional args_list or a single args string.",
	}, s.runSkillScriptToolHandler)
	return t
}
