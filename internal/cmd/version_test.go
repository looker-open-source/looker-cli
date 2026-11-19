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

package cmd

import "testing"

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		commit   string
		date     string
		modified bool
		want     string
	}{
		{
			name:    "version only",
			version: "1.3.0",
			want:    "looker-cli 1.3.0",
		},
		{
			name:    "release with commit and date",
			version: "1.3.0",
			commit:  "abc1234",
			date:    "2026-11-19T12:00:00Z",
			want:    "looker-cli 1.3.0 (commit: abc1234, built: 2026-11-19T12:00:00Z)",
		},
		{
			name:     "dirty local build",
			version:  "1.3.0",
			commit:   "abc1234",
			date:     "2026-11-19T12:00:00Z",
			modified: true,
			want:     "looker-cli 1.3.0 (commit: abc1234-dirty, built: 2026-11-19T12:00:00Z)",
		},
		{
			name:    "commit only",
			version: "1.3.0",
			commit:  "abc1234",
			want:    "looker-cli 1.3.0 (commit: abc1234)",
		},
		{
			name:    "date only",
			version: "1.3.0",
			date:    "2026-11-19T12:00:00Z",
			want:    "looker-cli 1.3.0 (built: 2026-11-19T12:00:00Z)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.version, tt.commit, tt.date, tt.modified)
			if got != tt.want {
				t.Errorf("formatVersion(%q, %q, %q, %v) = %q, want %q", tt.version, tt.commit, tt.date, tt.modified, got, tt.want)
			}
		})
	}
}
