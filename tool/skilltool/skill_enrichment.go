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
	"fmt"
	"path"
	"sort"
	"strings"

	"google.golang.org/adk/skills"
)

func availabilityField(prefix string) string {
	switch prefix {
	case "references/":
		return "available_references"
	case "assets/":
		return "available_assets"
	case "scripts/":
		return "available_scripts"
	default:
		return "available_paths"
	}
}

func (s *SkillToolset) sortedSkillNames() []string {
	out := make([]string, 0, len(s.skills))
	for k := range s.skills {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *SkillToolset) skillNotFoundPayload(wrong string) map[string]any {
	base := map[string]any{
		"error":      fmt.Sprintf("Skill '%s' not found.", wrong),
		"error_code": "SKILL_NOT_FOUND",
	}
	names := s.sortedSkillNames()
	if len(names) == 0 {
		return base
	}
	ql := strings.ToLower(wrong)
	best, d, second := levPickBestLower(ql, names)
	maxDist := defaultLevMax(len(wrong))
	if d <= maxDist && d < second {
		base["did_you_mean"] = best
		return base
	}
	base["available_skills"] = rankedByLevenshtein(ql, names, closestSkillNameHints)
	return base
}

func useLoadSkillPayload(skillName string) map[string]any {
	return map[string]any{
		"error":          "SKILL.md instructions are loaded with load_skill, not load_skill_resource.",
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
		"suggested_args": map[string]any{"skill_name": skillName, "path": p},
	}
}

func (s *SkillToolset) emptyResourcePathPayload(sk *skills.Skill) map[string]any {
	paths := BundledVirtualPaths(sk)
	return map[string]any{
		"error":         "Resource path is required (use path or resource_path).",
		"error_code":    "MISSING_RESOURCE_PATH",
		"path_examples": rankedByLevenshtein("", paths, maxRankedCandidates),
	}
}

func (s *SkillToolset) junkResourceKeyPayload(sk *skills.Skill, prefix string) map[string]any {
	m := map[string]any{
		"error":      "Resource path must include a file path after the bucket prefix.",
		"error_code": "INVALID_RESOURCE_PATH",
	}
	keys := bucketKeys(sk, prefix)
	m[availabilityField(prefix)] = rankedByLevenshtein("", keys, maxRankedCandidates)
	return m
}

func (s *SkillToolset) resourceNotFoundPayload(sk *skills.Skill, fullPath, prefix, attemptKey string) map[string]any {
	errStr := fmt.Sprintf("Resource '%s' not found in skill '%s'.", fullPath, sk.Name())
	if alt := crossBucketCanonicalPath(sk, prefix, attemptKey); alt != "" {
		return map[string]any{
			"error":             "Resource exists under a different bucket prefix.",
			"error_code":        "FILE_UNDER_DIFFERENT_PREFIX",
			"did_you_mean_path": alt,
		}
	}
	bk := bucketKeys(sk, prefix)
	ak := strings.ToLower(attemptKey)
	best, d, second := levPickBestLower(ak, bk)
	if len(bk) > 0 && d <= defaultLevMax(len(attemptKey)) && d < second {
		return map[string]any{
			"error":             errStr,
			"error_code":        "RESOURCE_NOT_FOUND",
			"did_you_mean_path": prefix + best,
		}
	}
	if vp := virtualPathsByBasename(sk, path.Base(attemptKey)); len(vp) == 1 {
		return map[string]any{
			"error":             errStr,
			"error_code":        "RESOURCE_NOT_FOUND",
			"did_you_mean_path": vp[0],
		}
	}
	inv := BundledVirtualPaths(sk)
	fp := strings.ToLower(fullPath)
	bestVP, dVP, secondVP := levPickBestLower(fp, inv)
	if len(inv) > 0 && dVP <= defaultLevMax(len(fullPath)) && dVP < secondVP {
		return map[string]any{
			"error":             errStr,
			"error_code":        "RESOURCE_NOT_FOUND",
			"did_you_mean_path": bestVP,
		}
	}
	return map[string]any{
		"error":                   errStr,
		"error_code":              "RESOURCE_NOT_FOUND",
		availabilityField(prefix): rankedByLevenshtein(ak, bk, maxRankedCandidates),
	}
}

func (s *SkillToolset) missingPrefixPayload(sk *skills.Skill, skillName, norm string) map[string]any {
	paths := BundledVirtualPaths(sk)
	ql := strings.ToLower(norm)

	for _, vp := range paths {
		if strings.EqualFold(vp, norm) {
			return map[string]any{
				"error":             "Path must start with references/, assets/, or scripts/.",
				"error_code":        "MISSING_RESOURCE_PREFIX",
				"did_you_mean_path": vp,
				"suggested_args":    map[string]any{"skill_name": skillName, "path": vp},
			}
		}
	}

	hits := virtualPathsByBasename(sk, path.Base(norm))
	if len(hits) == 1 {
		return map[string]any{
			"error":             "Path must start with references/, assets/, or scripts/.",
			"error_code":        "MISSING_RESOURCE_PREFIX",
			"did_you_mean_path": hits[0],
			"suggested_args":    map[string]any{"skill_name": skillName, "path": hits[0]},
		}
	}
	if len(hits) > 1 {
		return map[string]any{
			"error":             "Ambiguous file name; include the full bucket prefix.",
			"error_code":        "AMBIGUOUS_BASENAME",
			"candidates_ranked": rankedByLevenshtein(ql, hits, maxRankedCandidates),
		}
	}

	if !strings.Contains(norm, "/") {
		for _, pre := range []string{"references/", "assets/", "scripts/"} {
			trial := pre + norm
			if _, ok := lookupVirtual(sk, trial); ok {
				return map[string]any{
					"error":             "Path must start with references/, assets/, or scripts/.",
					"error_code":        "MISSING_RESOURCE_PREFIX",
					"did_you_mean_path": trial,
					"suggested_args":    map[string]any{"skill_name": skillName, "path": trial},
				}
			}
		}
	}

	best, d, second := levPickBestLower(ql, paths)
	if d <= defaultLevMax(len(norm)) && d < second {
		return map[string]any{
			"error":             "Path must start with references/, assets/, or scripts/.",
			"error_code":        "MISSING_RESOURCE_PREFIX",
			"did_you_mean_path": best,
			"suggested_args":    map[string]any{"skill_name": skillName, "path": best},
		}
	}

	return map[string]any{
		"error":                   "Path must start with references/, assets/, or scripts/.",
		"error_code":              "INVALID_RESOURCE_PATH",
		"available_path_prefixes": []string{"references/", "assets/", "scripts/"},
		"candidates_ranked":       rankedByLevenshtein(ql, paths, maxRankedCandidates),
	}
}

func (s *SkillToolset) scriptNotFoundPayload(sk *skills.Skill, displayPath, scriptKey string) map[string]any {
	errStr := fmt.Sprintf("Script '%s' not found in skill '%s'.", displayPath, sk.Name())
	var scripts []string
	if sk.Resources != nil {
		scripts = append(scripts, sk.Resources.ListScripts()...)
	}
	skKey := strings.ToLower(scriptKey)
	best, d, second := levPickBestLower(skKey, scripts)
	if len(scripts) > 0 && d <= defaultLevMax(len(scriptKey)) && d < second {
		return map[string]any{
			"error":             errStr,
			"error_code":        "SCRIPT_NOT_FOUND",
			"did_you_mean_path": "scripts/" + best,
		}
	}
	if hits := virtualPathsByBasename(sk, path.Base(scriptKey)); len(hits) == 1 && !strings.HasPrefix(hits[0], "scripts/") {
		return map[string]any{
			"error":          "That name refers to a reference or asset; use load_skill_resource to read it.",
			"error_code":     "USE_LOAD_SKILL_RESOURCE",
			"suggested_tool": "load_skill_resource",
			"suggested_args": map[string]any{"skill_name": sk.Name(), "path": hits[0]},
		}
	}
	scriptPaths := ScriptBundledVirtualPaths(sk)
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
