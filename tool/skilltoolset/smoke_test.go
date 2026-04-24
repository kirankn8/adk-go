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

package skilltoolset_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/code_executors"
	icontext "google.golang.org/adk/internal/context"
	"google.golang.org/adk/internal/toolinternal"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/skilltoolset"
)

// writeSkillTree lays out a minimal on-disk skill so we can drive the real
// filesystem source and real executor end-to-end.
func writeSkillTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	for _, sub := range []string{"references", "assets", "scripts"} {
		if err := os.MkdirAll(filepath.Join(skillDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"SKILL.md":         "---\nname: demo\ndescription: Demo skill.\n---\nHello from demo.\n",
		"references/x.md":  "x doc",
		"assets/data.txt":  "hello-asset",
		"scripts/hello.sh": "#!/bin/bash\necho \"hi $1\"\n",
	}
	for rel, content := range files {
		p := filepath.Join(skillDir, rel)
		mode := os.FileMode(0o644)
		if filepath.Ext(rel) == ".sh" {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func toolContext(t *testing.T) tool.Context {
	t.Helper()
	invCtx := icontext.NewInvocationContext(context.Background(), icontext.InvocationContextParams{})
	return toolinternal.NewToolContext(invCtx, "", nil, nil)
}

func getTool(t *testing.T, ts *skilltoolset.SkillToolset, name string) toolinternal.FunctionTool {
	t.Helper()
	tools, err := ts.Tools(agent.ReadonlyContext(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if tl.Name() == name {
			ft, ok := tl.(toolinternal.FunctionTool)
			if !ok {
				t.Fatalf("tool %q is not a FunctionTool", name)
			}
			return ft
		}
	}
	t.Fatalf("tool %q not found; got %d tools", name, len(tools))
	return nil
}

func TestSmoke_EndToEnd(t *testing.T) {
	root := writeSkillTree(t)

	exec := code_executors.NewUnsafeLocalCodeExecutor(10 * time.Second)
	ts, err := skilltoolset.NewFileSystem(context.Background(), root, exec)
	if err != nil {
		t.Fatalf("NewFileSystem: %v", err)
	}

	tools, err := ts.Tools(agent.ReadonlyContext(nil))
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	t.Logf("tools: %d registered", len(tools))

	ctx := toolContext(t)

	// 1) list_skills returns our demo
	list := getTool(t, ts, "list_skills")
	listOut, err := list.Run(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("list_skills: %v", err)
	}
	if s, _ := listOut["skills"].(string); s == "" {
		t.Errorf("list_skills: empty skills field; got %v", listOut)
	}

	// 2) load_skill success via name alias
	load := getTool(t, ts, "load_skill")
	loadOut, err := load.Run(ctx, map[string]any{"name": "demo"})
	if err != nil {
		t.Fatalf("load_skill: %v", err)
	}
	if loadOut["skill_name"] != "demo" {
		t.Errorf("load_skill: got skill_name=%v", loadOut["skill_name"])
	}
	if s, _ := loadOut["instructions"].(string); s == "" {
		t.Errorf("load_skill: empty instructions; got %v", loadOut)
	}

	// 3) load_skill did_you_mean
	loadBadOut, _ := load.Run(ctx, map[string]any{"name": "dmo"})
	if loadBadOut["error_code"] != "SKILL_NOT_FOUND" {
		t.Errorf("load_skill bad: want SKILL_NOT_FOUND, got %v", loadBadOut)
	}
	if loadBadOut["did_you_mean"] != "demo" {
		t.Errorf("load_skill bad: did_you_mean=%v", loadBadOut["did_you_mean"])
	}

	// 4) load_skill_resource success via path
	loadRes := getTool(t, ts, "load_skill_resource")
	resOut, err := loadRes.Run(ctx, map[string]any{"skill_name": "demo", "path": "references/x.md"})
	if err != nil {
		t.Fatalf("load_skill_resource: %v", err)
	}
	if resOut["content"] != "x doc" {
		t.Errorf("load_skill_resource: got content=%v", resOut["content"])
	}

	// 5) load_skill_resource cross-bucket detection (asset/data.txt exists, not references/data.txt)
	resBad, _ := loadRes.Run(ctx, map[string]any{"skill_name": "demo", "path": "references/data.txt"})
	if resBad["error_code"] != "FILE_UNDER_DIFFERENT_PREFIX" {
		t.Errorf("cross-bucket: want FILE_UNDER_DIFFERENT_PREFIX, got %v", resBad)
	}
	if resBad["did_you_mean_path"] != "assets/data.txt" {
		t.Errorf("cross-bucket: did_you_mean_path=%v", resBad["did_you_mean_path"])
	}

	// 6) load_skill_resource: SKILL.md redirect
	resSkill, _ := loadRes.Run(ctx, map[string]any{"skill_name": "demo", "path": "SKILL.md"})
	if resSkill["error_code"] != "USE_LOAD_SKILL" {
		t.Errorf("SKILL.md redirect: got %v", resSkill)
	}

	// 7) run_skill_script: execute hello.sh with arg
	runScript := getTool(t, ts, "run_skill_script")
	execOut, err := runScript.Run(ctx, map[string]any{
		"skill_name":  "demo",
		"script_path": "scripts/hello.sh",
		"args_list":   []string{"world"},
	})
	if err != nil {
		t.Fatalf("run_skill_script: %v", err)
	}
	if execOut["error_code"] != nil {
		t.Fatalf("run_skill_script: unexpected error %v", execOut)
	}
	if got, _ := execOut["stdout"].(string); got != "hi world\n" {
		t.Errorf("run_skill_script: stdout=%q want %q", got, "hi world\n")
	}

	// 8) run_skill_script: doc redirect when given a reference path
	redir, _ := runScript.Run(ctx, map[string]any{"skill_name": "demo", "script_path": "references/x.md"})
	if redir["error_code"] != "USE_LOAD_SKILL_RESOURCE" {
		t.Errorf("run_skill_script doc: got %v", redir)
	}

	// 9) run_skill_script: did-you-mean for typo
	typo, _ := runScript.Run(ctx, map[string]any{"skill_name": "demo", "script_path": "scripts/helo.sh"})
	if typo["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Errorf("typo: got %v", typo)
	}
	if typo["did_you_mean_path"] != "scripts/hello.sh" {
		t.Errorf("typo: did_you_mean_path=%v", typo["did_you_mean_path"])
	}

	// 10) run_skill_script: escape attempt sanitized
	esc, _ := runScript.Run(ctx, map[string]any{"skill_name": "demo", "script_path": "../../../etc/passwd"})
	if esc["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Errorf("escape: got %v", esc)
	}
}
