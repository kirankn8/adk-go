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

// Package skilltool is a compatibility shim.
// Use google.golang.org/adk/tool/skilltoolset instead.
package skilltool

import (
	"path/filepath"

	"google.golang.org/adk/code_executors"
	"google.golang.org/adk/skills"
	"google.golang.org/adk/tool/skilltoolset"
)

// SkillToolset is an alias for skilltoolset.SkillToolset.
type SkillToolset = skilltoolset.SkillToolset

// NewSkillToolset is a compatibility constructor that infers the skills root
// directory from the first skill's path and delegates to
// skilltoolset.NewFileSystemSkillToolset.
//
// Deprecated: use skilltoolset.NewFileSystemSkillToolset directly.
func NewSkillToolset(skillList []*skills.Skill, codeExecutor code_executors.CodeExecutor) (*SkillToolset, error) {
	if len(skillList) == 0 {
		return skilltoolset.NewFileSystemSkillToolset(".", codeExecutor)
	}
	// All skills in a list are expected to share the same parent directory.
	// GetSkillPath() returns the skill's own directory; its parent is the skills root.
	skillsRoot := filepath.Dir(skillList[0].GetSkillPath())
	return skilltoolset.NewFileSystemSkillToolset(skillsRoot, codeExecutor)
}
