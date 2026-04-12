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

package skilltoolset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/model"
	"google.golang.org/adk/skills"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

const defaultSkillSystemInstruction = "" +
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

// SkillToolset provides LLM tool access to a collection of skills backed by a skill.Source.
// Skills are loaded on demand — no data is held in memory at construction time.
type SkillToolset struct {
	source     skill.Source
	skillsRoot string // OS path to the skills root directory; used only for script execution.
	executor   code_executors.CodeExecutor
	tools      []tool.Tool
}

// NewSkillToolset creates a SkillToolset from any skill.Source implementation.
// skillsRoot is the OS filesystem path to the directory containing skill subdirectories;
// it is only used when executing scripts (run_skill_script). Pass an empty string if
// script execution is not needed.
func NewSkillToolset(source skill.Source, skillsRoot string, executor code_executors.CodeExecutor) *SkillToolset {
	st := &SkillToolset{
		source:     source,
		skillsRoot: skillsRoot,
		executor:   executor,
	}
	st.tools = []tool.Tool{
		st.listSkillsTool(),
		st.loadSkillTool(),
		st.loadSkillResourceTool(),
		st.runSkillScriptTool(),
	}
	return st
}

// NewFileSystemSkillToolset creates a SkillToolset backed by a directory on disk.
// The directory must contain skill subdirectories, each with a SKILL.md file.
// This is the standard constructor for production use.
func NewFileSystemSkillToolset(dir string, executor code_executors.CodeExecutor) (*SkillToolset, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve skills dir %q: %w", dir, err)
	}
	src := skill.NewFileSystemSource(os.DirFS(abs))
	return NewSkillToolset(src, abs, executor), nil
}

func (s *SkillToolset) Name() string {
	return "SkillToolset"
}

func (s *SkillToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	return s.tools, nil
}

// ProcessRequest injects the skill list and usage instructions into the system prompt.
func (s *SkillToolset) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	fms, err := s.source.ListFrontmatters(ctx)
	if err != nil {
		return fmt.Errorf("list skills for system instruction: %w", err)
	}
	skillXML := skills.FormatSkillsAsXML(fms)
	instruction := strings.Join([]string{defaultSkillSystemInstruction, skillXML}, "\n\n")
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	if req.Config.SystemInstruction == nil {
		req.Config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: instruction}},
			Role:  "user",
		}
	} else {
		req.Config.SystemInstruction.Parts = append(req.Config.SystemInstruction.Parts,
			&genai.Part{Text: instruction},
		)
	}
	siJSON, _ := json.Marshal(req.Config.SystemInstruction)
	log.Printf("SkillToolset ProcessRequest SystemInstruction: %s", siJSON)
	return nil
}

// toolCtx returns a non-nil context.Context from a tool.Context, falling back to
// context.Background() when ctx is nil (e.g. in unit tests).
func toolCtx(ctx tool.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// ── list_skills ──────────────────────────────────────────────────────────────

type listSkillsArgs struct{}

func (s *SkillToolset) listSkillsToolHandler(ctx tool.Context, _ listSkillsArgs) (map[string]any, error) {
	fms, err := s.source.ListFrontmatters(toolCtx(ctx))
	if err != nil {
		return map[string]any{"error": err.Error(), "error_code": "LIST_ERROR"}, nil
	}
	xml := skills.FormatSkillsAsXML(fms)
	return map[string]any{"result": xml}, nil
}

func (s *SkillToolset) listSkillsTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "list_skills",
		Description: "Lists all available skills with their names and descriptions.",
	}, s.listSkillsToolHandler)
	return t
}

// ── load_skill ───────────────────────────────────────────────────────────────

type loadSkillArgs struct {
	Name      string `json:"name,omitempty"       jsonschema:"The skill id to load (preferred)."`
	Skill     string `json:"skill,omitempty"      jsonschema:"Alias for name (same as the skill id)."`
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
	c := toolCtx(ctx)

	fm, err := s.source.LoadFrontmatter(c, id)
	if err != nil {
		if isSkillNotFound(err) {
			return s.skillNotFoundPayload(c, id), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	instructions, err := s.source.LoadInstructions(c, id)
	if err != nil {
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	fmJSON, _ := json.Marshal(fm)
	return map[string]any{
		"skill_name":   fm.Name,
		"instructions": instructions,
		"frontmatter":  string(fmJSON),
	}, nil
}

func (s *SkillToolset) loadSkillTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "load_skill",
		Description: "Loads the SKILL.md instructions for a given skill.",
	}, s.loadSkillToolHandler)
	return t
}

// ── load_skill_resource ──────────────────────────────────────────────────────

type loadSkillResourceArgs struct {
	SkillName    string `json:"skill_name"              jsonschema:"The name of the skill."`
	Path         string `json:"path"                    jsonschema:"The relative path to the resource (e.g., 'references/x.md', 'assets/template.txt', 'scripts/setup.sh')."`
	ResourcePath string `json:"resource_path,omitempty" jsonschema:"Alias for path."`
}

