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
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	projectLsFields       string
	projectLsPlain        bool
	projectLsCSV          bool
	projectCatFields      string
	projectCatDir         string
	projectCatTrim        bool
	projectBranchAll      bool
	projectBranchFields   string
	projectBranchPlain    bool
	projectBranchCSV      bool
	projectCommitMessage  string
	projectCommitAmend    bool
	projectValidateFields string
	projectValidatePlain  bool
	projectValidateCSV    bool
)

var ProjectCmd = &cobra.Command{
	Use:   "project",
	Short: "Commands pertaining to projects",
}

var projectLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }

		projects, err := c.SDK.AllProjects(projectLsFields, nil)
		if err != nil { return fmt.Errorf("failed to list projects: %w", err) }

		headers := util.ParseFieldsForHeaders(projectLsFields)

		table := util.NewTable(headers)
		for _, p := range projects {
			table.Append(extractFields(p, projectLsFields))
		}
		table.Render(projectLsPlain, projectLsCSV)
		return nil
	},
}

var projectCatCmd = &cobra.Command{
	Use:   "cat [PROJECT_ID]",
	Short: "Output json information about a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]
		project, err := c.SDK.Project(pID, projectCatFields, nil)
		if err != nil { return err }

		b, _ := json.Marshal(project)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if projectCatTrim {
			keep := map[string]bool{
				"name":                           true,
				"git_remote_url":                 true,
				"git_username":                   true,
				"git_production_branch_name":     true,
				"use_git_cookie_auth":            true,
				"git_username_user_attribute":    true,
				"git_password_user_attribute":    true,
				"git_service_name":               true,
				"git_application_server_http_port": true,
				"git_application_server_http_scheme": true,
				"pull_request_mode":              true,
				"validation_required":            true,
				"git_release_mgmt_enabled":       true,
				"allow_warnings":                 true,
			}
			for k := range m {
				if !keep[k] { delete(m, k) }
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if projectCatDir != "" {
			fn := fmt.Sprintf("%s/Project_%s.json", projectCatDir, pID)
			_ = os.WriteFile(fn, outBytes, 0644)
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(string(outBytes))
		}
		return nil
	},
}

var projectImportCmd = &cobra.Command{
	Use:   "import [PROJECT_FILE]",
	Short: "Import a project from a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		file := args[0]

		b, err := util.ReadFileOrStdin(file)
		if err != nil { return err }

		var wp v4.WriteProject
		if err := json.Unmarshal(b, &wp); err != nil { return err }

		project, err := c.SDK.CreateProject(wp, nil)
		if err != nil { return err }

		fmt.Printf("Imported project %s\n", *project.Id)
		return nil
	},
}

var projectUpdateCmd = &cobra.Command{
	Use:   "update [PROJECT_ID] [PROJECT_FILE]",
	Short: "Update a project from a file",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]
		file := args[1]

		b, err := util.ReadFileOrStdin(file)
		if err != nil { return err }

		var wp v4.WriteProject
		if err := json.Unmarshal(b, &wp); err != nil { return err }

		project, err := c.SDK.UpdateProject(pID, wp, "", nil)
		if err != nil { return err }

		fmt.Printf("Updated project %s\n", *project.Id)
		return nil
	},
}

var projectDeployKeyCmd = &cobra.Command{
	Use:   "key [PROJECT_ID]",
	Short: "Generate a git deploy public key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]

		key, err := c.SDK.CreateGitDeployKey(pID, nil)
		if err != nil { return err }

		fmt.Println(key)
		return nil
	},
}

type projectCommitRequest struct {
	Files   *[]string `json:"files,omitempty"`
	Message *string   `json:"message,omitempty"`
	Amend   *bool     `json:"amend,omitempty"`
}

var projectCommitCmd = &cobra.Command{
	Use:   "commit [PROJECT_ID] [FILES...]",
	Short: "Commit changes in a project",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]
		req := projectCommitRequest{}

		if cmd.Flags().Changed("message") || projectCommitMessage != "" {
			req.Message = &projectCommitMessage
		}

		if len(args) > 1 {
			files := args[1:]
			req.Files = &files
		}

		if cmd.Flags().Changed("amend") || projectCommitAmend {
			req.Amend = &projectCommitAmend
		}

		var result string
		path := fmt.Sprintf("/projects/%s/commit", url.PathEscape(pID))
		err = c.SDK.AuthSession.Do(&result, "POST", "/4.0", path, nil, req, nil)
		if err != nil {
			return err
		}

		if strings.TrimSpace(result) != "" {
			fmt.Println(result)
		} else {
			fmt.Printf("Committed changes for project %s.\n", pID)
		}
		return nil
	},
}

var projectPushBranchCmd = &cobra.Command{
	Use:     "push-branch [PROJECT_ID]",
	Aliases: []string{"push"},
	Short:   "Push active branch of a project to remote",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]

		var result string
		path := fmt.Sprintf("/projects/%s/push_branch", url.PathEscape(pID))
		err = c.SDK.AuthSession.Do(&result, "POST", "/4.0", path, nil, nil, nil)
		if err != nil {
			return err
		}

		if strings.TrimSpace(result) != "" {
			fmt.Println(result)
		} else {
			fmt.Printf("Pushed branch for project %s.\n", pID)
		}
		return nil
	},
}

