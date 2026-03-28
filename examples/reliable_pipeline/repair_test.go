// SPDX-License-Identifier: Apache-2.0

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
