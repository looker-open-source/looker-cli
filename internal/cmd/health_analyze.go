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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	analyzeProject    string
	analyzeModel      string
	analyzeExplore    string
	analyzeTimeframe  int
	analyzeMinQueries int
	analyzePlain      bool
	analyzeCSV        bool
)

var HealthAnalyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze Looker projects, models, and explores",
	Long:  `Performs health analysis on Looker assets to identify usage and potential issues.`,
}

var analyzeProjectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Analyze Looker projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runAnalyzeProjects(cmd.Context(), c, analyzeProject, analyzePlain, analyzeCSV)
	},
}

var analyzeModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Analyze Looker models",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runAnalyzeModels(cmd.Context(), c, analyzeProject, analyzeModel, analyzeTimeframe, analyzeMinQueries, analyzePlain, analyzeCSV)
	},
}

var analyzeExploresCmd = &cobra.Command{
	Use:   "explores",
	Short: "Analyze Looker explores",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runAnalyzeExplores(cmd.Context(), c, analyzeModel, analyzeExplore, analyzeTimeframe, analyzeMinQueries, analyzePlain, analyzeCSV)
	},
}

func runAnalyzeProjects(ctx context.Context, c *client.ClientWrapper, projectID string, plain, csv bool) error {
	var projects []*v4.Project
	if projectID != "" {
		p, err := c.SDK.Project(projectID, "", nil)
		if err != nil {
			return fmt.Errorf("error fetching project %s: %w", projectID, err)
		}
		projects = append(projects, &p)
	} else {
		allProjects, err := c.SDK.AllProjects("", nil)
		if err != nil {
			return fmt.Errorf("error fetching all projects: %w", err)
		}
		for i := range allProjects {
			projects = append(projects, &allProjects[i])
		}
	}

	headers := []string{"Project", "# Models", "# View Files", "Git Connection Status", "PR Mode", "Is Validation Required"}
	table := util.NewTable(headers)

	for _, p := range projects {
		if p.Name == nil || p.Id == nil {
			continue
		}
		pName := *p.Name
		pID := *p.Id

		projectFiles, err := c.SDK.AllProjectFiles(pID, "", nil)
		if err != nil {
			return fmt.Errorf("error fetching files for project %s: %w", pName, err)
		}

		modelCount := 0
		viewFileCount := 0
		for _, f := range projectFiles {
			if f.Type != nil {
				if *f.Type == "model" {
					modelCount++
				}
				if *f.Type == "view" {
					viewFileCount++
				}
			}
		}

		gitConnectionStatus := "OK"
		if p.GitRemoteUrl == nil {
			gitConnectionStatus = "No repo found"
		} else if strings.Contains(*p.GitRemoteUrl, "/bare_models/") {
			gitConnectionStatus = "Bare repo, no tests required"
		}

		prMode := ""
		if p.PullRequestMode != nil {
			prMode = string(*p.PullRequestMode)
		}

		valReq := "false"
		if p.ValidationRequired != nil {
			valReq = fmt.Sprintf("%v", *p.ValidationRequired)
		}

		table.Append([]string{
			pName,
			fmt.Sprintf("%d", modelCount),
			fmt.Sprintf("%d", viewFileCount),
			gitConnectionStatus,
			prMode,
			valReq,
		})
	}

	table.Render(plain, csv)
	return nil
}

func runAnalyzeModels(ctx context.Context, c *client.ClientWrapper, project, model string, timeframe, minQueries int, plain, csv bool) error {
	usedModels, err := getUsedModels(ctx, c, timeframe, minQueries)
	if err != nil {
		return fmt.Errorf("error fetching used models: %w", err)
	}

	lookmlModels, err := c.SDK.AllLookmlModels(v4.RequestAllLookmlModels{}, nil)
	if err != nil {
		return fmt.Errorf("error fetching LookML models: %w", err)
	}

	headers := []string{"Project", "Model", "# Explores", "Query Count"}
	table := util.NewTable(headers)

	for _, m := range lookmlModels {
		if m.Name == nil || m.ProjectName == nil {
			continue
		}
		if (project == "" || *m.ProjectName == project) &&
			(model == "" || *m.Name == model) {

			queryCount := 0
			if qc, ok := usedModels[*m.Name]; ok {
				queryCount = qc
			}

			exploreCount := 0
			if m.Explores != nil {
				exploreCount = len(*m.Explores)
			}

			table.Append([]string{
				*m.ProjectName,
				*m.Name,
				fmt.Sprintf("%d", exploreCount),
				fmt.Sprintf("%d", queryCount),
			})
		}
	}

	table.Render(plain, csv)
	return nil
}