var projectBranchCmd = &cobra.Command{
	Use:   "branch [PROJECT_ID]",
	Short: "List active branch or all branches of a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]

		headers := strings.Split(projectBranchFields, ",")
		for i := range headers { headers[i] = strings.TrimSpace(headers[i]) }
		table := util.NewTable(headers)

		if projectBranchAll {
			branches, err := c.SDK.AllGitBranches(pID, nil)
			if err != nil { return err }
			for _, b := range branches {
				table.Append(extractFields(b, projectBranchFields))
			}
		} else {
			branch, err := c.SDK.GitBranch(pID, nil)
			if err != nil { return err }
			table.Append(extractFields(branch, projectBranchFields))
		}

		table.Render(projectBranchPlain, projectBranchCSV)
		return nil
	},
}

var projectDeployCmd = &cobra.Command{
	Use:   "deploy [PROJECT_ID]",
	Short: "Deploy active branch of a project to production",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]

		_, err = c.SDK.DeployToProduction(pID, nil)
		if err != nil { return err }

		fmt.Printf("Deployed project %s to production.\n", pID)
		return nil
	},
}

var projectCheckoutCmd = &cobra.Command{
	Use:   "checkout [PROJECT_ID] [BRANCH_NAME]",
	Short: "Change active branch of a project",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]
		branch := args[1]

		wgb := v4.WriteGitBranch{Name: &branch}
		_, err = c.SDK.UpdateGitBranch(pID, wgb, nil)
		if err != nil { return err }

		fmt.Printf("Checked out branch %s for project %s.\n", branch, pID)
		return nil
	},
}

var projectCreateCmd = &cobra.Command{
	Use:   "create [PROJECT_NAME]",
	Short: "Create a new project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		name := args[0]

		wp := v4.WriteProject{
			Name: &name,
		}

		project, err := c.SDK.CreateProject(wp, nil)
		if err != nil { return err }

		fmt.Printf("Created project %s\n", *project.Id)
		return nil
	},
}

var projectValidateCmd = &cobra.Command{
	Use:   "validate [PROJECT_ID]",
	Short: "Validate a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		pID := args[0]

		validation, err := c.SDK.ValidateProject(pID, "", nil)
		if err != nil { return err }

		if validation.Errors == nil || len(*validation.Errors) == 0 {
			fmt.Println("Project is valid.")
			return nil
		}

		headers := util.ParseFieldsForHeaders(projectValidateFields)
		table := util.NewTable(headers)
		for _, e := range *validation.Errors {
			table.Append(extractFields(e, projectValidateFields))
		}
		table.Render(projectValidatePlain, projectValidateCSV)

		return fmt.Errorf("project validation failed with %d errors", len(*validation.Errors))
	},
}

func init() {
	RootCmd.AddCommand(ProjectCmd)
	ProjectCmd.AddCommand(projectLsCmd)
	ProjectCmd.AddCommand(projectCatCmd)
	ProjectCmd.AddCommand(projectImportCmd)
	ProjectCmd.AddCommand(projectUpdateCmd)
	ProjectCmd.AddCommand(projectBranchCmd)
	ProjectCmd.AddCommand(projectCommitCmd)
	ProjectCmd.AddCommand(projectPushBranchCmd)
	ProjectCmd.AddCommand(projectDeployCmd)
	ProjectCmd.AddCommand(projectCheckoutCmd)
	ProjectCmd.AddCommand(projectCreateCmd)
	ProjectCmd.AddCommand(projectValidateCmd)

	projectDeployCmd.AddCommand(projectDeployKeyCmd)

	projectLsCmd.Flags().StringVar(&projectLsFields, "fields", "id,name,git_production_branch_name", "Fields to display")
	projectLsCmd.Flags().BoolVar(&projectLsPlain, "plain", false, "print without formatting")
	projectLsCmd.Flags().BoolVar(&projectLsCSV, "csv", false, "output in csv format")

	projectCatCmd.Flags().StringVar(&projectCatDir, "dir", "", "Directory to store output file")
	projectCatCmd.Flags().StringVar(&projectCatFields, "fields", "", "Fields to display")
	projectCatCmd.Flags().BoolVar(&projectCatTrim, "trim", false, "Trim output to minimal set of fields")

	projectBranchCmd.Flags().BoolVar(&projectBranchAll, "all", false, "List all branches")
	projectBranchCmd.Flags().StringVar(&projectBranchFields, "fields", "name,error,message", "Fields to display")
	projectBranchCmd.Flags().BoolVar(&projectBranchPlain, "plain", false, "print without formatting")
	projectBranchCmd.Flags().BoolVar(&projectBranchCSV, "csv", false, "output in csv format")

	projectCommitCmd.Flags().StringVarP(&projectCommitMessage, "message", "m", "", "Commit message")
	_ = projectCommitCmd.MarkFlagRequired("message")
	projectCommitCmd.Flags().BoolVar(&projectCommitAmend, "amend", false, "Amend the last commit")

	projectValidateCmd.Flags().StringVar(&projectValidateFields, "fields", "severity,file_path,line_number,message", "Fields to display")
	projectValidateCmd.Flags().BoolVar(&projectValidatePlain, "plain", false, "print without formatting")
	projectValidateCmd.Flags().BoolVar(&projectValidateCSV, "csv", false, "output in csv format")
}
