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
	"strings"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	healthPulsePlain bool
	healthPulseCSV   bool
)

var HealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Health checks for Looker",
}

var HealthPulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Run health pulse checks",
	Long:  `Runs all health pulse checks or specific ones via subcommands.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If run without subcommands, run all checks
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, "Running all health pulse checks...")
		fmt.Fprintln(os.Stderr, "=================================")

		var errs []error
		if err := runDBConnectionsCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking DB connections: %v\n", err)
			errs = append(errs, fmt.Errorf("db-connections: %w", err))
		}
		fmt.Fprintln(os.Stderr)

		if err := runDashboardPerformanceCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking dashboard performance: %v\n", err)
			errs = append(errs, fmt.Errorf("dashboard-performance: %w", err))
		}
		fmt.Fprintln(os.Stderr)

		if err := runDashboardErrorsCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking dashboard errors: %v\n", err)
			errs = append(errs, fmt.Errorf("dashboard-errors: %w", err))
		}
		fmt.Fprintln(os.Stderr)

		if err := runExplorePerformanceCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking explore performance: %v\n", err)
			errs = append(errs, fmt.Errorf("explore-performance: %w", err))
		}
		fmt.Fprintln(os.Stderr)

		if err := runScheduleFailuresCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking schedule failures: %v\n", err)
			errs = append(errs, fmt.Errorf("schedule-failures: %w", err))
		}
		fmt.Fprintln(os.Stderr)

		if err := runLegacyFeaturesCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking legacy features: %v\n", err)
			errs = append(errs, fmt.Errorf("legacy-features: %w", err))
		}

		return errors.Join(errs...)
	},
}

var pulseDBConnectionsCmd = &cobra.Command{
	Use:   "db-connections",
	Short: "Check database connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runDBConnectionsCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV)
	},
}

var pulseDashboardPerformanceCmd = &cobra.Command{
	Use:   "dashboard-performance",
	Short: "Check dashboard performance",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runDashboardPerformanceCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV)
	},
}

var pulseDashboardErrorsCmd = &cobra.Command{
	Use:   "dashboard-errors",
	Short: "Check dashboard errors",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runDashboardErrorsCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV)
	},
}

var pulseExplorePerformanceCmd = &cobra.Command{
	Use:   "explore-performance",
	Short: "Check explore performance",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runExplorePerformanceCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV)
	},
}

var pulseScheduleFailuresCmd = &cobra.Command{
	Use:   "schedule-failures",
	Short: "Check schedule failures",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runScheduleFailuresCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV)
	},
}

var pulseLegacyFeaturesCmd = &cobra.Command{
	Use:   "legacy-features",
	Short: "Check legacy features",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		return runLegacyFeaturesCheck(cmd.Context(), c, healthPulsePlain, healthPulseCSV)
	},
}

func runDBConnectionsCheck(ctx context.Context, c *client.ClientWrapper, plain, csv bool) error {
	fmt.Fprintln(os.Stderr, "Test 1/6: Checking DB Connections")
	reservedNames := map[string]struct{}{
		"looker__internal__analytics__replica": {},
		"looker__internal__analytics":          {},
		"looker":                               {},
		"looker__ilooker":                      {},
	}

	connections, err := c.SDK.AllConnections("", nil)
	if err != nil {
		return fmt.Errorf("error fetching connections: %w", err)
	}

	var filteredConnections []v4.DBConnection
	for _, conn := range connections {
		if conn.Name == nil {
			continue
		}
		if _, reserved := reservedNames[*conn.Name]; !reserved {
			filteredConnections = append(filteredConnections, conn)
		}
	}
	if len(filteredConnections) == 0 {
		fmt.Fprintln(os.Stderr, "No connections found.")
		return nil
	}

	headers := []string{"Connection", "Status", "Errors", "Query Count (90d)"}
	table := util.NewTable(headers)

	for _, conn := range filteredConnections {
		var errors []string
		resp, err := c.SDK.TestConnection(*conn.Name, nil, nil)
		if err != nil {
			errors = append(errors, "API Error")
		} else {
			for _, r := range resp {
				if r.Status != nil && *r.Status == "error" && r.Message != nil {
					errors = append(errors, *r.Message)
				}
			}
		}

		status := "OK"
		if len(errors) > 0 {
			status = "ERROR"
		}

		// Run inline query for connection activity
		limit := "1"
		query := &v4.WriteQuery{
			Model:  "system__activity",
			View:   "history",
			Fields: &[]string{"history.query_run_count"},
			Filters: &map[string]interface{}{
				"history.connection_name": *conn.Name,
				"history.created_date":    "90 days",
				"user.dev_branch_name":    "NULL",
			},
			Limit: &limit,
		}

		req := v4.RequestRunInlineQuery{
			ResultFormat: "json",
			Body:         *query,
		}
		raw, err := c.SDK.RunInlineQuery(req, nil)

		queryRunCount := "N/A"
		if err == nil {
			var data []map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &data); err == nil && len(data) > 0 {
				if val, ok := data[0]["history.query_run_count"]; ok {
					queryRunCount = fmt.Sprintf("%v", val)
				}
			}
		}

		table.Append([]string{
			*conn.Name,
			status,
			strings.Join(errors, "; "),
			queryRunCount,
		})
	}

	table.Render(plain, csv)
	return nil
}

func runDashboardPerformanceCheck(ctx context.Context, c *client.ClientWrapper, plain, csv bool) error {
	fmt.Fprintln(os.Stderr, "Test 2/6: Checking for dashboards with queries slower than 30 seconds in the last 7 days")
	limit := "20"
	query := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "history",
		Fields: &[]string{"dashboard.title", "query.count"},
		Filters: &map[string]interface{}{
			"history.created_date": "7 days",
			"history.real_dash_id": "-NULL",
			"history.runtime":      ">30",
			"history.status":       "complete",
		},
		Sorts: &[]string{"query.count desc"},
		Limit: &limit,
	}

	req := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *query,
	}
	raw, err := c.SDK.RunInlineQuery(req, nil)
	if err != nil {
		return err
	}

	var dashboards []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &dashboards); err != nil {
		return err
	}

	headers := []string{"Dashboard Title", "Slow Query Count"}
	table := util.NewTable(headers)
	for _, d := range dashboards {
		title := ""
		if val, ok := d["dashboard.title"]; ok && val != nil {
			title = fmt.Sprintf("%v", val)
		}
		count := ""
		if val, ok := d["query.count"]; ok && val != nil {
			count = fmt.Sprintf("%v", val)
		}
		table.Append([]string{title, count})
	}
	table.Render(plain, csv)
	return nil
}

func runDashboardErrorsCheck(ctx context.Context, c *client.ClientWrapper, plain, csv bool) error {
	fmt.Fprintln(os.Stderr, "Test 3/6: Checking for dashboards with erroring queries in the last 7 days")
	limit := "20"
	query := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "history",
		Fields: &[]string{"dashboard.title", "history.query_run_count"},
		Filters: &map[string]interface{}{
			"dashboard.title":           "-NULL",
			"history.created_date":      "7 days",
			"history.dashboard_session": "-NULL",
			"history.status":            "error",
		},
		Sorts: &[]string{"history.query_run_count desc"},
		Limit: &limit,
	}

	req := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *query,
	}
	raw, err := c.SDK.RunInlineQuery(req, nil)
	if err != nil {
		return err
	}

	var dashboards []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &dashboards); err != nil {
		return err
	}

	headers := []string{"Dashboard Title", "Erroring Query Count"}
	table := util.NewTable(headers)
	for _, d := range dashboards {
		title := ""
		if val, ok := d["dashboard.title"]; ok && val != nil {
			title = fmt.Sprintf("%v", val)
		}
		count := ""
		if val, ok := d["history.query_run_count"]; ok && val != nil {
			count = fmt.Sprintf("%v", val)
		}
		table.Append([]string{title, count})
	}
	table.Render(plain, csv)
	return nil
}

func runExplorePerformanceCheck(ctx context.Context, c *client.ClientWrapper, plain, csv bool) error {
	fmt.Fprintln(os.Stderr, "Test 4/6: Checking for the slowest explores in the past 7 days")
	limit := "20"
	query := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "history",
		Fields: &[]string{"query.model", "query.view", "history.average_runtime"},
		Filters: &map[string]interface{}{
			"history.created_date": "7 days",
			"query.model":          "-NULL, -system^_^_activity",
		},
		Sorts: &[]string{"history.average_runtime desc"},
		Limit: &limit,
	}

	req := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *query,
	}
	raw, err := c.SDK.RunInlineQuery(req, nil)
	if err != nil {
		return err
	}

	var explores []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &explores); err != nil {
		return err
	}

	// Average query runtime for context
	queryAvg := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "history",
		Fields: &[]string{"history.average_runtime"},
		Filters: &map[string]interface{}{
			"history.created_date": "7 days",
			"query.model":          "-NULL, -system^_^_activity",
		},
		Limit: &limit,
	}
	reqAvg := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *queryAvg,
	}
	rawAvg, err := c.SDK.RunInlineQuery(reqAvg, nil)
	if err == nil {
		var avgData []map[string]interface{}
		if err := json.Unmarshal([]byte(rawAvg), &avgData); err == nil && len(avgData) > 0 {
			if avgRuntime, ok := avgData[0]["history.average_runtime"].(float64); ok {
				fmt.Fprintf(os.Stderr, "For context, the average query runtime is %.4fs\n", avgRuntime)
			}
		}
	}

	headers := []string{"Model", "Explore (View)", "Average Runtime (s)"}
	table := util.NewTable(headers)
	for _, e := range explores {
		model := ""
		if val, ok := e["query.model"]; ok && val != nil {
			model = fmt.Sprintf("%v", val)
		}
		view := ""
		if val, ok := e["query.view"]; ok && val != nil {
			view = fmt.Sprintf("%v", val)
		}
		runtime := ""
		if val, ok := e["history.average_runtime"]; ok && val != nil {
			runtime = fmt.Sprintf("%v", val)
		}
		table.Append([]string{model, view, runtime})
	}
	table.Render(plain, csv)
	return nil
}

func runScheduleFailuresCheck(ctx context.Context, c *client.ClientWrapper, plain, csv bool) error {
	fmt.Fprintln(os.Stderr, "Test 5/6: Checking for failing schedules")
	limit := "500"
	query := &v4.WriteQuery{
		Model:  "system__activity",
		View:   "scheduled_plan",
		Fields: &[]string{"scheduled_job.name", "scheduled_job.count"},
		Filters: &map[string]interface{}{
			"scheduled_job.created_date": "7 days",
			"scheduled_job.status":       "failure",
		},
		Sorts: &[]string{"scheduled_job.count desc"},
		Limit: &limit,
	}

	req := v4.RequestRunInlineQuery{
		ResultFormat: "json",
		Body:         *query,
	}
	raw, err := c.SDK.RunInlineQuery(req, nil)
	if err != nil {
		return err
	}

	var schedules []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &schedules); err != nil {
		return err
	}

	headers := []string{"Schedule Job Name", "Failure Count"}
	table := util.NewTable(headers)
	for _, s := range schedules {
		name := ""
		if val, ok := s["scheduled_job.name"]; ok && val != nil {
			name = fmt.Sprintf("%v", val)
		}
		count := ""
		if val, ok := s["scheduled_job.count"]; ok && val != nil {
			count = fmt.Sprintf("%v", val)
		}
		table.Append([]string{name, count})
	}
	table.Render(plain, csv)
	return nil
}

func runLegacyFeaturesCheck(ctx context.Context, c *client.ClientWrapper, plain, csv bool) error {
	fmt.Fprintln(os.Stderr, "Test 6/6: Checking for enabled legacy features")
	features, err := c.SDK.AllLegacyFeatures(nil)
	if err != nil {
		if strings.Contains(err.Error(), "Unsupported in Looker (Google Cloud core)") {
			fmt.Fprintln(os.Stderr, "Legacy features check: Unsupported in Looker (Google Cloud core)")
			return nil
		}
		return err
	}

	headers := []string{"Enabled Legacy Feature"}
	table := util.NewTable(headers)
	hasLegacy := false
	for _, f := range features {
		if f.Enabled != nil && *f.Enabled && f.Name != nil {
			table.Append([]string{*f.Name})
			hasLegacy = true
		}
	}

	if !hasLegacy {
		fmt.Fprintln(os.Stderr, "No legacy features enabled.")
		return nil
	}

	table.Render(plain, csv)
	return nil
}

func init() {
	RootCmd.AddCommand(HealthCmd)
	HealthCmd.AddCommand(HealthPulseCmd)

	HealthPulseCmd.AddCommand(pulseDBConnectionsCmd)
	HealthPulseCmd.AddCommand(pulseDashboardPerformanceCmd)
	HealthPulseCmd.AddCommand(pulseDashboardErrorsCmd)
	HealthPulseCmd.AddCommand(pulseExplorePerformanceCmd)
	HealthPulseCmd.AddCommand(pulseScheduleFailuresCmd)
	HealthPulseCmd.AddCommand(pulseLegacyFeaturesCmd)

	HealthPulseCmd.PersistentFlags().BoolVar(&healthPulsePlain, "plain", false, "print without formatting")
	HealthPulseCmd.PersistentFlags().BoolVar(&healthPulseCSV, "csv", false, "output in csv format")
}
