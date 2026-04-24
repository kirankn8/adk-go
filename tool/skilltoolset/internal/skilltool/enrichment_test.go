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

package skilltool_test

import (
	"testing"

	"google.golang.org/adk/internal/toolinternal"
	"google.golang.org/adk/tool/skilltoolset/internal/skilltool"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

func runTool(t *testing.T, tl interface{}, args map[string]any) map[string]any {
	t.Helper()
	ft, ok := tl.(toolinternal.FunctionTool)
	if !ok {
		t.Fatal("tool does not implement toolinternal.FunctionTool")
	}
	got, err := ft.Run(createToolContext(t), args)
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}
	return got
}

func TestLoadSkill_NameAlias(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "multiplication", Description: "Multiply numbers."}},
		instructions: map[string]string{"multiplication": "Call multiply.py."},
	}
	tl, err := skilltool.LoadSkill(src)
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"name", "skill", "skill_name"} {
		got := runTool(t, tl, map[string]any{key: "multiplication"})
		if got["skill_name"] != "multiplication" {
			t.Errorf("alias %q: got skill_name=%v, want multiplication", key, got["skill_name"])
		}
		if got["instructions"] != "Call multiply.py." {
			t.Errorf("alias %q: got instructions=%v", key, got["instructions"])
		}
	}
}

func TestLoadSkill_MissingName(t *testing.T) {
	src := &mockSource{frontmatters: []*skill.Frontmatter{{Name: "foo"}}}
	tl, _ := skilltool.LoadSkill(src)
	got := runTool(t, tl, map[string]any{})
	if got["error_code"] != "MISSING_SKILL_NAME" {
		t.Errorf("error_code: got %v, want MISSING_SKILL_NAME", got["error_code"])
	}
}

func TestLoadSkill_DidYouMean(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{
			{Name: "multiplication", Description: "Multiply."},
			{Name: "addition", Description: "Add."},
		},
	}
	tl, _ := skilltool.LoadSkill(src)
	got := runTool(t, tl, map[string]any{"name": "multipliction"})
	if got["error_code"] != "SKILL_NOT_FOUND" {
		t.Fatalf("error_code: got %v, want SKILL_NOT_FOUND", got["error_code"])
	}
	if got["did_you_mean"] != "multiplication" {
		t.Errorf("did_you_mean: got %v, want multiplication", got["did_you_mean"])
	}
}

func TestLoadSkillResource_PathAlias(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"references/doc.md": "hello"}},
	}
	tl, _ := skilltool.LoadSkillResource(src)
	for _, key := range []string{"path", "resource_path"} {
		got := runTool(t, tl, map[string]any{"skill_name": "s1", key: "references/doc.md"})
		if got["content"] != "hello" {
			t.Errorf("alias %q: got content=%v, want hello", key, got["content"])
		}
	}
}

func TestLoadSkillResource_CollapseDoublePrefix(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"references/doc.md": "hello"}},
	}
	tl, _ := skilltool.LoadSkillResource(src)
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "path": "references/references/doc.md"})
	if got["content"] != "hello" {
		t.Fatalf("content: got %v, want hello", got["content"])
	}
	if got["corrected_path"] != "references/doc.md" {
		t.Errorf("corrected_path: got %v, want references/doc.md", got["corrected_path"])
	}
}

func TestLoadSkillResource_MissingPrefixRecovery(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"references/doc.md": "hello"}},
	}
	tl, _ := skilltool.LoadSkillResource(src)
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "path": "doc.md"})
	if got["error_code"] != "MISSING_RESOURCE_PREFIX" {
		t.Fatalf("error_code: got %v, want MISSING_RESOURCE_PREFIX", got["error_code"])
	}
	if got["did_you_mean_path"] != "references/doc.md" {
		t.Errorf("did_you_mean_path: got %v, want references/doc.md", got["did_you_mean_path"])
	}
}

func TestLoadSkillResource_CrossBucketDetection(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"references/setup.md": "x"}},
	}
	tl, _ := skilltool.LoadSkillResource(src)
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "path": "assets/setup.md"})
	if got["error_code"] != "FILE_UNDER_DIFFERENT_PREFIX" {
		t.Fatalf("error_code: got %v, want FILE_UNDER_DIFFERENT_PREFIX", got["error_code"])
	}
	if got["did_you_mean_path"] != "references/setup.md" {
		t.Errorf("did_you_mean_path: got %v", got["did_you_mean_path"])
	}
}

func TestLoadSkillResource_UseSkillMD(t *testing.T) {
	src := &mockSource{frontmatters: []*skill.Frontmatter{{Name: "s1"}}}
	tl, _ := skilltool.LoadSkillResource(src)
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "path": "SKILL.md"})
	if got["error_code"] != "USE_LOAD_SKILL" {
		t.Errorf("error_code: got %v, want USE_LOAD_SKILL", got["error_code"])
	}
}

func TestRunSkillScript_DocRedirect(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"references/how-to.md": "doc"}},
	}
	tl, _ := skilltool.RunSkillScript(src, "/tmp/unused", nil)
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "script_path": "references/how-to.md"})
	if got["error_code"] != "USE_LOAD_SKILL_RESOURCE" {
		t.Errorf("error_code: got %v, want USE_LOAD_SKILL_RESOURCE", got["error_code"])
	}
}

func TestRunSkillScript_SanitizesEscapeAttempt(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"scripts/only.sh": "#!/bin/sh\n"}},
	}
	tl, _ := skilltool.RunSkillScript(src, "/tmp/unused", nil)
	// Escape attempts are normalized away before the source lookup; the request
	// then falls into SCRIPT_NOT_FOUND rather than reading anything outside the skill.
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "script_path": "../../etc/passwd"})
	if got["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Errorf("error_code: got %v, want SCRIPT_NOT_FOUND", got["error_code"])
	}
}

func TestRunSkillScript_NotFoundDidYouMean(t *testing.T) {
	src := &mockSource{
		frontmatters: []*skill.Frontmatter{{Name: "s1"}},
		resources:    map[string]map[string]string{"s1": {"scripts/deploy.sh": "#!/bin/sh\n"}},
	}
	tl, _ := skilltool.RunSkillScript(src, "/tmp/unused", nil)
	got := runTool(t, tl, map[string]any{"skill_name": "s1", "script_path": "scripts/deplo.sh"})
	if got["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Fatalf("error_code: got %v, want SCRIPT_NOT_FOUND", got["error_code"])
	}
	if got["did_you_mean_path"] != "scripts/deploy.sh" {
		t.Errorf("did_you_mean_path: got %v", got["did_you_mean_path"])
	}
}
