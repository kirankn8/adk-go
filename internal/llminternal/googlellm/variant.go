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

package googlellm

import (
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
)

// GetGoogleLLMVariant returns Gemini API, Vertex AI, or Unspecified if the LLM is not a Google adapter.
func GetGoogleLLMVariant(llm model.LLM) genai.Backend {
	i, ok := llm.(GoogleLLM)
	if !ok {
		return genai.BackendUnspecified
	}
	return i.GetGoogleLLMVariant()
}

// GoogleLLM is implemented by Google LLM adapters to expose their backend (Gemini API vs Vertex).
type GoogleLLM interface {
	GetGoogleLLMVariant() genai.Backend
}

// IsGeminiModel reports whether the model id names a Gemini family model.
func IsGeminiModel(model string) bool {
	return strings.HasPrefix(extractModelName(model), "gemini-")
}

// IsGeminiAPIVariant reports whether llm uses the consumer Gemini API backend.
func IsGeminiAPIVariant(llm model.LLM) bool {
	return GetGoogleLLMVariant(llm) == genai.BackendGeminiAPI
}

func extractModelName(model string) string {
	modelstring := model[strings.LastIndex(model, "/")+1:]
	return strings.ToLower(modelstring)
}