func getUsedModels(ctx context.Context, c *client.ClientWrapper, timeframe, minQueries int) (map[string]int, error) {
	limit := "5000"
	query := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "history",
		Fields: &[]string{"history.query_run_count", "query.model"},
		Filters: &map[string]interface{}{
			"history.created_date":    fmt.Sprintf("%d days", timeframe),
			"query.model":             "-system__activity, -i__looker",
			"history.query_run_count": fmt.Sprintf(">%d", minQueries-1),
			"user.dev_branch_name":    "NULL",
		},
		Limit: &limit,
	}
	req := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *query,
	}
	raw, err := c.SDK.RunInlineQuery(req, nil)
	if err != nil {
		return nil, err
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}

	results := make(map[string]int)
	for _, row := range data {
		model, _ := row["query.model"].(string)
		count, _ := row["history.query_run_count"].(float64)
		results[model] = int(count)
	}
	return results, nil
}

func runAnalyzeExplores(ctx context.Context, c *client.ClientWrapper, model, explore string, timeframe, minQueries int, plain, csv bool) error {
	lookmlModels, err := c.SDK.AllLookmlModels(v4.RequestAllLookmlModels{}, nil)
	if err != nil {
		return fmt.Errorf("error fetching LookML models: %w", err)
	}

	headers := []string{"Model", "Explore", "Is Hidden", "Has Description", "# Joins", "# Unused Joins", "# Unused Fields", "# Fields", "Query Count"}
	table := util.NewTable(headers)
	var errs []error

	for _, m := range lookmlModels {
		if m.Name == nil {
			continue
		}
		if model != "" && *m.Name != model {
			continue
		}
		if m.Explores == nil {
			continue
		}

		for _, e := range *m.Explores {
			if e.Name == nil {
				continue
			}
			if explore != "" && *e.Name != explore {
				continue
			}

			req := v4.RequestLookmlModelExplore{
				LookmlModelName: *m.Name,
				ExploreName:     *e.Name,
			}
			exploreDetail, err := c.SDK.LookmlModelExplore(req, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching detail for explore %s.%s: %v\n", *m.Name, *e.Name, err)
				errs = append(errs, fmt.Errorf("error fetching detail for explore %s.%s: %w", *m.Name, *e.Name, err))
				continue
			}

			fieldCount := 0
			if exploreDetail.Fields != nil {
				dims := 0
				if exploreDetail.Fields.Dimensions != nil {
					dims = len(*exploreDetail.Fields.Dimensions)
				}
				meas := 0
				if exploreDetail.Fields.Measures != nil {
					meas = len(*exploreDetail.Fields.Measures)
				}
				fieldCount = dims + meas
			}

			joinCount := 0
			if exploreDetail.Joins != nil {
				joinCount = len(*exploreDetail.Joins)
			}

			usedFields, err := getUsedExploreFields(ctx, c, *m.Name, *e.Name, timeframe)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching used fields for explore %s.%s: %v\n", *m.Name, *e.Name, err)
				errs = append(errs, fmt.Errorf("error fetching used fields for explore %s.%s: %w", *m.Name, *e.Name, err))
				continue
			}

			allFields := []string{}
			if exploreDetail.Fields != nil {
				if exploreDetail.Fields.Dimensions != nil {
					for _, d := range *exploreDetail.Fields.Dimensions {
						if d.Hidden != nil && !*d.Hidden && d.Name != nil {
							allFields = append(allFields, *d.Name)
						}
					}
				}
				if exploreDetail.Fields.Measures != nil {
					for _, ms := range *exploreDetail.Fields.Measures {
						if ms.Hidden != nil && !*ms.Hidden && ms.Name != nil {
							allFields = append(allFields, *ms.Name)
						}
					}
				}
			}

			unusedFieldsCount := 0
			for _, field := range allFields {
				if _, ok := usedFields[field]; !ok {
					unusedFieldsCount++
				}
			}

			joinStats := make(map[string]int)
			if exploreDetail.Joins != nil {
				for field, queryCount := range usedFields {
					parts := strings.Split(field, ".")
					if len(parts) > 0 {
						join := parts[0]
						joinStats[join] += queryCount
					}
				}
				for _, join := range *exploreDetail.Joins {
					if join.Name != nil {
						if _, ok := joinStats[*join.Name]; !ok {
							joinStats[*join.Name] = 0
						}
					}
				}
			}

			unusedJoinsCount := 0
			for _, count := range joinStats {
				if count == 0 {
					unusedJoinsCount++
				}
			}

			// Use inline query to get query count
			limit := "1"
			queryCountQueryBody := &v4.WriteQuery{
				Model:  "system__activity",
				View:   "history",
				Fields: &[]string{"history.query_run_count"},
				Filters: &map[string]interface{}{
					"query.model":             *m.Name,
					"query.view":              *e.Name,
					"history.created_date":    fmt.Sprintf("%d days", timeframe),
					"history.query_run_count": fmt.Sprintf(">%d", minQueries-1),
					"user.dev_branch_name":    "NULL",
				},
				Limit: &limit,
			}
			reqQC := v4.RequestRunInlineQuery{
				ResultFormat: "json",
				Body:         *queryCountQueryBody,
			}
			rawQueryCount, err := c.SDK.RunInlineQuery(reqQC, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching query count for explore %s.%s: %v\n", *m.Name, *e.Name, err)
				errs = append(errs, fmt.Errorf("error fetching query count for explore %s.%s: %w", *m.Name, *e.Name, err))
				continue
			}
			var data []map[string]interface{}
			if err := json.Unmarshal([]byte(rawQueryCount), &data); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing query count for explore %s.%s: %v\n", *m.Name, *e.Name, err)
				errs = append(errs, fmt.Errorf("error parsing query count for explore %s.%s: %w", *m.Name, *e.Name, err))
				continue
			}
			queryCount := 0
			if len(data) > 0 {
				if count, ok := data[0]["history.query_run_count"].(float64); ok {
					queryCount = int(count)
				}
			}

			isHidden := "false"
			if e.Hidden != nil {
				isHidden = fmt.Sprintf("%v", *e.Hidden)
			}

			hasDesc := "false"
			if e.Description != nil && *e.Description != "" {
				hasDesc = "true"
			}

			table.Append([]string{
				*m.Name,
				*e.Name,
				isHidden,
				hasDesc,
				fmt.Sprintf("%d", joinCount),
				fmt.Sprintf("%d", unusedJoinsCount),
				fmt.Sprintf("%d", unusedFieldsCount),
				fmt.Sprintf("%d", fieldCount),
				fmt.Sprintf("%d", queryCount),
			})
		}
	}

	table.Render(plain, csv)
	return errors.Join(errs...)
}

