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
	"slices"
	"testing"

	"google.golang.org/adk/skills"
)

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"foo", "foo", 0},
	}
	for _, tc := range tests {
		if g := levenshtein(tc.a, tc.b); g != tc.want {
			t.Errorf("levenshtein(%q,%q)=%d want %d", tc.a, tc.b, g, tc.want)
		}
	}
}

func TestNormalizeBundledPath(t *testing.T) {
	if g := normalizeBundledPath("  ./scripts//foo.sh  "); g != "scripts/foo.sh" {
		t.Errorf("got %q", g)
	}
	if g := normalizeBundledPath("/references/a.md"); g != "references/a.md" {
		t.Errorf("got %q", g)
	}
}

func TestCollapseDoublePrefixes(t *testing.T) {
	out, red := collapseDoublePrefixes("references/references/x.md")
	if !red || out != "references/x.md" {
		t.Errorf("got %q red=%v", out, red)
	}
	out, red = collapseDoublePrefixes("scripts/scripts/scripts/a.sh")
	if !red || out != "scripts/a.sh" {
		t.Errorf("got %q red=%v", out, red)
	}
}

func TestBundledVirtualPaths(t *testing.T) {
	sk := &skills.Skill{
		Resources: &skills.Resources{
			References: map[string]string{"a.md": "x"},
			Assets:     map[string]string{"b.txt": "y"},
			Scripts:    map[string]*skills.Script{"c.sh": {Src: "z"}},
		},
	}
	got := BundledVirtualPaths(sk)
	want := []string{"assets/b.txt", "references/a.md", "scripts/c.sh"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestScriptBundledVirtualPaths(t *testing.T) {
	sk := &skills.Skill{
		Resources: &skills.Resources{
			References: map[string]string{"a.md": "x"},
			Assets:     map[string]string{"b.txt": "y"},
			Scripts:    map[string]*skills.Script{"c.sh": {Src: "z"}, "d.sh": {Src: "w"}},
		},
	}
	got := ScriptBundledVirtualPaths(sk)
	want := []string{"scripts/c.sh", "scripts/d.sh"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if ScriptBundledVirtualPaths(nil) != nil {
		t.Errorf("nil skill: got %v want nil", ScriptBundledVirtualPaths(nil))
	}
}

func TestLevPickBestLowerUnique(t *testing.T) {
	cands := []string{"alpha", "beta", "gamma"}
	best, d, second := levPickBestLower("alpah", cands)
	if best != "alpha" || d >= second {
		t.Errorf("best=%q d=%d second=%d", best, d, second)
	}
}

func TestSecureScriptKeyUnderSkill(t *testing.T) {
	tmp := t.TempDir()
	sk := &skills.Skill{
		SkillMDPath: tmp + "/SKILL.md",
		Resources:   &skills.Resources{},
	}
	_, errMap := secureScriptKeyUnderSkill(sk, "../../../etc/passwd")
	if errMap == nil {
		t.Fatal("expected error map")
	}
	key, errMap := secureScriptKeyUnderSkill(sk, "scripts/foo.sh")
	if errMap != nil || key != "foo.sh" {
		t.Fatalf("key=%q errMap=%v", key, errMap)
	}
}
