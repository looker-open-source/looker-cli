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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	planLsFields      string
	planLsDisabled    bool
	planLsPlain       bool
	planLsCSV         bool
	planCatFields     string
	planCatDir        string
	planImportPlain   bool
	planImportEnable  bool
	planImportDisable bool
	planFailuresPlain bool
	planFailuresCSV   bool
	planRandWindow    int
	planRandAll       bool
)

var PlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Commands pertaining to scheduled plans",
}

var planLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List scheduled plans",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		req := v4.RequestAllScheduledPlans{
			Fields: &planLsFields,
		}
		req.AllUsers = ptrBool(true) // E.g. Ruby gzr ls had --all-users or just listed all? E.g. Ruby gzr ls had no --all flag but description says 'List scheduled plans on a server'. E.g. let's check if it passed all_users. E.g. earlier I saw `req[:all_users] = true if user_id == "all"`. E.g. in ls.rb it might have passed all_users=true.

		plans, err := c.SDK.AllScheduledPlans(req, nil)
		if err != nil {
			return fmt.Errorf("failed to list plans: %w", err)
		}

		headers := util.ParseFieldsForHeaders(planLsFields)

		table := util.NewTable(headers)
		for _, p := range plans {
			if planLsDisabled && p.Enabled != nil && *p.Enabled {
				continue
			}
			table.Append(extractFields(p, planLsFields))
		}
		table.Render(planLsPlain, planLsCSV)
		return nil
	},
}

var planCatCmd = &cobra.Command{
	Use:   "cat [PLAN_ID]",
	Short: "Output JSON representation of a scheduled plan",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]

		plan, err := c.SDK.ScheduledPlan(pID, planCatFields, nil)
		if err != nil {
			return err
		}

		bytes, _ := json.MarshalIndent(plan, "", "  ")
		if planCatDir != "" {
			name := ""
			if plan.Name != nil {
				name = *plan.Name
			}
			fn := filepath.Join(planCatDir, fmt.Sprintf("Plan_%s_%s.json", pID, strings.ReplaceAll(name, "/", "_")))
			if err := writeOutputFile(fn, bytes); err != nil {
				return err
			}
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(string(bytes))
		}
		return nil
	},
}

var planRmCmd = &cobra.Command{
	Use:   "rm [PLAN_ID]",
	Short: "Delete a scheduled plan",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]
		_, err = c.SDK.DeleteScheduledPlan(pID, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Scheduled plan %s deleted.\n", pID)
		return nil
	},
}

var planEnableCmd = &cobra.Command{
	Use:   "enable [PLAN_ID]",
	Short: "Enable a scheduled plan",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]

		plan, err := c.SDK.ScheduledPlan(pID, "", nil)
		if err != nil {
			return err
		}

		pb, _ := json.Marshal(plan)
		var wsp v4.WriteScheduledPlan
		_ = json.Unmarshal(pb, &wsp)
		wsp.Enabled = ptrBool(true)

		_, err = c.SDK.UpdateScheduledPlan(pID, wsp, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Scheduled plan %s enabled.\n", pID)
		return nil
	},
}

var planDisableCmd = &cobra.Command{
	Use:   "disable [PLAN_ID]",
	Short: "Disable a scheduled plan",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]

		plan, err := c.SDK.ScheduledPlan(pID, "", nil)
		if err != nil {
			return err
		}

		pb, _ := json.Marshal(plan)
		var wsp v4.WriteScheduledPlan
		_ = json.Unmarshal(pb, &wsp)
		wsp.Enabled = ptrBool(false)

		_, err = c.SDK.UpdateScheduledPlan(pID, wsp, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Scheduled plan %s disabled.\n", pID)
		return nil
	},
}

var planRunItCmd = &cobra.Command{
	Use:   "runit [PLAN_ID]",
	Short: "Execute a saved plan immediately",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		pID := args[0]

		plan, err := c.SDK.ScheduledPlan(pID, "", nil)
		if err != nil {
			return err
		}

		pb, _ := json.Marshal(plan)
		var wsp v4.WriteScheduledPlan
		_ = json.Unmarshal(pb, &wsp)

		_, err = c.SDK.ScheduledPlanRunOnceById(pID, wsp, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Executed plan %s\n", pID)
		return nil
	},
}

