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

package util

import "testing"

func TestCleanHost(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain hostname",
			input:    "example.looker.com",
			expected: "example.looker.com",
		},
		{
			name:     "https prefix",
			input:    "https://example.looker.com",
			expected: "example.looker.com",
		},
		{
			name:     "http prefix",
			input:    "http://example.looker.com",
			expected: "example.looker.com",
		},
		{
			name:     "trailing slash",
			input:    "example.looker.com/",
			expected: "example.looker.com",
		},
		{
			name:     "https prefix with trailing slash",
			input:    "https://example.looker.com/",
			expected: "example.looker.com",
		},
		{
			name:     "http prefix with trailing slash",
			input:    "http://example.looker.com/",
			expected: "example.looker.com",
		},
		{
			name:     "multiple trailing slashes",
			input:    "https://example.looker.com///",
			expected: "example.looker.com",
		},
		{
			name:     "case insensitive HTTPS",
			input:    "HTTPS://example.looker.com/",
			expected: "example.looker.com",
		},
		{
			name:     "case insensitive HTTP",
			input:    "Http://EXAMPLE.looker.com/",
			expected: "EXAMPLE.looker.com",
		},
		{
			name:     "with port",
			input:    "https://example.looker.com:19999/",
			expected: "example.looker.com:19999",
		},
		{
			name:     "surrounding whitespace",
			input:    "   https://example.looker.com/   ",
			expected: "example.looker.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only https prefix",
			input:    "https://",
			expected: "",
		},
		{
			name:     "only http prefix",
			input:    "http://",
			expected: "",
		},
		{
			name:     "only slash",
			input:    "/",
			expected: "",
		},
		{
			name:     "only multiple slashes",
			input:    "///",
			expected: "",
		},
		{
			name:     "https prefix with multiple slashes only",
			input:    "https://///",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanHost(tt.input)
			if got != tt.expected {
				t.Errorf("CleanHost(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
