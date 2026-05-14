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
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/adk/tool/skilltoolset/skill"
)

// normalizeBundledPath canonicalizes a virtual skill resource path: trims
// whitespace, converts backslashes to forward slashes, drops leading "./" and
// "/" segments, and collapses redundant separators via path.Clean.
func normalizeBundledPath(s string) string {
	t := strings.TrimSpace(s)
	t = filepath.ToSlash(t)
	for strings.HasPrefix(t, "./") {
		t = strings.TrimPrefix(t, "./")
	}
	t = strings.TrimPrefix(t, "/")
	t = strings.TrimPrefix(path.Clean("/"+t), "/")
	return t
}

// collapseDoublePrefixes folds references/references/ → references/ (and the
// same for assets/ and scripts/). Reports whether any collapse occurred so the
// caller can surface a corrected_path hint.
func collapseDoublePrefixes(p string) (out string, redundant bool) {
	out = p
	for {
		before := out
		out = strings.ReplaceAll(out, "references/references/", "references/")
		out = strings.ReplaceAll(out, "assets/assets/", "assets/")
		out = strings.ReplaceAll(out, "scripts/scripts/", "scripts/")
		if out != before {
			redundant = true
			continue
		}
		break
	}
	return out, redundant
}

// isOnlySkillMDPath reports whether the path resolves to a top-level SKILL.md
// (case-insensitive). Used by run_skill_script to redirect callers to load_skill.
func isOnlySkillMDPath(p string) bool {
	c := path.Clean("/" + strings.TrimSpace(filepath.ToSlash(p)))
	rel := strings.TrimPrefix(c, "/")
	if rel == "." || rel == "" {
		return false
	}
	return strings.EqualFold(path.Base(rel), "skill.md")
}

// isDocLikeForRunScript reports whether the path looks like documentation
// (references/, assets/, or a non-SKILL.md .md file) — not an executable script.
func isDocLikeForRunScript(sp string) bool {
	lower := strings.ToLower(sp)
	if strings.HasSuffix(lower, ".md") && !strings.EqualFold(path.Base(sp), "skill.md") {
		return true
	}
	return strings.Contains(lower, "references/") || strings.Contains(lower, "assets/")
}

// isUnsafeVirtualKey reports whether key, treated as a relative path, would
// escape its parent directory after path.Clean.
func isUnsafeVirtualKey(key string) bool {
	if key == "" {
		return true
	}
	c := path.Clean("/" + key)
	rel := strings.TrimPrefix(c, "/")
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return true
	}
	return strings.Contains(rel, "/../")
}

// walksAboveRoot reports whether relative path segments step above the
// starting directory at any point (rather than only the final destination).
func walksAboveRoot(p string) bool {
	depth := 0
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			depth--
			if depth < 0 {
				return true
			}
		} else {
			depth++
		}
	}
	return false
}

// normalizeScriptPathForSecurity trims and slashes only (no path.Clean) so
// ".." segments stay visible for walksAboveRoot.
func normalizeScriptPathForSecurity(s string) string {
	t := strings.TrimSpace(s)
	t = filepath.ToSlash(t)
	for strings.HasPrefix(t, "./") {
		t = strings.TrimPrefix(t, "./")
	}
	return strings.TrimPrefix(t, "/")
}

