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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	"github.com/spf13/cobra"
)

var (
	projectFileLsPlain bool
	projectFileLsCSV   bool
	projectDirLsPlain  bool
	projectDirLsCSV    bool
)

var projectFileCmd = &cobra.Command{
	Use:     "file",
	Aliases: []string{"files"},
	Short:   "Manage project files",
}

var projectDirectoryCmd = &cobra.Command{
	Use:     "directory",
	Aliases: []string{"directories", "dir", "dirs"},
	Short:   "Manage project directories",
}

var projectFileLsCmd = &cobra.Command{
	Use:   "ls [PROJECT_ID]",
	Short: "List all files in a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		projectID := args[0]

		files, err := c.SDK.AllProjectFiles(projectID, "id,path,type", nil)
		if err != nil {
			return fmt.Errorf("failed to list project files: %w", err)
		}

		headers := []string{"ID", "PATH", "TYPE"}
		table := util.NewTable(headers)
		for _, f := range files {
			row := []string{
				getStringValue(f.Id),
				getStringValue(f.Path),
				getStringValue(f.Type),
			}
			table.Append(row)
		}

		table.Render(projectFileLsPlain, projectFileLsCSV)
		return nil
	},
}

func buildProjectAPIURL(baseURL, projectID, suffix string, query url.Values) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	basePath := strings.TrimRight(u.Path, "/")
	baseRawPath := strings.TrimRight(u.EscapedPath(), "/")
	u.Path = basePath + "/api/4.0/projects/" + projectID + suffix
	u.RawPath = baseRawPath + "/api/4.0/projects/" + url.PathEscape(projectID) + suffix
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func newProjectAPIRequest(ctx context.Context, c *client.ClientWrapper, method, projectID, suffix string, query url.Values, body io.Reader) (*http.Request, error) {
	reqURL, err := buildProjectAPIURL(c.Session.Config.BaseUrl, projectID, suffix, query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range c.Session.Config.Headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

var projectFileCatCmd = &cobra.Command{
	Use:   "cat [PROJECT_ID] [FILE_PATH]",
	Short: "Show contents of a project file",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}
		projectID := args[0]
		filePath := args[1]

		q := url.Values{}
		q.Set("file_path", filePath)
		req, err := newProjectAPIRequest(ctx, c, http.MethodGet, projectID, "/file/content", q, nil)
		if err != nil {
			return err
		}

		resp, err := c.Session.Client.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode >= 400 {
			return fmt.Errorf("failed to fetch file content (status %s): %s", resp.Status, string(bodyBytes))
		}

		fmt.Print(string(bodyBytes))
		return nil
	},
}

var projectFileCreateCmd = &cobra.Command{
	Use:   "create [PROJECT_ID] [FILE_PATH] [CONTENT_FILE_OR_-]",
	Short: "Create a new project file",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return writeProjectFileGeneric(cmd, "POST", args)
	},
}

var projectFileUpdateCmd = &cobra.Command{
	Use:   "update [PROJECT_ID] [FILE_PATH] [CONTENT_FILE_OR_-]",
	Short: "Update an existing project file",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return writeProjectFileGeneric(cmd, "PUT", args)
	},
}

var projectFileRmCmd = &cobra.Command{
	Use:     "rm [PROJECT_ID] [FILE_PATH]",
	Aliases: []string{"delete", "del"},
	Short:   "Delete a project file",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}
		projectID := args[0]
		filePath := args[1]

		q := url.Values{}
		q.Set("file_path", filePath)
		req, err := newProjectAPIRequest(ctx, c, http.MethodDelete, projectID, "/files", q, nil)
		if err != nil {
			return err
		}

		resp, err := c.Session.Client.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to delete file (status %s): %s", resp.Status, string(bodyBytes))
		}

		fmt.Printf("Deleted file '%s' from project '%s'\n", filePath, projectID)
		return nil
	},
}

