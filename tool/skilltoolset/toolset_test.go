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
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/skills"
	"google.golang.org/adk/tool"
)

// mockSkillDefs holds the in-memory skill definitions used to seed test directories.
var mockSkillDefs = map[string]*skills.Skill{
	"multiplication-calculator": {
		Frontmatter: &skills.Frontmatter{
			Name:        "multiplication-calculator",
			Description: "提供乘法数值计算功能。当需要执行乘法运算任务时使用此技能。",
		},
		Instructions: "---\nname: multiplication-calculator\ndescription: 提供乘法数值计算功能。当需要执行乘法运算任务时使用此技能。\n---\n\n# 乘法数值计算器\n\n## 概述\n\n乘法数值计算器技能提供简单的数字相乘能力。该技能包含Python脚本，可用于执行乘法计算任务。\n\n## 快速开始\n\n要使用乘法计算功能，可以直接调用提供的Python脚本：\n\n```bash\n# 运行演示脚本\npython scripts/multiply.py <num1> <num2> ... <numn>\n```",
		Resources: &skills.Resources{
			Scripts: map[string]*skills.Script{
				"multiply.py": {
					Src: `#!/usr/bin/env python3
import sys

"""
乘法数值计算脚本
提供多种乘法运算功能
"""


def multiply_list(numbers):
    """
    列表中的所有数字相乘

    Args:
        numbers (list): 数字列表

    Returns:
        float: 所有数字的乘积

    Raises:
        ValueError: 如果列表为空
    """
    if not numbers:
        raise ValueError("数字列表不能为空")

    result = 1
    for num in numbers:
        result *= num
    return result


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python multiply.py <num1> <num2> ...<numn>")
        sys.exit(1)
    nums = []
    for n in sys.argv[1:]:
        nums.append(float(n))
    out = multiply_list(nums)
    print(out)
`,
				},
			},
		},
	},
}

// createTestToolset writes mock skills to a temp directory and returns a
// FileSystem-backed SkillToolset. This exercises the full lazy-loading path.
func createTestToolset(t *testing.T, executor code_executors.CodeExecutor) *SkillToolset {
	t.Helper()
	tmpDir := t.TempDir()
	for _, sk := range mockSkillDefs {
		if err := sk.WriteSkill(tmpDir); err != nil {
			t.Fatalf("write skill %s: %v", sk.Name(), err)
		}
	}
	ts, err := NewFileSystemSkillToolset(tmpDir, executor)
	if err != nil {
		t.Fatalf("NewFileSystemSkillToolset: %v", err)
	}
	return ts
}

type mockToolContext struct {
	tool.Context
}

func (m *mockToolContext) InvocationID() string { return "test-invocation-id" }

func TestListSkillsTool(t *testing.T) {
	ts := createTestToolset(t, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))

	listTool := ts.listSkillsTool()
	if listTool.Name() != "list_skills" {
		t.Errorf("list tool name: got %q want list_skills", listTool.Name())
	}

	result, err := ts.listSkillsToolHandler(nil, listSkillsArgs{})
	if err != nil {
		t.Fatalf("listSkillsToolHandler: %v", err)
	}
	xmlResult, ok := result["result"].(string)
	if !ok {
		t.Fatal("result is not a string")
	}
	if !strings.Contains(xmlResult, "multiplication-calculator") {
		t.Errorf("xml should contain multiplication-calculator: %q", xmlResult)
	}
	if !strings.Contains(xmlResult, "提供乘法数值计算功能。当需要执行乘法运算任务时使用此技能。") {
		t.Error("xml should contain skill description")
	}
}