// secureScriptKey returns the script map key (slash form) safe to use under
// scripts/, or an error payload describing why the path was rejected. Pure
// string validation — no filesystem access required.
func secureScriptKey(scriptPath string) (key string, errPayload map[string]any) {
	p := normalizeScriptPathForSecurity(scriptPath)
	p, _ = collapseDoublePrefixes(p)
	for strings.HasPrefix(p, "scripts/") {
		p = strings.TrimPrefix(p, "scripts/")
	}
	if strings.HasPrefix(p, "/") {
		return "", pathNotAllowedMap()
	}
	if walksAboveRoot(p) {
		return "", pathEscapeMap()
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if isUnsafeVirtualKey(p) {
		return "", pathEscapeMap()
	}
	return filepath.ToSlash(p), nil
}

func pathEscapeMap() map[string]any {
	return map[string]any{
		"error":      "Script path escapes the skill bundle or is invalid.",
		"error_code": "PATH_ESCAPE",
	}
}

func pathNotAllowedMap() map[string]any {
	return map[string]any{
		"error":      "Script path must stay under the skill's scripts/ directory.",
		"error_code": "PATH_NOT_ALLOWED",
	}
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	if len(ra) > len(rb) {
		ra, rb = rb, ra
	}
	row := make([]int, len(rb)+1)
	for j := 0; j <= len(rb); j++ {
		row[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		prev := row[0]
		row[0] = i
		for j := 1; j <= len(rb); j++ {
			cur := row[j]
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			row[j] = min(row[j]+1, row[j-1]+1, prev+cost)
			prev = cur
		}
	}
	return row[len(rb)]
}

// defaultLevMax bounds the largest acceptable Levenshtein distance for a
// did-you-mean hint, scaling gently with query length: 2 for short queries,
// up to 3 for longer ones. A hit beyond this is too dissimilar to suggest.
func defaultLevMax(queryLen int) int {
	if queryLen <= 0 {
		return 2
	}
	m := queryLen / 5
	if m < 2 {
		m = 2
	}
	if m > 3 {
		m = 3
	}
	return m
}

// levPickBestLower picks the candidate with the smallest Levenshtein distance
// to queryLower and also returns the runner-up distance so callers can require
// an unambiguous best match (best.dist < second.dist).
func levPickBestLower(queryLower string, candidates []string) (best string, dist, secondDist int) {
	const big = 1 << 29
	if len(candidates) == 0 {
		return "", big, big
	}
	type scored struct {
		s string
		d int
	}
	sc := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		sc = append(sc, scored{c, levenshtein(queryLower, strings.ToLower(c))})
	}
	sort.Slice(sc, func(i, j int) bool {
		if sc[i].d != sc[j].d {
			return sc[i].d < sc[j].d
		}
		return sc[i].s < sc[j].s
	})
	second := big
	if len(sc) > 1 {
		second = sc[1].d
	}
	return sc[0].s, sc[0].d, second
}

// didYouMeanSkill returns the closest skill name to wrong, or "" if none of
// the candidates are close enough (or the best is tied with the runner-up).
func didYouMeanSkill(ctx context.Context, source skill.Source, wrong string) string {
	fms, err := source.ListFrontmatters(ctx)
	if err != nil || len(fms) == 0 {
		return ""
	}
	names := make([]string, 0, len(fms))
	for _, fm := range fms {
		names = append(names, fm.Name)
	}
	best, d, second := levPickBestLower(strings.ToLower(wrong), names)
	if d <= defaultLevMax(len(wrong)) && d < second {
		return best
	}
	return ""
}

// didYouMeanResource returns the closest known resource path to wrong inside
// the named skill, or "" if none are close enough.
func didYouMeanResource(ctx context.Context, source skill.Source, skillName, wrong string) string {
	paths, err := source.ListResources(ctx, skillName, "")
	if err != nil || len(paths) == 0 {
		return ""
	}
	best, d, second := levPickBestLower(strings.ToLower(wrong), paths)
	if d <= defaultLevMax(len(wrong)) && d < second {
		return best
	}
	return ""
}

// isSkillNotFound reports whether err is a skill-not-found error from Source.
func isSkillNotFound(err error) bool {
	return errors.Is(err, skill.ErrSkillNotFound) || errors.Is(err, skill.ErrInvalidSkillName)
}

// isResourceNotFound reports whether err is a resource-not-found error from Source.
func isResourceNotFound(err error) bool {
	return errors.Is(err, skill.ErrResourceNotFound) || errors.Is(err, skill.ErrInvalidResourcePath)
}