var projectDirectoryLsCmd = &cobra.Command{
	Use:   "ls [PROJECT_ID]",
	Short: "List all directories in a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}
		projectID := args[0]

		req, err := newProjectAPIRequest(ctx, c, http.MethodGet, projectID, "/directories", nil, nil)
		if err != nil {
			return err
		}

		resp, err := c.Session.Client.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode >= 400 {
			return fmt.Errorf("failed to list directories (status %s): %s", resp.Status, string(bodyBytes))
		}

		var dirs []string
		if err := json.Unmarshal(bodyBytes, &dirs); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		headers := []string{"DIRECTORY PATH"}
		table := util.NewTable(headers)
		for _, d := range dirs {
			table.Append([]string{d})
		}

		table.Render(projectDirLsPlain, projectDirLsCSV)
		return nil
	},
}

var projectDirectoryCreateCmd = &cobra.Command{
	Use:   "create [PROJECT_ID] [DIR_PATH]",
	Short: "Create a new project directory",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}
		projectID := args[0]
		dirPath := args[1]

		payload := map[string]string{
			"path": dirPath,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to encode payload: %w", err)
		}

		req, err := newProjectAPIRequest(ctx, c, http.MethodPost, projectID, "/directories", nil, bytes.NewReader(payloadBytes))
		if err != nil {
			return err
		}

		resp, err := c.Session.Client.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to create directory (status %s): %s", resp.Status, string(bodyBytes))
		}

		fmt.Printf("Created directory '%s' in project '%s'\n", dirPath, projectID)
		return nil
	},
}

var projectDirectoryRmCmd = &cobra.Command{
	Use:     "rm [PROJECT_ID] [DIR_PATH]",
	Aliases: []string{"delete", "del"},
	Short:   "Delete a project directory",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}
		projectID := args[0]
		dirPath := args[1]

		q := url.Values{}
		q.Set("path", dirPath)
		req, err := newProjectAPIRequest(ctx, c, http.MethodDelete, projectID, "/directories", q, nil)
		if err != nil {
			return err
		}

		resp, err := c.Session.Client.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to delete directory (status %s): %s", resp.Status, string(bodyBytes))
		}

		fmt.Printf("Deleted directory '%s' from project '%s'\n", dirPath, projectID)
		return nil
	},
}

func writeProjectFileGeneric(cmd *cobra.Command, method string, args []string) error {
	ctx := cmd.Context()
	c, err := initClient(ctx, false)
	if err != nil {
		return err
	}
	projectID := args[0]
	filePath := args[1]
	contentFile := args[2]

	var bodyBytes []byte
	if contentFile == "-" {
		bodyBytes, err = io.ReadAll(os.Stdin)
	} else {
		bodyBytes, err = os.ReadFile(contentFile)
	}
	if err != nil {
		return fmt.Errorf("failed to read content from %s: %w", contentFile, err)
	}

	payload := map[string]string{
		"path":    filePath,
		"content": string(bodyBytes),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode payload: %w", err)
	}

	req, err := newProjectAPIRequest(ctx, c, method, projectID, "/files", nil, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	resp, err := c.Session.Client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to write file (status %s): %s", resp.Status, string(respBytes))
	}

	action := "Created"
	if method == "PUT" {
		action = "Updated"
	}
	fmt.Printf("%s file '%s' in project '%s'\n", action, filePath, projectID)
	return nil
}

func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func init() {
	ProjectCmd.AddCommand(projectFileCmd)
	ProjectCmd.AddCommand(projectDirectoryCmd)

	projectFileCmd.AddCommand(projectFileLsCmd)
	projectFileCmd.AddCommand(projectFileCatCmd)
	projectFileCmd.AddCommand(projectFileCreateCmd)
	projectFileCmd.AddCommand(projectFileUpdateCmd)
	projectFileCmd.AddCommand(projectFileRmCmd)

	projectDirectoryCmd.AddCommand(projectDirectoryLsCmd)
	projectDirectoryCmd.AddCommand(projectDirectoryCreateCmd)
	projectDirectoryCmd.AddCommand(projectDirectoryRmCmd)

	projectFileLsCmd.Flags().BoolVar(&projectFileLsPlain, "plain", false, "print without any extra formatting")
	projectFileLsCmd.Flags().BoolVar(&projectFileLsCSV, "csv", false, "output in csv format")

	projectDirectoryLsCmd.Flags().BoolVar(&projectDirLsPlain, "plain", false, "print without any extra formatting")
	projectDirectoryLsCmd.Flags().BoolVar(&projectDirLsCSV, "csv", false, "output in csv format")
}
