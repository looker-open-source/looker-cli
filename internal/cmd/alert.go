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
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	alertLsFields    string
	alertLsDisabled  string
	alertLsAll       bool
	alertLsPlain     bool
	alertLsCSV       bool
	alertRandWindow  int
	alertRandAll     bool
	alertCatDir      string
	alertNotifPlain  bool
	alertNotifCSV    bool
	alertReadPlain   bool
	alertReadCSV     bool
	alertImportPlain bool
)

var AlertCmd = &cobra.Command{
	Use:   "alert",
	Short: "Commands pertaining to alerts",
}

var alertLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list alerts",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		req := v4.RequestSearchAlerts{
			Fields: &alertLsFields,
		}
		if alertLsDisabled != "" {
			b, _ := strconv.ParseBool(alertLsDisabled)
			req.Disabled = &b
		}
		if alertLsAll {
			req.AllOwners = ptrBool(true)
		}

		alerts, err := c.SDK.SearchAlerts(req, nil)
		if err != nil {
			return fmt.Errorf("failed to list alerts: %w", err)
		}

		headers := util.ParseFieldsForHeaders(alertLsFields)

		table := util.NewTable(headers)
		for _, a := range alerts {
			table.Append(extractFields(a, alertLsFields))
		}
		table.Render(alertLsPlain, alertLsCSV)
		return nil
	},
}

func randomizeCron(crontab string, window int) string {
	if window <= 0 {
		return crontab
	}
	fields := strings.Fields(crontab)
	if len(fields) < 5 {
		return crontab
	}
	min, err1 := strconv.Atoi(fields[0])
	hour, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return crontab
	}

	factor := rand.Intn(window) - (window / 2)
	total := hour*60 + min + factor
	if fields[2] == "*" && fields[4] == "*" {
		total = ((total % 1440) + 1440) % 1440
	} else {
		if total < 0 {
			total = 0
		} else if total >= 1440 {
			total = 1439
		}
	}

	fields[0] = strconv.Itoa(total % 60)
	fields[1] = strconv.Itoa(total / 60)
	return strings.Join(fields, " ")
}

var alertRandomizeCmd = &cobra.Command{
	Use:   "randomize [ALERT_ID]",
	Short: "Randomize scheduled alerts on a server",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		window := alertRandWindow
		if window < 1 || window > 60 {
			return fmt.Errorf("window must be between 1 and 60")
		}

		var alerts []v4.Alert
		if len(args) > 0 {
			aID := args[0]
			a, err := c.SDK.GetAlert(aID, nil)
			if err != nil {
				return fmt.Errorf("alert %s not found: %w", aID, err)
			}
			alerts = append(alerts, a)
		} else {
			req := v4.RequestSearchAlerts{Disabled: ptrBool(false)}
			if alertRandAll {
				req.AllOwners = ptrBool(true)
			}
			alerts, err = c.SDK.SearchAlerts(req, nil)
			if err != nil {
				return err
			}
		}

		var errs []error
		for _, a := range alerts {
			if a.Cron != "" && a.Id != nil {
				newCron := randomizeCron(a.Cron, window)
				ab, _ := json.Marshal(a)
				var wa v4.WriteAlert
				_ = json.Unmarshal(ab, &wa)
				wa.Cron = newCron
				if _, err := c.SDK.UpdateAlert(*a.Id, wa, nil); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to randomize alert %s: %v\n", *a.Id, err)
					errs = append(errs, fmt.Errorf("failed to update alert %s: %w", *a.Id, err))
					continue
				}
				fmt.Printf("Randomized alert %s cron to %s\n", *a.Id, newCron)
			}
		}
		return errors.Join(errs...)
	},
}