var planImportCmd = &cobra.Command{
	Use:   "import [PLAN_FILE] [OBJ_TYPE] [OBJ_ID]",
	Short: "Import a plan from a file",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		file := args[0]
		objType := strings.ToLower(args[1])
		objID := args[2]

		b, err := util.ReadFileOrStdin(file)
		if err != nil {
			return err
		}

		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}

		me, err := c.SDK.Me("id", nil)
		if err != nil || me.Id == nil {
			return fmt.Errorf("failed to get me: %v", err)
		}
		myID := *me.Id

		mb, _ := json.Marshal(m)
		var wsp v4.WriteScheduledPlan
		_ = json.Unmarshal(mb, &wsp)
		wsp.UserId = &myID

		switch objType {
		case "look":
			wsp.LookId = &objID
		case "dashboard":
			wsp.DashboardId = &objID
		default:
			return fmt.Errorf("invalid obj_type %s, must be look or dashboard", objType)
		}

		if planImportEnable {
			wsp.Enabled = ptrBool(true)
		}
		if planImportDisable {
			wsp.Enabled = ptrBool(false)
		}

		var existingPlans []v4.ScheduledPlan
		if objType == "look" {
			existingPlans, err = c.SDK.ScheduledPlansForLook(v4.RequestScheduledPlansForLook{LookId: objID, AllUsers: ptrBool(true)}, nil)
			if err != nil {
				return fmt.Errorf("failed to list scheduled plans for look %s: %w", objID, err)
			}
		} else {
			existingPlans, err = c.SDK.ScheduledPlansForDashboard(v4.RequestScheduledPlansForDashboard{DashboardId: objID, AllUsers: ptrBool(true)}, nil)
			if err != nil {
				return fmt.Errorf("failed to list scheduled plans for dashboard %s: %w", objID, err)
			}
		}

		var matchedPlan *v4.ScheduledPlan
		pName := ""
		if wsp.Name != nil {
			pName = *wsp.Name
		}
		for i := range existingPlans {
			ep := existingPlans[i]
			epName := ""
			if ep.Name != nil {
				epName = *ep.Name
			}
			if epName == pName && ep.UserId != nil && *ep.UserId == myID && ep.Id != nil {
				matchedPlan = &ep
				break
			}
		}

		var resultPlan *v4.ScheduledPlan
		if matchedPlan != nil {
			mpID := *matchedPlan.Id
			updated, err := c.SDK.UpdateScheduledPlan(mpID, wsp, nil)
			if err != nil {
				return err
			}
			resultPlan = &updated
		} else {
			created, err := c.SDK.CreateScheduledPlan(wsp, nil)
			if err != nil {
				return err
			}
			resultPlan = &created
		}

		idStr := ""
		if resultPlan.Id != nil {
			idStr = *resultPlan.Id
		}
		if planImportPlain {
			fmt.Println(idStr)
		} else {
			fmt.Printf("Imported plan %s\n", idStr)
		}
		return nil
	},
}

var planFailuresCmd = &cobra.Command{
	Use:   "failures",
	Short: "Report all plans that failed in their most recent run attempt",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		fields := []string{
			"scheduled_plan.id",
			"scheduled_plan.name",
			"user.id",
			"user.name",
			"scheduled_job.status",
			"scheduled_job.id",
			"scheduled_job.created_time",
			"scheduled_plan.next_run_time",
			"scheduled_plan.look_id",
			"scheduled_plan.dashboard_id",
			"scheduled_plan.lookml_dashboard_id",
		}
		wq := v4.WriteQuery{
			Model:  "i__looker",
			View:   "scheduled_plan",
			Fields: &fields,
			Filters: &map[string]interface{}{
				"scheduled_job_stage.stage":  "execute",
				"scheduled_job.created_time": "1 months",
				"scheduled_plan.run_once":    "no",
			},
			Sorts: ptrSlice([]string{"scheduled_plan.id", "scheduled_job.created_time desc"}),
			Limit: ptr("5000"),
		}

		res, err := c.SDK.RunInlineQuery(v4.RequestRunInlineQuery{ResultFormat: "json", Body: wq}, nil)
		if err != nil {
			return err
		}

		var rows []map[string]interface{}
		if err := json.Unmarshal([]byte(res), &rows); err != nil {
			return fmt.Errorf("failed to parse scheduled plan failures response: %w", err)
		}

		var tableRows [][]string
		priorID := ""
		for _, r := range rows {
			pID, _ := r["scheduled_plan.id"].(string)
			if pID == priorID {
				continue
			}
			priorID = pID
			status, _ := r["scheduled_job.status"].(string)
			if status == "success" {
				continue
			}

			tableRows = append(tableRows, mapToRow(r, fields))
		}

		table := util.NewTable(fields)
		table.Rows = tableRows
		table.Render(planFailuresPlain, planFailuresCSV)
		return nil
	},
}

