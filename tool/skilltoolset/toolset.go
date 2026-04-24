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

package skilltoolset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/internal/utils"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/skilltoolset/internal/skilltool"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

const (
	defaultName                   string = "SkillToolset"
	defaultSkillSystemInstruction string = `You can use specialized 'skills' to help you with complex tasks. You MUST use the skill tools to interact with these skills.

Skills are folders of instructions and resources that extend your capabilities for specialized tasks. Each skill folder contains:
- **SKILL.md** (required): The main instruction file with skill metadata and detailed markdown instructions.
- **references/** (Optional): Additional documentation or examples for skill usage.
- **assets/** (Optional): Templates, scripts or other resources used by the skill.
- **scripts/** (Optional): Executable scripts that can be run via bash.

This is very important:

` +
		"1. If a skill seems relevant to the current user query, you MUST use the `load_skill` tool with `name=\"<SKILL_NAME>\"` (or the same value in `skill_name` or `skill`) to read its full instructions before proceeding.\n" +
		"2. Once you have read the instructions, follow them exactly as documented before replying to the user. For example, If the instruction lists multiple steps, please make sure you complete all of them in order.\n" +
		"3. The `load_skill_resource` tool is for viewing files within a skill's directory (e.g., `references/*`, `assets/*`, `scripts/*`). Pass the relative path as `path` (or the same value in `resource_path`). Do NOT use other tools to access these files.\n" +
		"4. Use `run_skill_script` to run scripts from a skill's `scripts/` directory. Use `load_skill_resource` to view script content first if needed.\n"
)

// Config holds the configuration for creating a Skill Toolset.
type Config struct {
	Source skill.Source
	// Optional name of the toolset. If empty, default name will be used.
	Name string
	// Optional system instruction. If empty, default instruction will be used.
	SystemInstruction string
	// Optional executor for run_skill_script. When nil, the run_skill_script
	// tool is not exposed.
	Executor code_executors.CodeExecutor
	// Optional OS filesystem path containing skill subdirectories. Required
	// alongside Executor for run_skill_script to resolve scripts on disk.
	SkillsRoot string
}

// SkillToolset provides a toolset for skills.
type SkillToolset struct {
	name              string
	tools             []tool.Tool
	source            skill.Source
	systemInstruction string
}

// New creates a new Skill Toolset based on the provided configuration.
func New(ctx context.Context, cfg Config) (*SkillToolset, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("skill source must be provided")
	}
	name := defaultName
	if cfg.Name != "" {
		name = cfg.Name
	}
	instruction := defaultSkillSystemInstruction
	if cfg.SystemInstruction != "" {
		instruction = cfg.SystemInstruction
	}
	listTool, err := skilltool.ListSkills(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("create list skills tool: %w", err)
	}
	loadTool, err := skilltool.LoadSkill(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("create load skill tool: %w", err)
	}
	loadResourceTool, err := skilltool.LoadSkillResource(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("create load skill resource tool: %w", err)
	}
	tools := []tool.Tool{listTool, loadTool, loadResourceTool}
	if cfg.Executor != nil && cfg.SkillsRoot != "" {
		runScriptTool, err := skilltool.RunSkillScript(cfg.Source, cfg.SkillsRoot, cfg.Executor)
		if err != nil {
			return nil, fmt.Errorf("create run skill script tool: %w", err)
		}
		tools = append(tools, runScriptTool)
	}
	return &SkillToolset{
		name:              name,
		tools:             tools,
		source:            cfg.Source,
		systemInstruction: instruction,
	}, nil
}

// NewFileSystem creates a SkillToolset backed by a directory on disk. Each
// subdirectory of dir must contain a SKILL.md file. When executor is non-nil,
// the run_skill_script tool is enabled and resolves scripts under dir.
func NewFileSystem(ctx context.Context, dir string, executor code_executors.CodeExecutor) (*SkillToolset, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve skills dir %q: %w", dir, err)
	}
	return New(ctx, Config{
		Source:     skill.NewFileSystemSource(os.DirFS(abs)),
		Executor:   executor,
		SkillsRoot: abs,
	})
}

// Name implements tool.Toolset. Returns the name of the toolset.
func (ts *SkillToolset) Name() string { return ts.name }

// Tools implements tool.Toolset. It returns a list of tools agent can use to
// interact with skills.
func (ts *SkillToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) { return ts.tools, nil }

// ProcessRequest implements toolinternal.RequestProcessor. It attaches
// the list of available skills and the system instruction explaining to the
// agent what it can do with these skills.
func (ts *SkillToolset) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	skills, err := ts.source.ListFrontmatters(ctx)
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		return nil
	}
	utils.AppendInstructions(req, ts.systemInstruction, skilltool.SkillsToXML(skills))
	return nil
}