func TestLoadSkillTool(t *testing.T) {
	ts := createTestToolset(t, nil)

	loadTool := ts.loadSkillTool()
	if loadTool.Name() != "load_skill" {
		t.Errorf("load tool name: got %q want load_skill", loadTool.Name())
	}

	// Missing name.
	result, err := ts.loadSkillToolHandler(nil, loadSkillArgs{})
	if err != nil {
		t.Fatalf("loadSkillToolHandler missing name: %v", err)
	}
	if result["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("missing name error_code: got %v", result["error_code"])
	}

	// Unknown skill.
	result, err = ts.loadSkillToolHandler(nil, loadSkillArgs{Name: "unknown-skill"})
	if err != nil {
		t.Fatalf("loadSkillToolHandler unknown: %v", err)
	}
	if result["error_code"] != "SKILL_NOT_FOUND" {
		t.Errorf("unknown skill error_code: got %v", result["error_code"])
	}

	// Success via name field.
	result, err = ts.loadSkillToolHandler(nil, loadSkillArgs{Name: "multiplication-calculator"})
	if err != nil {
		t.Fatalf("loadSkillToolHandler success: %v", err)
	}
	if result["skill_name"] != "multiplication-calculator" {
		t.Errorf("skill_name: got %v", result["skill_name"])
	}
	if result["instructions"] == "" {
		t.Error("instructions should be non-empty")
	}
	if result["frontmatter"] == "" {
		t.Error("frontmatter should be non-empty")
	}

	// Success via skill alias.
	result, err = ts.loadSkillToolHandler(nil, loadSkillArgs{Skill: "multiplication-calculator"})
	if err != nil {
		t.Fatalf("loadSkillToolHandler skill alias: %v", err)
	}
	if result["skill_name"] != "multiplication-calculator" {
		t.Errorf("skill alias skill_name: got %v", result["skill_name"])
	}
}

func TestLoadSkillResourceTool(t *testing.T) {
	ts := createTestToolset(t, nil)

	resourceTool := ts.loadSkillResourceTool()
	if resourceTool.Name() != "load_skill_resource" {
		t.Errorf("resource tool name: got %q", resourceTool.Name())
	}

	// Missing skill name.
	result, err := ts.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler: %v", err)
	}
	if result["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("error_code: got %v", result["error_code"])
	}

	// Missing resource path.
	result, err = ts.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{SkillName: "multiplication-calculator"})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler: %v", err)
	}
	if result["error_code"] != "MISSING_RESOURCE_PATH" {
		t.Errorf("error_code: got %v", result["error_code"])
	}

	// Success via path field.
	result, err = ts.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{
		SkillName: "multiplication-calculator",
		Path:      "scripts/multiply.py",
	})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler success: %v", err)
	}
	if result["skill_name"] != "multiplication-calculator" {
		t.Errorf("skill_name: got %v", result["skill_name"])
	}
	if result["path"] != "scripts/multiply.py" {
		t.Errorf("path: got %v", result["path"])
	}
	wantContent := mockSkillDefs["multiplication-calculator"].Resources.Scripts["multiply.py"].Src
	if result["content"] != wantContent {
		t.Errorf("content mismatch: got %q want %q", result["content"], wantContent)
	}

	// Success via resource_path alias.
	result, err = ts.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{
		SkillName:    "multiplication-calculator",
		ResourcePath: "scripts/multiply.py",
	})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler resource_path alias: %v", err)
	}
	if result["path"] != "scripts/multiply.py" {
		t.Errorf("resource_path alias path: got %v", result["path"])
	}
	if result["content"] != wantContent {
		t.Error("resource_path alias content mismatch")
	}

	// Not found.
	result, err = ts.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{
		SkillName: "multiplication-calculator",
		Path:      "scripts/unknown.py",
	})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler not found: %v", err)
	}
	if result["error_code"] != "RESOURCE_NOT_FOUND" {
		t.Errorf("error_code: got %v", result["error_code"])
	}
}

