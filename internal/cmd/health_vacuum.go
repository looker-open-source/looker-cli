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
	"slices"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	vacuumProject    string
	vacuumModel      string
	vacuumExplore    string
	vacuumTimeframe  int
	vacuumMinQueries int
	vacuumPlain      bool
	vacuumCSV        bool
)

var HealthVacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Identify unused Looker assets for cleanup",
	Long:  `Helps identify unused explores, joins, and fields that can be removed to clean up LookML.`,
}

var vacuumModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Identify unused explores in models",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runVacuumModels(cmd.Context(), c, vacuumProject, vacuumModel, vacuumTimeframe, vacuumMinQueries, vacuumPlain, vacuumCSV)
	},
}

var vacuumExploresCmd = &cobra.Command{
	Use:   "explores",
	Short: "Identify unused joins and fields in explores",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runVacuumExplores(cmd.Context(), c, vacuumModel, vacuumExplore, vacuumTimeframe, vacuumMinQueries, vacuumPlain, vacuumCSV)
	},
}

func runVacuumModels(ctx context.Context, c *client.ClientWrapper, project, model string, timeframe, minQueries int, plain, csv bool) error {
	usedModels, err := getUsedModels(ctx, c, timeframe, minQueries)
	if err != nil {
		return fmt.Errorf("error fetching used models: %w", err)
	}

	lookmlModels, err := c.SDK.AllLookmlModels(v4.RequestAllLookmlModels{}, nil)
	if err != nil {
		return fmt.Errorf("error fetching LookML models: %w", err)
	}

	headers := []string{"Model", "Model Query Count", "Unused Explores"}
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

			unusedExplores, err := getUnusedExplores(ctx, c, *m.Name, timeframe, minQueries)
			if err != nil {
				return fmt.Errorf("error checking unused explores for model %s: %w", *m.Name, err)
			}

			table.Append([]string{
				*m.Name,
				fmt.Sprintf("%d", queryCount),
				strings.Join(unusedExplores, ", "),
			})
		}
	}

	table.Render(plain, csv)
	return nil
}

func getUnusedExplores(ctx context.Context, c *client.ClientWrapper, modelName string, timeframe, minQueries int) ([]string, error) {
	lookmlModel, err := c.SDK.LookmlModel(modelName, "", nil)
	if err != nil {
		return nil, fmt.Errorf("error fetching LookML model %s: %w", modelName, err)
	}

	var unusedExplores []string
	if lookmlModel.Explores != nil {
		for _, e := range *lookmlModel.Explores {
			if e.Name == nil {
				continue
			}
			limit := "1"
			queryCountQueryBody := &v4.WriteQuery{
				Model:  "system__activity",
				View:   "history",
				Fields: &[]string{"history.query_run_count"},
				Filters: &map[string]interface{}{
					"query.model":             modelName,
					"query.view":              *e.Name,
					"history.created_date":    fmt.Sprintf("%d days", timeframe),
					"history.query_run_count": fmt.Sprintf(">%d", minQueries-1),
					"user.dev_branch_name":    "NULL",
				},
				Limit: &limit,
			}

			req := v4.RequestRunInlineQuery{
				ResultFormat: "json",
				Body:         *queryCountQueryBody,
			}
			rawQueryCount, err := c.SDK.RunInlineQuery(req, nil)
			if err != nil {
				return nil, fmt.Errorf("error querying usage for explore %s.%s: %w", modelName, *e.Name, err)
			}

			var data []map[string]interface{}
			if err := json.Unmarshal([]byte(rawQueryCount), &data); err != nil {
				return nil, fmt.Errorf("error parsing usage response for explore %s.%s: %w", modelName, *e.Name, err)
			}
			if len(data) == 0 {
				unusedExplores = append(unusedExplores, *e.Name)
			}
		}
	}
	return unusedExplores, nil
}

func runVacuumExplores(ctx context.Context, c *client.ClientWrapper, model, explore string, timeframe, minQueries int, plain, csv bool) error {
	lookmlModels, err := c.SDK.AllLookmlModels(v4.RequestAllLookmlModels{}, nil)
	if err != nil {
		return fmt.Errorf("error fetching LookML models: %w", err)
	}

	headers := []string{"Model", "Explore", "Unused Joins", "Unused Fields"}
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

			exploreDetail, err := c.SDK.LookmlModelExplore(v4.RequestLookmlModelExplore{
				LookmlModelName: *m.Name,
				ExploreName:     *e.Name,
			}, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching detail for explore %s.%s: %v\n", *m.Name, *e.Name, err)
				errs = append(errs, fmt.Errorf("error fetching detail for explore %s.%s: %w", *m.Name, *e.Name, err))
				continue
			}

			usedFields, err := getUsedExploreFields(ctx, c, *m.Name, *e.Name, timeframe)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching used fields for explore %s.%s: %v\n", *m.Name, *e.Name, err)
				errs = append(errs, fmt.Errorf("error fetching used fields for explore %s.%s: %w", *m.Name, *e.Name, err))
				continue
			}

			var allFields []string
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

			var unusedFields []string
			for _, field := range allFields {
				if _, ok := usedFields[field]; !ok {
					unusedFields = append(unusedFields, field)
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

			var unusedJoins []string
			for join, count := range joinStats {
				if count == 0 {
					unusedJoins = append(unusedJoins, join)
				}
			}
			slices.Sort(unusedJoins)

			table.Append([]string{
				*m.Name,
				*e.Name,
				strings.Join(unusedJoins, ", "),
				strings.Join(unusedFields, ", "),
			})
		}
	}

	table.Render(plain, csv)
	return errors.Join(errs...)
}

func init() {
	HealthCmd.AddCommand(HealthVacuumCmd)
	HealthVacuumCmd.AddCommand(vacuumModelsCmd)
	HealthVacuumCmd.AddCommand(vacuumExploresCmd)

	HealthVacuumCmd.PersistentFlags().IntVar(&vacuumTimeframe, "timeframe", 90, "Timeframe in days to analyze")
	HealthVacuumCmd.PersistentFlags().IntVar(&vacuumMinQueries, "min-queries", 1, "Minimum number of queries to consider used")
	HealthVacuumCmd.PersistentFlags().BoolVar(&vacuumPlain, "plain", false, "print without formatting")
	HealthVacuumCmd.PersistentFlags().BoolVar(&vacuumCSV, "csv", false, "output in csv format")

	vacuumModelsCmd.Flags().StringVar(&vacuumProject, "project", "", "Filter by project")
	vacuumModelsCmd.Flags().StringVar(&vacuumModel, "model", "", "Model name to vacuum")
	vacuumExploresCmd.Flags().StringVar(&vacuumModel, "model", "", "Filter by model")
	vacuumExploresCmd.Flags().StringVar(&vacuumExplore, "explore", "", "Explore name to vacuum")
}
