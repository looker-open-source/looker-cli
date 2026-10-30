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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/looker-open-source/looker-cli/internal/config"
)

func TestBuildAPICallURL_NoDoubleEncodingAndPreservesBasePath(t *testing.T) {
	tests := []struct {
		name          string
		baseURL       string
		pathTemplate  string
		pathParams    []string
		pathValues    []string
		requiredQuery map[string]string
		optionalQuery map[string]string
		want          string
	}{
		{
			name:         "spaces and slashes in path params are single-encoded",
			baseURL:      "https://looker.example.com:19999",
			pathTemplate: "/projects/{project_id}/files/{file_id}",
			pathParams:   []string{"project_id", "file_id"},
			pathValues:   []string{"my project", "views/users.view.lkml"},
			want:         "https://looker.example.com:19999/api/4.0/projects/my%20project/files/views%2Fusers.view.lkml",
		},
		{
			name:         "percent signs and reserved URI characters are single-encoded",
			baseURL:      "https://looker.example.com:19999",
			pathTemplate: "/connections/{connection_name}",
			pathParams:   []string{"connection_name"},
			pathValues:   []string{"100% prod?v=1#main"},
			want:         "https://looker.example.com:19999/api/4.0/connections/100%25%20prod%3Fv=1%23main",
		},
		{
			name:         "base URL path prefix is preserved",
			baseURL:      "https://gateway.example.com/looker-prod/",
			pathTemplate: "/projects/{project_id}",
			pathParams:   []string{"project_id"},
			pathValues:   []string{"corp/analytics"},
			want:         "https://gateway.example.com/looker-prod/api/4.0/projects/corp%2Fanalytics",
		},
		{
			name:         "escaped base URL path prefix is preserved without double-encoding",
			baseURL:      "https://gateway.example.com/my%20looker",
			pathTemplate: "/looks/{look_id}",
			pathParams:   []string{"look_id"},
			pathValues:   []string{"42"},
			want:         "https://gateway.example.com/my%20looker/api/4.0/looks/42",
		},
		{
			name:         "simultaneous replacement avoids cross-parameter placeholder injection",
			baseURL:      "https://looker.example.com:19999",
			pathTemplate: "/models/{model_name}/explores/{explore_name}",
			pathParams:   []string{"model_name", "explore_name"},
			pathValues:   []string{"{explore_name}", "orders"},
			want:         "https://looker.example.com:19999/api/4.0/models/%7Bexplore_name%7D/explores/orders",
		},
		{
			name:          "required and optional query parameters are encoded",
			baseURL:       "https://looker.example.com:19999",
			pathTemplate:  "/projects/{project_id}",
			pathParams:    []string{"project_id"},
			pathValues:    []string{"proj one"},
			requiredQuery: map[string]string{"branch": "dev/feature"},
			optionalQuery: map[string]string{"fields": "id,name"},
			want:          "https://looker.example.com:19999/api/4.0/projects/proj%20one?branch=dev%2Ffeature&fields=id%2Cname",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildAPICallURL(tc.baseURL, tc.pathTemplate, tc.pathParams, tc.pathValues, tc.requiredQuery, tc.optionalQuery)
			if err != nil {
				t.Fatalf("buildAPICallURL failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("buildAPICallURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExecuteApiCallGeneric_WireRequestSingleEncoded(t *testing.T) {
	resetGlobalClientFlags(t)
	MockSDK = nil

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	origOverride := config.ConfigPathOverride
	config.ConfigPathOverride = filepath.Join(tmpDir, "config.yaml")
	t.Cleanup(func() { config.ConfigPathOverride = origOverride })

	var capturedRequestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"views/users.view.lkml","path":"views/users.view.lkml"}`))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}

	oldStdout := os.Stdout
	rOut, wOut, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe failed: %v", pipeErr)
	}
	os.Stdout = wOut
	defer func() { os.Stdout = oldStdout }()

	RootCmd.SetArgs([]string{
		"api", "project", "project_file",
		"org/my project",
		"views/users.view.lkml",
		"--host", u.Hostname(),
		"--port", u.Port(),
		"--ssl=false",
		"--token", "test_tok",
	})
	execErr := RootCmd.Execute()
	_ = wOut.Close()

	var outBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)

	if execErr != nil {
		t.Fatalf("api project project_file failed: %v", execErr)
	}

	const wantURI = "/api/4.0/projects/org%2Fmy%20project/files/file?file_id=views%2Fusers.view.lkml"
	if capturedRequestURI != wantURI {
		t.Fatalf("server received RequestURI = %q, want %q", capturedRequestURI, wantURI)
	}
}

func TestGenerateAPITemplateMatchesGeneratedHelper(t *testing.T) {
	genBytes, err := os.ReadFile(filepath.Join("..", "..", "tool", "generate_api.go"))
	if err != nil {
		t.Fatalf("failed to read tool/generate_api.go: %v", err)
	}
	apiBytes, err := os.ReadFile("api_generated.go")
	if err != nil {
		t.Fatalf("failed to read api_generated.go: %v", err)
	}

	const marker = "func buildAPICallURL("
	idx := strings.Index(string(apiBytes), marker)
	if idx < 0 {
		t.Fatalf("could not find %q in api_generated.go", marker)
	}
	runtimeHelper := strings.TrimSpace(string(apiBytes[idx:]))
	if !strings.Contains(string(genBytes), runtimeHelper) {
		t.Fatalf("executeApiCallGeneric / buildAPICallURL in api_generated.go drifted from tool/generate_api.go")
	}
}
