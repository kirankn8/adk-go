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

package main

import "testing"

func TestExtractJSONObject_chatterAndFence(t *testing.T) {
	raw := "Sure! ```json\n{\"intent\":\"search\",\"q\":\"a \\\"b\\\"\"}\n```\nDone."
	got, err := extractJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"intent":"search","q":"a \"b\""}` {
		t.Fatalf("got %q", got)
	}
}

func TestParseIntentJSON(t *testing.T) {
	type intent struct {
		Intent string `json:"intent"`
		Q      string `json:"q"`
	}
	var out intent
	err := ParseIntentJSON("prefix {\"intent\":\"x\",\"q\":\"y\"} suffix", &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Intent != "x" || out.Q != "y" {
		t.Fatalf("%+v", out)
	}
}
