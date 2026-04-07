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

package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var mockSkill = &Skill{
	Frontmatter: &Frontmatter{
		Name:        "test-skill",
		Description: "A test skill for unit testing",
		Metadata: map[string]string{
			"version": "1.0.0",
		},
	},
	Instructions: "\n# Test Skill\n\nThis is a test skill.",
	Resources: &Resources{
		Scripts: map[string]*Script{
			"test_script.py": {
				Src: `print("Hello World")`,
			},
		},
		References: map[string]string{
			"ref.md": "# Reference\nThis is a reference file.",
		},
		Assets: map[string]string{
			"data.json":       `{"key": "value"}`,
			"subdir/file.txt": "content in subdir",
		},
	},
}

func TestLoadSkillFromDir(t *testing.T) {
	tmpDir := t.TempDir()

	err := mockSkill.WriteSkill(tmpDir)
	if err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	skillDir := filepath.Join(tmpDir, mockSkill.Name())

	loadedSkill, err := LoadSkillFromDir(skillDir)
	if err != nil {
		t.Fatalf("LoadSkillFromDir: %v", err)
	}
	if loadedSkill == nil {
		t.Fatal("loadedSkill is nil")
	}

	if loadedSkill.Frontmatter.Name != mockSkill.Frontmatter.Name {
		t.Errorf("Name: got %q want %q", loadedSkill.Frontmatter.Name, mockSkill.Frontmatter.Name)
	}
	if loadedSkill.Frontmatter.Description != mockSkill.Frontmatter.Description {
		t.Errorf("Description: got %q want %q", loadedSkill.Frontmatter.Description, mockSkill.Frontmatter.Description)
	}

	expectedInstructions := "\n# Test Skill\n\nThis is a test skill."
	if loadedSkill.Instructions != expectedInstructions {
		t.Errorf("Instructions: got %q want %q", loadedSkill.Instructions, expectedInstructions)
	}

	if loadedSkill.Resources == nil {
		t.Fatal("Resources is nil")
	}

	script, ok := loadedSkill.Resources.GetScript("test_script.py")
	if !ok {
		t.Fatal("GetScript test_script.py: not found")
	}
	if script.Src != mockSkill.Resources.Scripts["test_script.py"].Src {
		t.Errorf("script Src: got %q want %q", script.Src, mockSkill.Resources.Scripts["test_script.py"].Src)
	}

	ref, ok := loadedSkill.Resources.GetReference("ref.md")
	if !ok {
		t.Fatal("GetReference ref.md: not found")
	}
	if ref != mockSkill.Resources.References["ref.md"] {
		t.Errorf("ref: got %q want %q", ref, mockSkill.Resources.References["ref.md"])
	}

	asset, ok := loadedSkill.Resources.GetAsset("data.json")
	if !ok {
		t.Fatal("GetAsset data.json: not found")
	}
	if asset != mockSkill.Resources.Assets["data.json"] {
		t.Errorf("asset: got %q want %q", asset, mockSkill.Resources.Assets["data.json"])
	}

	assetSub, ok := loadedSkill.Resources.GetAsset("subdir/file.txt")
	if !ok {
		t.Fatal("GetAsset subdir/file.txt: not found")
	}
	if assetSub != mockSkill.Resources.Assets["subdir/file.txt"] {
		t.Errorf("subdir asset: got %q want %q", assetSub, mockSkill.Resources.Assets["subdir/file.txt"])
	}
}

func TestReadSkillProperties(t *testing.T) {
	tmpDir := t.TempDir()
	err := mockSkill.WriteSkill(tmpDir)
	if err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	skillDir := filepath.Join(tmpDir, mockSkill.Name())

	frontmatter, err := ReadSkillProperties(skillDir)
	if err != nil {
		t.Fatalf("ReadSkillProperties: %v", err)
	}
	if frontmatter == nil {
		t.Fatal("frontmatter is nil")
	}

	if frontmatter.Name != mockSkill.Frontmatter.Name {
		t.Errorf("Name: got %q want %q", frontmatter.Name, mockSkill.Frontmatter.Name)
	}
	if frontmatter.Description != mockSkill.Frontmatter.Description {
		t.Errorf("Description: got %q want %q", frontmatter.Description, mockSkill.Frontmatter.Description)
	}
}

func TestLoadSkillFromDir_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadSkillFromDir(filepath.Join(tmpDir, "non-existent"))
	if err == nil {
		t.Error("LoadSkillFromDir non-existent: expected error")
	}

	emptyDir := filepath.Join(tmpDir, "empty-skill")
	if err := os.Mkdir(emptyDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	_, err = LoadSkillFromDir(emptyDir)
	if err == nil {
		t.Error("LoadSkillFromDir empty dir: expected error")
	}
	if !strings.Contains(err.Error(), "SKILL.md not found") {
		t.Errorf("error %q should mention SKILL.md not found", err.Error())
	}
}
