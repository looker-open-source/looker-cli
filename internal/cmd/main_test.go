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

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points HOME and the user cache dir at a throwaway directory so no
// test can read or write the developer's real ~/.looker_auth, ~/.netrc,
// config, or dashboard backups.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "looker-cli-cmd-test-home-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp home: %v\n", err)
		os.Exit(1)
	}
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		_ = os.Setenv(k, tmp)
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
