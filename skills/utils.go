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
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	skill "google.golang.org/adk/tool/skilltoolset/skill"
)

// loadDir recursively loads files from a directory into a map of relative path → content.
func loadDir(directory string) (map[string]string, error) {
	files := make(map[string]string)
	info, err := os.Stat(directory)
	if err != nil {
		return files, nil
	}
	if !info.IsDir() {
		return files, nil
	}
	err = filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, part := range strings.Split(path, string(filepath.Separator)) {
			if part == "__pycache__" {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}
		if !utf8.Valid(b) {
			return errors.New("invalid UTF-8")
		}
		rel, err := filepath.Rel(directory, path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		files[rel] = string(b)
		return nil
	})
	if err != nil {
		return files, err
	}
	return files, nil
}

func parseSkillMD(skillDir string) (*Skill, error) {
	result := &Skill{}
	info, err := os.Stat(skillDir)
	if err != nil {
		return result, fmt.Errorf("skill directory '%s' stat error:%w", skillDir, err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("skill directory '%s' is not a directory", skillDir)
	}

	var skillMD string
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(skillDir, name)
		if _, err := os.Stat(p); err == nil {
			skillMD = p
			break
		}
	}
	if skillMD == "" {
		return result, fmt.Errorf("SKILL.md not found in '%s'", skillDir)
	}

	content, err := os.ReadFile(skillMD)
	if err != nil {
		return result, fmt.Errorf("read skill file '%s': %w", skillMD, err)
	}

	fm, instructions, err := skill.ParseBytes(content)
	if err != nil {
		return result, fmt.Errorf("failed to parse '%s': %w", skillMD, err)
	}

	result.Frontmatter = fm
	result.Instructions = instructions
	result.SkillMDPath = skillMD

	log.Printf("Successfully loaded skill %s locally from %s", fm.Name, skillDir)
	return result, nil
}

func LoadSkillFromDir(skillDir string) (*Skill, error) {
	abs, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, err
	}
	sk, err := parseSkillMD(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse skill directory '%s' error:%w", skillDir, err)
	}

	if base := filepath.Base(abs); base != sk.Name() {
		return nil, fmt.Errorf("skill name '%s' does not match directory name '%s'", sk.Name(), base)
	}
	refs, err := loadDir(filepath.Join(abs, "references"))
	if err != nil {
		return nil, fmt.Errorf("failed to load references dir '%s' error:%w", abs, err)
	}
	assets, err := loadDir(filepath.Join(abs, "assets"))
	if err != nil {
		return nil, fmt.Errorf("failed to load assets dir '%s' error:%w", abs, err)
	}
	rawScripts, err := loadDir(filepath.Join(abs, "scripts"))
	if err != nil {
		return nil, fmt.Errorf("failed to load scripts dir '%s' error:%w", abs, err)
	}
	scripts := make(map[string]*Script, len(rawScripts))
	for k, v := range rawScripts {
		scripts[k] = &Script{Src: v}
	}
	sk.Resources = &Resources{
		References: refs,
		Assets:     assets,
		Scripts:    scripts,
	}

	return sk, nil
}

func ReadSkillProperties(skillDir string) (*Frontmatter, error) {
	abs, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, err
	}
	sk, err := parseSkillMD(abs)
	if err != nil {
		return nil, err
	}
	return sk.Frontmatter, nil
}
