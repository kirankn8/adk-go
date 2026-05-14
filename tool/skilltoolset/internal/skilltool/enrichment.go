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

package skilltool

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"google.golang.org/adk/tool/skilltoolset/skill"
)

const (
	maxRankedCandidates   = 12 // cap for distance-ranked hint lists (paths, keys, inventory)
	closestSkillNameHints = 3  // SKILL_NOT_FOUND: top skill ids when no single did_you_mean
)

// rankedByLevenshtein returns up to k strings from paths ordered by ascending
// edit distance to queryLower (lower-cased on each side); ties broken
// lexicographically.
func rankedByLevenshtein(queryLower string, paths []string, k int) []string {
	type scored struct {
		s string
		d int
	}
	sc := make([]scored, 0, len(paths))
	for _, p := range paths {
		sc = append(sc, scored{p, levenshtein(queryLower, strings.ToLower(p))})
	}
	sort.Slice(sc, func(i, j int) bool {
		if sc[i].d != sc[j].d {
			return sc[i].d < sc[j].d
		}
		return sc[i].s < sc[j].s
	})
	if k > len(sc) {
		k = len(sc)
	}
	out := make([]string, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, sc[i].s)
	}
	return out
}

// bucketKeys strips the bucket prefix from full virtual paths to get the
// relative key portion. e.g. bucketKeys(["scripts/foo.sh"], "scripts/") →
// ["foo.sh"].
func bucketKeys(bucketPaths []string, prefix string) []string {
	out := make([]string, 0, len(bucketPaths))
	for _, p := range bucketPaths {
		out = append(out, strings.TrimPrefix(p, prefix))
	}
	return out
}

// virtualPathsByBasename returns paths from allPaths whose base name matches
// basename (case-insensitive).
func virtualPathsByBasename(allPaths []string, basename string) []string {
	if basename == "" || basename == "." {
		return nil
	}
	var hits []string
	for _, vp := range allPaths {
		if strings.EqualFold(path.Base(vp), basename) {
			hits = append(hits, vp)
		}
	}
	return hits
}

func sortedSkillNames(ctx context.Context, source skill.Source) []string {
	fms, _ := source.ListFrontmatters(ctx)
	names := make([]string, 0, len(fms))
	for _, fm := range fms {
		names = append(names, fm.Name)
	}
	sort.Strings(names)
	return names
}

func skillNotFoundPayload(ctx context.Context, source skill.Source, wrong string) map[string]any {
	base := map[string]any{
		"error":      fmt.Sprintf("Skill '%s' not found.", wrong),
		"error_code": "SKILL_NOT_FOUND",
	}
	names := sortedSkillNames(ctx, source)
	if len(names) == 0 {
		return base
	}
	ql := strings.ToLower(wrong)
	best, d, second := levPickBestLower(ql, names)
	if d <= defaultLevMax(len(wrong)) && d < second {
		base["did_you_mean"] = best
		return base
	}
	base["available_skills"] = rankedByLevenshtein(ql, names, closestSkillNameHints)
	return base
}

func useLoadSkillPayload(skillName string) map[string]any {
	return map[string]any{
		"error":          "SKILL.md instructions are loaded with load_skill, not run_skill_script.",
		"error_code":     "USE_LOAD_SKILL",
		"suggested_tool": "load_skill",
		"suggested_args": map[string]any{"name": skillName},
	}
}

func useLoadSkillResourcePayload(skillName, p string) map[string]any {
	return map[string]any{
		"error":          "references/, assets/, and .md files are read with load_skill_resource; run_skill_script is only for scripts/.",
		"error_code":     "USE_LOAD_SKILL_RESOURCE",
		"suggested_tool": "load_skill_resource",
		"suggested_args": map[string]any{"skill_name": skillName, "resource_path": p},
	}
}

func scriptNotFoundPayload(ctx context.Context, source skill.Source, skillName, displayPath, scriptKey string) map[string]any {
	errStr := fmt.Sprintf("Script '%s' not found in skill '%s'.", displayPath, skillName)
	scriptPaths, _ := source.ListResources(ctx, skillName, "scripts")
	scripts := bucketKeys(scriptPaths, "scripts/")

	skKey := strings.ToLower(scriptKey)
	best, d, second := levPickBestLower(skKey, scripts)
	if len(scripts) > 0 && d <= defaultLevMax(len(scriptKey)) && d < second {
		return map[string]any{
			"error":             errStr,
			"error_code":        "SCRIPT_NOT_FOUND",
			"did_you_mean_path": "scripts/" + best,
		}
	}

	allPaths, _ := source.ListResources(ctx, skillName, "")
	if hits := virtualPathsByBasename(allPaths, path.Base(scriptKey)); len(hits) == 1 && !strings.HasPrefix(hits[0], "scripts/") {
		return map[string]any{
			"error":          "That name refers to a reference or asset; use load_skill_resource to read it.",
			"error_code":     "USE_LOAD_SKILL_RESOURCE",
			"suggested_tool": "load_skill_resource",
			"suggested_args": map[string]any{"skill_name": skillName, "resource_path": hits[0]},
		}
	}

	dp := strings.ToLower(displayPath)
	bestVP, dVP, secondVP := levPickBestLower(dp, scriptPaths)
	if len(scriptPaths) > 0 && dVP <= defaultLevMax(len(displayPath)) && dVP < secondVP {
		return map[string]any{
			"error":             errStr,
			"error_code":        "SCRIPT_NOT_FOUND",
			"did_you_mean_path": bestVP,
		}
	}
	return map[string]any{
		"error":             errStr,
		"error_code":        "SCRIPT_NOT_FOUND",
		"available_scripts": rankedByLevenshtein(dp, scriptPaths, maxRankedCandidates),
	}
}

// toolCtx returns a non-nil context.Context from a tool.Context, falling back
// to context.Background() when ctx is nil (e.g. in unit tests).
func toolCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