func TestRunSkillScriptTool(t *testing.T) {
	ts := createTestToolset(t, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))

	runTool := ts.runSkillScriptTool()
	if runTool.Name() != "run_skill_script" {
		t.Errorf("run tool name: got %q", runTool.Name())
	}

	// Missing skill name.
	result, err := ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{})
	if err != nil {
		t.Fatalf("runSkillScriptToolHandler missing: %v", err)
	}
	if result["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("error_code: got %v", result["error_code"])
	}

	// Success with args_list.
	result, err = ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName:  "multiplication-calculator",
		ScriptPath: "scripts/multiply.py",
		Args:       []string{"2", "3", "4"},
	})
	if err != nil {
		t.Fatalf("runSkillScriptToolHandler success: %v", err)
	}
	if result["status"] != "success" {
		t.Errorf("status: got %v want success", result["status"])
	}
	if result["stdout"] != "24.0\n" {
		t.Errorf("stdout: got %q want %q", result["stdout"], "24.0\n")
	}

	// Success with alias args string.
	result, err = ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName: "multiplication-calculator",
		Script:    "scripts/multiply.py",
		ArgsLine:  "2 3 4",
	})
	if err != nil {
		t.Fatalf("runSkillScriptToolHandler alias args: %v", err)
	}
	if result["status"] != "success" {
		t.Errorf("alias status: got %v want success", result["status"])
	}
	if result["stdout"] != "24.0\n" {
		t.Errorf("alias stdout: got %q want %q", result["stdout"], "24.0\n")
	}
}

func TestRunSkillScriptSteersToLoadSkillResource(t *testing.T) {
	ts := createTestToolset(t, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))

	// A script path that succeeds.
	res, err := ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName:  "multiplication-calculator",
		ScriptPath: "scripts/multiply.py",
		Args:       []string{"2", "3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["error_code"] != nil {
		t.Fatalf("unexpected error: %v", res)
	}

	// A references/ path should steer to load_skill_resource.
	res, err = ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName:  "multiplication-calculator",
		ScriptPath: "references/guide.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["error_code"] != "USE_LOAD_SKILL_RESOURCE" {
		t.Fatalf("error_code=%v", res["error_code"])
	}
}

func TestRunSkillScriptNotFoundDidYouMean(t *testing.T) {
	ts := createTestToolset(t, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))

	res, err := ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName:  "multiplication-calculator",
		ScriptPath: "scripts/multipy.py", // typo
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Fatalf("error_code=%v", res["error_code"])
	}
	if res["did_you_mean_path"] != "scripts/multiply.py" {
		t.Fatalf("did_you_mean_path=%v", res["did_you_mean_path"])
	}
	if res["available_scripts"] != nil {
		t.Fatalf("expected no available_scripts when did_you_mean_path is set, got %v", res["available_scripts"])
	}
}

func TestRunSkillScriptNotFoundAvailableScripts(t *testing.T) {
	ts := createTestToolset(t, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))

	res, err := ts.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName:  "multiplication-calculator",
		ScriptPath: "scripts/unknown_runner.py",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Fatalf("error_code=%v", res["error_code"])
	}
	if res["did_you_mean_path"] != nil {
		t.Fatalf("unexpected did_you_mean_path=%v", res["did_you_mean_path"])
	}
	list, ok := res["available_scripts"].([]string)
	if !ok || len(list) == 0 {
		t.Fatalf("available_scripts: got %T %v", res["available_scripts"], res["available_scripts"])
	}
	if list[0] != "scripts/multiply.py" {
		t.Fatalf("available_scripts[0]=%q want scripts/multiply.py", list[0])
	}
}

func TestLoadSkillResourceMissingPrefixRecovery(t *testing.T) {
	ts := createTestToolset(t, nil)

	res, err := ts.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{
		SkillName: "multiplication-calculator",
		Path:      "multiply.py", // missing scripts/ prefix
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["error_code"] != "MISSING_RESOURCE_PREFIX" {
		t.Fatalf("error_code=%v", res["error_code"])
	}
	if res["did_you_mean_path"] != "scripts/multiply.py" {
		t.Fatalf("did_you_mean_path=%v", res["did_you_mean_path"])
	}
}

func TestLoadSkillDidYouMean(t *testing.T) {
	ts := createTestToolset(t, nil)

	res, err := ts.loadSkillToolHandler(nil, loadSkillArgs{Name: "multiplication-calculatr"}) // typo
	if err != nil {
		t.Fatal(err)
	}
	if res["error_code"] != "SKILL_NOT_FOUND" {
		t.Fatalf("error_code=%v", res["error_code"])
	}
	if res["did_you_mean"] != "multiplication-calculator" {
		t.Fatalf("did_you_mean=%v", res["did_you_mean"])
	}
}