var alertCatCmd = &cobra.Command{
	Use:   "cat [ALERT_ID]",
	Short: "Output json information about an alert",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		a, err := c.SDK.GetAlert(aID, nil)
		if err != nil {
			return err
		}

		bytes, _ := json.MarshalIndent(a, "", "  ")
		if alertCatDir != "" {
			t := ""
			if a.CustomTitle != nil {
				t = *a.CustomTitle
			}
			fn := filepath.Join(alertCatDir, fmt.Sprintf("Alert_%s_%s.json", aID, strings.ReplaceAll(t, "/", "_")))
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

var alertFollowCmd = &cobra.Command{
	Use:   "follow [ALERT_ID]",
	Short: "Start following an alert",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		err = c.SDK.FollowAlert(aID, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Following alert %s\n", aID)
		return nil
	},
}

var alertUnfollowCmd = &cobra.Command{
	Use:   "unfollow [ALERT_ID]",
	Short: "Stop following an alert",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		err = c.SDK.UnfollowAlert(aID, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Unfollowed alert %s\n", aID)
		return nil
	},
}

var alertEnableCmd = &cobra.Command{
	Use:   "enable [ALERT_ID]",
	Short: "Enable an alert",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		_, err = c.SDK.UpdateAlertField(aID, v4.AlertPatch{IsDisabled: ptrBool(false)}, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Alert %s enabled\n", aID)
		return nil
	},
}

var alertDisableCmd = &cobra.Command{
	Use:   "disable [ALERT_ID] [REASON]",
	Short: "Disable an alert",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		reason := args[1]
		_, err = c.SDK.UpdateAlertField(aID, v4.AlertPatch{IsDisabled: ptrBool(true), DisabledReason: &reason}, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Alert %s disabled\n", aID)
		return nil
	},
}

var alertThresholdCmd = &cobra.Command{
	Use:   "threshold [ALERT_ID] [THRESHOLD]",
	Short: "Change threshold of an alert",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		val, err := strconv.ParseFloat(args[1], 64)
		if err != nil {
			return err
		}
		_, err = c.SDK.UpdateAlertField(aID, v4.AlertPatch{Threshold: &val}, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Alert %s threshold updated to %f\n", aID, val)
		return nil
	},
}

var alertRmCmd = &cobra.Command{
	Use:   "rm [ALERT_ID]",
	Short: "Delete an alert",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		err = c.SDK.DeleteAlert(aID, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Alert %s deleted\n", aID)
		return nil
	},
}

var alertChownCmd = &cobra.Command{
	Use:   "chown [ALERT_ID] [OWNER_ID]",
	Short: "Change owner of an alert",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		aID := args[0]
		ownerID := args[1]
		_, err = c.SDK.UpdateAlertField(aID, v4.AlertPatch{OwnerId: &ownerID}, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Alert %s owner changed to %s\n", aID, ownerID)
		return nil
	},
}

var alertNotifCmd = &cobra.Command{
	Use:   "notifications",
	Short: "Get notifications",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		var limit int64 = 64
		var offset int64 = 0
		notifs, err := c.SDK.AlertNotifications(v4.RequestAlertNotifications{Limit: &limit, Offset: &offset}, nil)
		if err != nil {
			return err
		}

		headers := []string{"id", "alert.custom_title", "is_read", "created_at"}
		table := util.NewTable(headers)
		for _, n := range notifs {
			table.Append(extractFields(n, strings.Join(headers, ",")))
		}
		table.Render(alertNotifPlain, alertNotifCSV)
		return nil
	},
}

var alertReadCmd = &cobra.Command{
	Use:   "read [NOTIFICATION_ID]",
	Short: "Read notification id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		nID := args[0]
		notif, err := c.SDK.ReadAlertNotification(nID, nil)
		if err != nil {
			return err
		}

		headers := []string{"id", "alert.custom_title", "is_read", "created_at"}
		table := util.NewTable(headers)
		table.Append(extractFields(notif, strings.Join(headers, ",")))
		table.Render(alertReadPlain, alertReadCSV)
		return nil
	},
}

var alertImportCmd = &cobra.Command{
	Use:   "import [FILE] [DASHBOARD_ELEMENT_ID]",
	Short: "Import an alert from a file",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		file := args[0]
		elemID := ""
		if len(args) > 1 {
			elemID = args[1]
		}

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
		var wa v4.WriteAlert
		_ = json.Unmarshal(mb, &wa)
		wa.OwnerId = myID
		if elemID != "" {
			wa.DashboardElementId = &elemID
		}

		alert, err := c.SDK.CreateAlert(wa, nil)
		if err != nil {
			return err
		}

		idStr := ""
		if alert.Id != nil {
			idStr = *alert.Id
		}
		if alertImportPlain {
			fmt.Println(idStr)
		} else {
			fmt.Printf("Imported alert %s\n", idStr)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(AlertCmd)
	AlertCmd.AddCommand(alertLsCmd)
	AlertCmd.AddCommand(alertRandomizeCmd)
	AlertCmd.AddCommand(alertCatCmd)
	AlertCmd.AddCommand(alertFollowCmd)
	AlertCmd.AddCommand(alertUnfollowCmd)
	AlertCmd.AddCommand(alertEnableCmd)
	AlertCmd.AddCommand(alertDisableCmd)
	AlertCmd.AddCommand(alertThresholdCmd)
	AlertCmd.AddCommand(alertRmCmd)
	AlertCmd.AddCommand(alertChownCmd)
	AlertCmd.AddCommand(alertNotifCmd)
	AlertCmd.AddCommand(alertReadCmd)
	AlertCmd.AddCommand(alertImportCmd)

	alertLsCmd.Flags().StringVar(&alertLsFields, "fields", "id,field(title,name),comparison_type,threshold,cron,custom_title,dashboard_element_id,description", "Fields to display")
	alertLsCmd.Flags().StringVar(&alertLsDisabled, "disabled", "", "return disabled alerts (true/false)")
	alertLsCmd.Flags().BoolVar(&alertLsAll, "all", false, "return alerts from all users")
	alertLsCmd.Flags().BoolVar(&alertLsPlain, "plain", false, "print without formatting")
	alertLsCmd.Flags().BoolVar(&alertLsCSV, "csv", false, "output in csv format")

	alertRandomizeCmd.Flags().IntVar(&alertRandWindow, "window", 60, "Length of window")
	alertRandomizeCmd.Flags().BoolVar(&alertRandAll, "all", false, "Randomize all alerts")

	alertCatCmd.Flags().StringVar(&alertCatDir, "dir", "", "Directory to store output file")

	alertNotifCmd.Flags().BoolVar(&alertNotifPlain, "plain", false, "print without formatting")
	alertNotifCmd.Flags().BoolVar(&alertNotifCSV, "csv", false, "output in csv format")

	alertReadCmd.Flags().BoolVar(&alertReadPlain, "plain", false, "print without formatting")
	alertReadCmd.Flags().BoolVar(&alertReadCSV, "csv", false, "output in csv format")

	alertImportCmd.Flags().BoolVar(&alertImportPlain, "plain", false, "Provide minimal response")
}