func (s *SkillToolset) loadSkillResourceToolHandler(ctx tool.Context, args loadSkillResourceArgs) (map[string]any, error) {
	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	c := toolCtx(ctx)

	// Validate skill exists before path processing so we give a good error.
	if _, err := s.source.LoadFrontmatter(c, skillName); err != nil {
		if isSkillNotFound(err) {
			return s.skillNotFoundPayload(c, skillName), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}

	resPath := strings.TrimSpace(args.Path)
	if resPath == "" {
		resPath = strings.TrimSpace(args.ResourcePath)
	}
	if resPath == "" {
		return s.emptyResourcePathPayload(c, skillName), nil
	}

	trialPath := normalizeBundledPath(resPath)
	trialPath, hadRedundant := collapseDoublePrefixes(trialPath)
	trialPath, aliasFrom := applyWrongPrefixAlias(trialPath)

	if isOnlySkillMDPath(trialPath) {
		return useLoadSkillPayload(skillName), nil
	}

	if pre, key, hasPre := splitVirtualPrefix(trialPath); hasPre {
		if isUnsafeVirtualKey(key) || key == "" {
			return s.junkResourceKeyPayload(c, skillName, pre), nil
		}

		rc, err := s.source.LoadResource(c, skillName, trialPath)
		if err != nil {
			if isResourceNotFound(err) {
				return s.resourceNotFoundPayload(c, skillName, trialPath, pre, key), nil
			}
			return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
		}
		defer rc.Close()

		content, err := io.ReadAll(rc)
		if err != nil {
			return map[string]any{"error": err.Error(), "error_code": "READ_ERROR"}, nil
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

	return s.missingPrefixPayload(c, skillName, trialPath), nil
}

func (s *SkillToolset) loadSkillResourceTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "load_skill_resource",
		Description: "Loads a resource file (from references/, assets/, or scripts/) from within a skill.",
	}, s.loadSkillResourceToolHandler)
	return t
}

// ── run_skill_script ─────────────────────────────────────────────────────────

type runSkillScriptArgs struct {
	SkillName  string   `json:"skill_name"            jsonschema:"The name of the skill."`
	ScriptPath string   `json:"script_path,omitempty" jsonschema:"Relative path to the script under the skill (e.g. scripts/discover-environment.sh)."`
	Script     string   `json:"script,omitempty"      jsonschema:"Alias for script_path."`
	Args       []string `json:"args_list,omitempty"   jsonschema:"Optional argv tokens for the script."`
	ArgsLine   string   `json:"args,omitempty"        jsonschema:"Alias for args_list: space-separated arguments as a single string."`
}

func (s *SkillToolset) runSkillScriptToolHandler(ctx tool.Context, args runSkillScriptArgs) (map[string]any, error) {
	scriptPath := strings.TrimSpace(args.ScriptPath)
	if scriptPath == "" {
		scriptPath = strings.TrimSpace(args.Script)
	}
	var execArgs []string
	if len(args.Args) > 0 {
		execArgs = append(execArgs, args.Args...)
	} else if line := strings.TrimSpace(args.ArgsLine); line != "" {
		execArgs = append(execArgs, strings.Fields(line)...)
	}

	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	if scriptPath == "" {
		return map[string]any{"error": "Script path is required (use script_path or script).", "error_code": "MISSING_SCRIPT_PATH"}, nil
	}
	c := toolCtx(ctx)

	// Validate skill exists.
	if _, err := s.source.LoadFrontmatter(c, skillName); err != nil {
		if isSkillNotFound(err) {
			return s.skillNotFoundPayload(c, skillName), nil
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

	// Verify the script exists via the source before attempting OS execution.
	rc, err := s.source.LoadResource(c, skillName, canonical)
	if err != nil {
		if isResourceNotFound(err) {
			return s.scriptNotFoundPayload(c, skillName, canonical, key), nil
		}
		return map[string]any{"error": err.Error(), "error_code": "LOAD_ERROR"}, nil
	}
	_ = rc.Close()

	if s.executor == nil {
		return map[string]any{
			"error":      "No code executor configured. A code executor is required to run scripts.",
			"error_code": "NO_CODE_EXECUTOR",
		}, nil
	}
	if s.skillsRoot == "" {
		return map[string]any{
			"error":      "No skills root directory configured. A skillsRoot is required to run scripts.",
			"error_code": "NO_SKILLS_ROOT",
		}, nil
	}

	argsStr, _ := json.Marshal(args)
	log.Printf("runSkillScriptToolHandler args: %s", argsStr)

	osScriptPath := filepath.Join(s.skillsRoot, skillName, "scripts", filepath.FromSlash(key))
	invID := ""
	if ctx != nil {
		invID = ctx.InvocationID()
	}
	result, err := s.executor.ExecuteCode(nil, code_executors.CodeExecutionInput{
		Args:        execArgs,
		ScriptPath:  osScriptPath,
		InputFiles:  nil,
		ExecutionID: invID,
	})
	resultStr, _ := json.Marshal(result)
	log.Printf("codeExecutor result: %s", resultStr)
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

func (s *SkillToolset) runSkillScriptTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "run_skill_script",
		Description: "Executes a script from a skill's scripts/ directory. Prefer skill_name + script_path (or alias script) + optional args_list or a single args string.",
	}, s.runSkillScriptToolHandler)
	return t
}
