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
	"sort"
	"strings"

	"google.golang.org/adk/tool/skilltoolset/skill"
)

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