func ptrSlice(s []string) *[]string { return &s }

var planRandomizeCmd = &cobra.Command{
	Use:   "randomize [PLAN_ID]",
	Short: "Randomize scheduled plans on a server",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		window := planRandWindow
		if window < 1 || window > 60 {
			return fmt.Errorf("window must be between 1 and 60")
		}

		var plans []v4.ScheduledPlan
		if len(args) > 0 {
			pID := args[0]
			p, err := c.SDK.ScheduledPlan(pID, "", nil)
			if err != nil {
				return err
			}
			plans = append(plans, p)
		} else {
			req := v4.RequestAllScheduledPlans{AllUsers: &planRandAll}
			plans, err = c.SDK.AllScheduledPlans(req, nil)
			if err != nil {
				return err
			}
		}

		var errs []error
		for _, p := range plans {
			if p.Crontab != nil && *p.Crontab != "" && p.Id != nil {
				newCron := randomizeCron(*p.Crontab, window)
				pb, _ := json.Marshal(p)
				var wsp v4.WriteScheduledPlan
				_ = json.Unmarshal(pb, &wsp)
				wsp.Crontab = &newCron
				if _, err := c.SDK.UpdateScheduledPlan(*p.Id, wsp, nil); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to randomize plan %s: %v\n", *p.Id, err)
					errs = append(errs, fmt.Errorf("failed to update scheduled plan %s: %w", *p.Id, err))
					continue
				}
				fmt.Printf("Randomized plan %s crontab to %s\n", *p.Id, newCron)
			}
		}
		return errors.Join(errs...)
	},
}

func init() {
	RootCmd.AddCommand(PlanCmd)
	PlanCmd.AddCommand(planLsCmd)
	PlanCmd.AddCommand(planCatCmd)
	PlanCmd.AddCommand(planRmCmd)
	PlanCmd.AddCommand(planEnableCmd)
	PlanCmd.AddCommand(planDisableCmd)
	PlanCmd.AddCommand(planRunItCmd)
	PlanCmd.AddCommand(planImportCmd)
	PlanCmd.AddCommand(planFailuresCmd)
	PlanCmd.AddCommand(planRandomizeCmd)

	planLsCmd.Flags().StringVar(&planLsFields, "fields", "id,enabled,name,user(id,display_name),look_id,dashboard_id,lookml_dashboard_id,crontab", "Fields to display")
	planLsCmd.Flags().BoolVar(&planLsDisabled, "disabled", false, "Retrieve disabled plans")
	planLsCmd.Flags().BoolVar(&planLsPlain, "plain", false, "print without formatting")
	planLsCmd.Flags().BoolVar(&planLsCSV, "csv", false, "output in csv format")

	planCatCmd.Flags().StringVar(&planCatDir, "dir", "", "Directory to store output file")
	planCatCmd.Flags().StringVar(&planCatFields, "fields", "", "Fields to display")

	planImportCmd.Flags().BoolVar(&planImportPlain, "plain", false, "Provide minimal response")
	planImportCmd.Flags().BoolVar(&planImportEnable, "enable", false, "Enable plan on import")
	planImportCmd.Flags().BoolVar(&planImportDisable, "disable", false, "Disable plan on import")

	planFailuresCmd.Flags().BoolVar(&planFailuresPlain, "plain", false, "print without formatting")
	planFailuresCmd.Flags().BoolVar(&planFailuresCSV, "csv", false, "output in csv format")

	planRandomizeCmd.Flags().IntVar(&planRandWindow, "window", 60, "Length of window")
	planRandomizeCmd.Flags().BoolVar(&planRandAll, "all", false, "Randomize all plans")
}
