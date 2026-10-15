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

package client

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bgentry/go-netrc/netrc"
	"github.com/looker-open-source/looker-cli/internal/util"
)

// GetNetrcCredentials parses ~/.netrc and returns (clientID, clientSecret, error) for the given host.
func GetNetrcCredentials(host string) (string, string, error) {
	host = util.CleanHost(host)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get home directory: %w", err)
	}
	netrcPath := filepath.Join(home, ".netrc")

	if _, err := os.Stat(netrcPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("netrc file not found at %s", netrcPath)
	}

	file, err := os.Open(netrcPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to open netrc: %w", err)
	}
	defer func() { _ = file.Close() }()

	n, err := netrc.Parse(file)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse netrc: %w", err)
	}

	machine := n.FindMachine(host)
	if machine == nil {
		return "", "", fmt.Errorf("no netrc entry found for machine %s", host)
	}

	return machine.Login, machine.Password, nil
}
