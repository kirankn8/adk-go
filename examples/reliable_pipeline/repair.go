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

import (
	"encoding/json"
	"errors"
	"strings"
)

var errNoJSONObject = errors.New("no balanced JSON object found")

// stripMarkdownFence removes a leading ```json / ``` fence if present so weak
// models that wrap JSON still parse.
func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToLower(s), "json") {
			s = strings.TrimSpace(s[4:])
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return strings.TrimSpace(s)
}

// extractJSONObject returns the first top-level balanced {...} substring,
// respecting strings and escapes. Use after the model adds chatter around JSON.
func extractJSONObject(raw string) (string, error) {
	raw = stripMarkdownFence(raw)
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return "", errNoJSONObject
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], nil
			}
		}
	}
	return "", errNoJSONObject
}

// ParseIntentJSON extracts and unmarshals a JSON object into dest (e.g. *Intent).
func ParseIntentJSON(raw string, dest any) error {
	obj, err := extractJSONObject(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(obj), dest)
}
