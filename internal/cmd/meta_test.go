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
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func runCmdWithStdoutRedirect(args []string) (string, error) {
	// The meta flags are bound to package-level variables on the shared
	// RootCmd, and pflag does not restore defaults between Execute() calls.
	// Reset them before and after each run so --noun/--output from one test
	// cannot leak into the next.
	resetMetaFlags := func() {
		metaTreeNoun = ""
		metaTreeOutput = ""
		metaSearchOut = ""
		for _, name := range []string{"noun", "output"} {
			if f := metaTreeCmd.Flags().Lookup(name); f != nil {
				f.Changed = false
			}
		}
		if f := metaSearchCmd.Flags().Lookup("output"); f != nil {
			f.Changed = false
		}
	}
	resetMetaFlags()
	defer resetMetaFlags()
	defer RootCmd.SetArgs(nil)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	outChan := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outChan <- buf.String()
	}()

	RootCmd.SetArgs(args)
	err := RootCmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	out := <-outChan
	return out, err
}

func TestMetaTreeCommandText(t *testing.T) {
	out, err := runCmdWithStdoutRedirect([]string{"meta", "tree"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(out, "looker-cli") {
		t.Errorf("expected 'looker-cli' in output, got %s", out)
	}
	if !strings.Contains(out, "folder") {
		t.Errorf("expected 'folder' in output, got %s", out)
	}
	if !strings.Contains(out, "meta") {
		t.Errorf("expected 'meta' in output, got %s", out)
	}
}

func TestMetaTreeCommandJSON(t *testing.T) {
	out, err := runCmdWithStdoutRedirect([]string{"meta", "tree", "--output", "json"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var node CommandNode
	err = json.Unmarshal([]byte(out), &node)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v. Output was: %s", err, out)
	}

	if node.Name != "looker-cli" {
		t.Errorf("expected root node name 'looker-cli', got %s", node.Name)
	}

	foundFolder := false
	for _, sub := range node.Subcommands {
		if sub.Name == "folder" {
			foundFolder = true
			break
		}
	}
	if !foundFolder {
		t.Errorf("expected to find 'folder' subcommand in JSON")
	}
}

func TestMetaTreeNounCommand(t *testing.T) {
	out, err := runCmdWithStdoutRedirect([]string{"meta", "tree", "--noun", "folder"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(out, "folder") {
		t.Errorf("expected 'folder' in output, got %s", out)
	}
	if strings.Contains(out, "└── connection") {
		t.Errorf("unexpected 'connection' in output when scoped to 'folder', got %s", out)
	}
}

func TestMetaSearchCommandText(t *testing.T) {
	out, err := runCmdWithStdoutRedirect([]string{"meta", "search", "folder"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(out, "looker-cli folder") {
		t.Errorf("expected 'looker-cli folder' in search results, got %s", out)
	}
}

func TestMetaSearchCommandJSON(t *testing.T) {
	out, err := runCmdWithStdoutRedirect([]string{"meta", "search", "folder", "--output", "json"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var nodes []CommandNode
	err = json.Unmarshal([]byte(out), &nodes)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v. Output was: %s", err, out)
	}

	if len(nodes) == 0 {
		t.Errorf("expected to find at least one matching command")
	}

	foundFolder := false
	for _, n := range nodes {
		if n.Name == "folder" {
			foundFolder = true
			break
		}
	}
	if !foundFolder {
		t.Errorf("expected to find 'folder' command in JSON search results")
	}
}
