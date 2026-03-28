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
	"log"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/skills"
	"google.golang.org/adk/tool"
)

var mockSkills = map[string]*skills.Skill{
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

func createMockSkill(t *testing.T) []*skills.Skill {
	tmpDir := t.TempDir()
	log.Print("Created temp dir: " + tmpDir)
	var skillList []*skills.Skill
	for _, sk := range mockSkills {
		err := sk.WriteSkill(tmpDir)
		if err != nil {
			t.Fatalf("write skill %s to %s error:%s", sk.Name(), tmpDir, err)
		}
		skillList = append(skillList, sk)
	}
	return skillList
}

func TestListSkillsTool(t *testing.T) {
	skillList := createMockSkill(t)
	toolset, err := NewSkillToolset(skillList, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))
	if err != nil {
		t.Fatalf("NewSkillToolset: %v", err)
	}

	listTool := toolset.listSkillsTool()
	if listTool.Name() != "list_skills" {
		t.Errorf("list tool name: got %q want list_skills", listTool.Name())
	}

	result, err := toolset.listSkillsToolHandler(nil, listSkillsArgs{})
	if err != nil {
		t.Fatalf("listSkillsToolHandler: %v", err)
	}

	outputMap := result
	xmlResult, ok := outputMap["result"].(string)
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
	skillList := createMockSkill(t)
	toolset, err := NewSkillToolset(skillList, nil)
	if err != nil {
		t.Fatalf("NewSkillToolset: %v", err)
	}

	loadTool := toolset.loadSkillTool()
	if loadTool.Name() != "load_skill" {
		t.Errorf("load tool name: got %q want load_skill", loadTool.Name())
	}

	result, err := toolset.loadSkillToolHandler(nil, loadSkillArgs{})
	if err != nil {
		t.Fatalf("loadSkillToolHandler missing name: %v", err)
	}
	outputMap := result
	if outputMap["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("missing name error_code: got %v", outputMap["error_code"])
	}

	result, err = toolset.loadSkillToolHandler(nil, loadSkillArgs{Name: "unknown-skill"})
	if err != nil {
		t.Fatalf("loadSkillToolHandler unknown: %v", err)
	}
	outputMap = result
	if outputMap["error_code"] != "SKILL_NOT_FOUND" {
		t.Errorf("unknown skill error_code: got %v", outputMap["error_code"])
	}

	result, err = toolset.loadSkillToolHandler(nil, loadSkillArgs{Name: "multiplication-calculator"})
	if err != nil {
		t.Fatalf("loadSkillToolHandler success: %v", err)
	}
	outputMap = result
	if outputMap["skill_name"] != "multiplication-calculator" {
		t.Errorf("skill_name: got %v", outputMap["skill_name"])
	}
	if outputMap["instructions"] != mockSkills["multiplication-calculator"].Instructions {
		t.Error("instructions mismatch")
	}
	if outputMap["frontmatter"] == "" {
		t.Error("frontmatter should be non-empty")
	}

	result, err = toolset.loadSkillToolHandler(nil, loadSkillArgs{Skill: "multiplication-calculator"})
	if err != nil {
		t.Fatalf("loadSkillToolHandler skill alias: %v", err)
	}
	outputMap = result
	if outputMap["skill_name"] != "multiplication-calculator" {
		t.Errorf("skill alias skill_name: got %v", outputMap["skill_name"])
	}
}

func TestLoadSkillResourceTool(t *testing.T) {
	skillList := createMockSkill(t)
	toolset, err := NewSkillToolset(skillList, nil)
	if err != nil {
		t.Fatalf("NewSkillToolset: %v", err)
	}

	resourceTool := toolset.loadSkillResourceTool()
	if resourceTool.Name() != "load_skill_resource" {
		t.Errorf("resource tool name: got %q", resourceTool.Name())
	}

	result, err := toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler: %v", err)
	}
	outputMap := result
	if outputMap["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("error_code: got %v", outputMap["error_code"])
	}

	result, err = toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{SkillName: "multiplication-calculator"})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler: %v", err)
	}
	outputMap = result
	if outputMap["error_code"] != "MISSING_RESOURCE_PATH" {
		t.Errorf("error_code: got %v", outputMap["error_code"])
	}

	result, err = toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{
		SkillName: "multiplication-calculator",
		Path:      "scripts/multiply.py",
	})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler success: %v", err)
	}
	outputMap = result
	if outputMap["skill_name"] != "multiplication-calculator" {
		t.Errorf("skill_name: got %v", outputMap["skill_name"])
	}
	if outputMap["path"] != "scripts/multiply.py" {
		t.Errorf("path: got %v", outputMap["path"])
	}
	wantContent := mockSkills["multiplication-calculator"].Resources.Scripts["multiply.py"].String()
	if outputMap["content"] != wantContent {
		t.Error("content mismatch")
	}

	result, err = toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{
		SkillName: "multiplication-calculator",
		Path:      "scripts/unknown.py",
	})
	if err != nil {
		t.Fatalf("loadSkillResourceToolHandler not found: %v", err)
	}
	outputMap = result
	if outputMap["error_code"] != "RESOURCE_NOT_FOUND" {
		t.Errorf("error_code: got %v", outputMap["error_code"])
	}
}

type mockToolContext struct {
	tool.Context
}

func (m *mockToolContext) InvocationID() string {
	return "test-invocation-id"
}

func TestRunSkillScriptTool(t *testing.T) {
	skillList := createMockSkill(t)

	mockExecutor := code_executors.NewUnsafeLocalCodeExecutor(300 * time.Second)

	toolset, err := NewSkillToolset(skillList, mockExecutor)
	if err != nil {
		t.Fatalf("NewSkillToolset: %v", err)
	}

	runTool := toolset.runSkillScriptTool()
	if runTool.Name() != "run_skill_script" {
		t.Errorf("run tool name: got %q", runTool.Name())
	}

	result, err := toolset.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{})
	if err != nil {
		t.Fatalf("runSkillScriptToolHandler missing: %v", err)
	}
	outputMap := result
	if outputMap["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("error_code: got %v", outputMap["error_code"])
	}

	result, err = toolset.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName:  "multiplication-calculator",
		ScriptPath: "scripts/multiply.py",
		Args:       []string{"2", "3", "4"},
	})
	if err != nil {
		t.Fatalf("runSkillScriptToolHandler success: %v", err)
	}
	outputMap = result
	if outputMap["status"] != "success" {
		t.Errorf("status: got %v want success", outputMap["status"])
	}
	if outputMap["stdout"] != "24.0\n" {
		t.Errorf("stdout: got %q want %q", outputMap["stdout"], "24.0\n")
	}

	// Model-style aliases: script + args string instead of script_path + args_list.
	result, err = toolset.runSkillScriptToolHandler(&mockToolContext{}, runSkillScriptArgs{
		SkillName: "multiplication-calculator",
		Script:    "scripts/multiply.py",
		ArgsLine:  "2 3 4",
	})
	if err != nil {
		t.Fatalf("runSkillScriptToolHandler alias args: %v", err)
	}
	outputMap = result
	if outputMap["status"] != "success" {
		t.Errorf("alias status: got %v want success", outputMap["status"])
	}
	if outputMap["stdout"] != "24.0\n" {
		t.Errorf("alias stdout: got %q want %q", outputMap["stdout"], "24.0\n")
	}
}
