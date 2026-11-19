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
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// x-release-please-start-version
const Version = "1.3.0"

// x-release-please-end

// Build metadata injected by GoReleaser ldflags (falls back to Go VCS build info when unset).
var (
	Commit = ""
	Date   = ""
)

func resolveBuildMetadata() (commit, date string, modified bool) {
	commit = Commit
	date = Date
	if commit != "" {
		return commit, date, false
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return commit, date, false
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if len(setting.Value) > 7 {
				commit = setting.Value[:7]
			} else {
				commit = setting.Value
			}
		case "vcs.time":
			if date == "" {
				date = setting.Value
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return commit, date, modified
}

func formatVersion(version, commit, date string, modified bool) string {
	var meta []string
	if commit != "" {
		if modified {
			commit += "-dirty"
		}
		meta = append(meta, fmt.Sprintf("commit: %s", commit))
	}
	if date != "" {
		meta = append(meta, fmt.Sprintf("built: %s", date))
	}
	if len(meta) == 0 {
		return fmt.Sprintf("looker-cli %s", version)
	}
	return fmt.Sprintf("looker-cli %s (%s)", version, strings.Join(meta, ", "))
}

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of looker-cli",
	Run: func(cmd *cobra.Command, args []string) {
		commit, date, modified := resolveBuildMetadata()
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), formatVersion(Version, commit, date, modified))
	},
}

func init() {
	RootCmd.AddCommand(VersionCmd)
}
