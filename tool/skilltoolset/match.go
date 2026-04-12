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

package skilltoolset

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxRankedCandidates   = 12 // cap for distance-ranked hint lists (paths, keys, inventory)
	closestSkillNameHints = 3  // SKILL_NOT_FOUND: top skill ids when no single did_you_mean
)

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

// collapseDoublePrefixes folds references/references/ → references/ (and assets, scripts).
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

// applyWrongPrefixAlias returns a rewritten path and the shorthand that was fixed (e.g. "ref/").
func applyWrongPrefixAlias(p string) (fixed, from string) {
	aliases := []struct{ wrong, right string }{
		{"ref/", "references/"},
		{"reference/", "references/"},
		{"refs/", "references/"},
		{"asset/", "assets/"},
		{"script/", "scripts/"},
	}
	lowerP := strings.ToLower(p)
	for _, a := range aliases {
		lw := strings.ToLower(a.wrong)
		if strings.HasPrefix(lowerP, lw) {
			return a.right + p[len(a.wrong):], a.wrong
		}
	}
	return p, ""
}

func splitVirtualPrefix(p string) (prefix, key string, ok bool) {
	for _, pre := range []string{"references/", "assets/", "scripts/"} {
		if strings.HasPrefix(p, pre) {
			return pre, strings.TrimPrefix(p, pre), true
		}
	}
	return "", "", false
}

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

func isOnlySkillMDPath(p string) bool {
	c := path.Clean("/" + strings.TrimSpace(filepath.ToSlash(p)))
	rel := strings.TrimPrefix(c, "/")
	if rel == "." || rel == "" {
		return false
	}
	return strings.EqualFold(path.Base(rel), "skill.md")
}

func isDocLikeForRunScript(sp string) bool {
	lower := strings.ToLower(sp)
	if strings.HasSuffix(lower, ".md") && !strings.EqualFold(path.Base(sp), "skill.md") {
		return true
	}
	return strings.Contains(lower, "references/") || strings.Contains(lower, "assets/")
}

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

// levPickBestLower picks the candidate with the smallest Levenshtein distance to queryLower.
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

// bucketKeys strips the bucket prefix from full virtual paths to get the relative key portion.
// e.g. bucketKeys(["references/foo.md"], "references/") → ["foo.md"]
func bucketKeys(bucketPaths []string, prefix string) []string {
	out := make([]string, 0, len(bucketPaths))
	for _, p := range bucketPaths {
		out = append(out, strings.TrimPrefix(p, prefix))
	}
	return out
}

// virtualPathsByBasename returns paths from allPaths whose base name matches basename (case-insensitive).
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

// crossBucketCanonicalPath returns a single virtual path in another bucket with the same basename as relKey.
func crossBucketCanonicalPath(allPaths []string, currentPrefix, relKey string) string {
	b := path.Base(relKey)
	var hits []string
	for _, vp := range allPaths {
		pref, k, ok := splitVirtualPrefix(vp)
		if !ok || pref == currentPrefix {
			continue
		}
		if path.Base(k) == b {
			hits = append(hits, vp)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}

// walksAboveRoot reports whether relative path segments step above the starting directory.
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

// normalizeScriptPathForSecurity trims and slashes only (no path.Clean) so ".." is still detectable.
func normalizeScriptPathForSecurity(s string) string {
	t := strings.TrimSpace(s)
	t = filepath.ToSlash(t)
	for strings.HasPrefix(t, "./") {
		t = strings.TrimPrefix(t, "./")
	}
	return strings.TrimPrefix(t, "/")
}

// secureScriptKey returns the script map key (slash form) safe to use under scripts/, or errPayload.
// Pure string validation — no filesystem access required.
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
