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

import "testing"

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"environment", "envronment", 1},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDefaultLevMax(t *testing.T) {
	for _, c := range []struct {
		qlen int
		want int
	}{
		{0, 2},
		{1, 2},
		{9, 2},
		{10, 2},
		{15, 3},
		{50, 3},
	} {
		if got := defaultLevMax(c.qlen); got != c.want {
			t.Errorf("defaultLevMax(%d) = %d, want %d", c.qlen, got, c.want)
		}
	}
}

func TestLevPickBestLower(t *testing.T) {
	candidates := []string{"environment-discovery", "palette-overview", "network-debug"}

	best, d, second := levPickBestLower("envronment-discovery", candidates)
	if best != "environment-discovery" {
		t.Errorf("best = %q, want %q", best, "environment-discovery")
	}
	if d >= second {
		t.Errorf("expected d < second, got d=%d second=%d", d, second)
	}

	// Empty candidate list → empty result with sentinel large distances.
	best, d, second = levPickBestLower("anything", nil)
	if best != "" || d == 0 || second == 0 {
		t.Errorf("expected empty best with large d/second, got best=%q d=%d second=%d", best, d, second)
	}
}