func getUsedExploreFields(ctx context.Context, c *client.ClientWrapper, model, explore string, timeframe int) (map[string]int, error) {
	limit := "5000"
	query := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "history",
		Fields: &[]string{"query.formatted_fields", "query.filters", "history.query_run_count"},
		Filters: &map[string]interface{}{
			"history.created_date":   fmt.Sprintf("%d days", timeframe),
			"query.model":            strings.ReplaceAll(model, "_", "^_"),
			"query.view":             strings.ReplaceAll(explore, "_", "^_"),
			"query.formatted_fields": "-NULL",
			"history.workspace_id":   "production",
		},
		Limit: &limit,
	}
	req := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *query,
	}
	raw, err := c.SDK.RunInlineQuery(req, nil)
	if err != nil {
		return nil, err
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}

	results := make(map[string]int)
	fieldRegex := regexp.MustCompile(`(\w+\.\w+)`)

	for _, row := range data {
		count, _ := row["history.query_run_count"].(float64)
		formattedFields, _ := row["query.formatted_fields"].(string)
		filters, _ := row["query.filters"].(string)

		usedFields := make(map[string]bool)

		for _, field := range fieldRegex.FindAllString(formattedFields, -1) {
			results[field] += int(count)
			usedFields[field] = true
		}

		for _, field := range fieldRegex.FindAllString(filters, -1) {
			if _, ok := usedFields[field]; !ok {
				results[field] += int(count)
			}
		}
	}
	return results, nil
}

func init() {
	// HealthCmd is defined in health.go, we just add subcommand here
	HealthCmd.AddCommand(HealthAnalyzeCmd)
	HealthAnalyzeCmd.AddCommand(analyzeProjectsCmd)
	HealthAnalyzeCmd.AddCommand(analyzeModelsCmd)
	HealthAnalyzeCmd.AddCommand(analyzeExploresCmd)

	HealthAnalyzeCmd.PersistentFlags().IntVar(&analyzeTimeframe, "timeframe", 90, "Timeframe in days to analyze")
	HealthAnalyzeCmd.PersistentFlags().IntVar(&analyzeMinQueries, "min-queries", 1, "Minimum number of queries to consider used")
	HealthAnalyzeCmd.PersistentFlags().BoolVar(&analyzePlain, "plain", false, "print without formatting")
	HealthAnalyzeCmd.PersistentFlags().BoolVar(&analyzeCSV, "csv", false, "output in csv format")

	analyzeProjectsCmd.Flags().StringVar(&analyzeProject, "project", "", "Project ID to analyze")
	analyzeModelsCmd.Flags().StringVar(&analyzeProject, "project", "", "Filter by project")
	analyzeModelsCmd.Flags().StringVar(&analyzeModel, "model", "", "Model name to analyze")
	analyzeExploresCmd.Flags().StringVar(&analyzeModel, "model", "", "Filter by model")
	analyzeExploresCmd.Flags().StringVar(&analyzeExplore, "explore", "", "Explore name to analyze")
}
