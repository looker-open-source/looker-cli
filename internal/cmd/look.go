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
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

var (
	lookCatFields   string
	lookCatDir      string
	lookCatTrim     bool
	lookCatPlans    bool
	lookImportForce bool
	lookImportPlain bool
	lookMvForce     bool
	lookMvPlain     bool
)

var LookCmd = &cobra.Command{
	Use:   "look",
	Short: "Commands pertaining to looks",
}

func ptrBool(b bool) *bool { return &b }

func CatLookHelper(c *client.ClientWrapper, lID string, trim bool, plans bool, fields string) (map[string]interface{}, error) {
	look, err := c.SDK.Look(lID, fields, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get look %s: %w", lID, err)
	}

	b, _ := json.Marshal(look)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)

	if plans {
		p, _ := c.SDK.ScheduledPlansForLook(v4.RequestScheduledPlansForLook{LookId: lID, AllUsers: ptrBool(true)}, nil)
		m["scheduled_plans"] = p
	}

	if trim {
		keepLook := map[string]bool{
			"id":              true,
			"title":           true,
			"description":     true,
			"is_run_on_load":  true,
			"public":          true,
			"query_id":        true,
			"folder_id":       true,
			"user_id":         true,
			"query":           true,
			"scheduled_plans": true,
		}
		for k := range m {
			if !keepLook[k] {
				delete(m, k)
			}
		}

		if qVal, ok := m["query"].(map[string]interface{}); ok {
			keepQuery := map[string]bool{
				"id":                  true,
				"model":               true,
				"view":                true,
				"fields":              true,
				"pivots":              true,
				"fill_fields":         true,
				"filters":             true,
				"filter_expression":   true,
				"sorts":               true,
				"limit":               true,
				"column_limit":        true,
				"total":               true,
				"row_total":           true,
				"subtotals":           true,
				"vis_config":          true,
				"filter_config":       true,
				"visible_ui_sections": true,
				"dynamic_fields":      true,
				"query_timezone":      true,
			}
			for k := range qVal {
				if !keepQuery[k] {
					delete(qVal, k)
				}
			}
		}

		if pVal, ok := m["scheduled_plans"].([]interface{}); ok {
			keepPlan := map[string]bool{
				"id":                         true,
				"name":                       true,
				"user_id":                    true,
				"run_as_recipient":           true,
				"enabled":                    true,
				"look_id":                    true,
				"dashboard_id":               true,
				"filters_string":             true,
				"require_results":            true,
				"require_no_results":         true,
				"require_change":             true,
				"send_all_results":           true,
				"crontab":                    true,
				"datagroup":                  true,
				"timezone":                   true,
				"scheduled_plan_destination": true,
				"run_once":                   true,
				"include_links":              true,
				"custom_url_base":            true,
				"custom_url_params":          true,
				"custom_url_label":           true,
				"show_custom_url":            true,
				"pdf_paper_size":             true,
				"pdf_landscape":              true,
				"color_theme":                true,
				"long_tables":                true,
				"inline_table_width":         true,
			}
			for i, pv := range pVal {
				if pm, ok := pv.(map[string]interface{}); ok {
					for k := range pm {
						if !keepPlan[k] {
							delete(pm, k)
						}
					}
					if destVal, ok := pm["scheduled_plan_destination"].([]interface{}); ok {
						for _, dv := range destVal {
							if dm, ok := dv.(map[string]interface{}); ok {
								delete(dm, "id")
								delete(dm, "scheduled_plan_id")
								delete(dm, "looker_recipient")
								delete(dm, "can")
							}
						}
					}
					pVal[i] = pm
				}
			}
		}
	}
	return m, nil
}

var lookCatCmd = &cobra.Command{
	Use:   "cat [LOOK_ID]",
	Short: "output json describing a look",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		lID := args[0]
		m, err := CatLookHelper(c, lID, lookCatTrim, lookCatPlans, lookCatFields)
		if err != nil {
			return err
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if lookCatDir != "" {
			title := ""
			if v, ok := m["title"].(string); ok { title = v }
			fn := fmt.Sprintf("%s/Look_%s_%s.json", lookCatDir, lID, strings.ReplaceAll(title, "/", "_"))
			_ = os.WriteFile(fn, outBytes, 0644)
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(string(outBytes))
		}
		return nil
	},
}

var lookRmCmd = &cobra.Command{
	Use:   "rm [LOOK_ID]",
	Short: "delete a look",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		lID := args[0]
		_, err = c.SDK.DeleteLook(lID, nil)
		if err != nil {
			return fmt.Errorf("failed to delete look %s: %w", lID, err)
		}
		fmt.Printf("Look %s deleted.\n", lID)
		return nil
	},
}

var lookImportCmd = &cobra.Command{
	Use:   "import [LOOK_FILE] [FOLDER_ID]",
	Short: "import a look from a file",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		file := args[0]
		folderID := args[1]

		b, err := util.ReadFileOrStdin(file)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("invalid json in %s: %w", file, err)
		}

		if _, ok := m["dashboard_elements"]; ok {
			return fmt.Errorf("file contains dashboard_elements! Is this a dashboard?")
		}

		me, err := c.SDK.Me("id", nil)
		if err != nil || me.Id == nil {
			return fmt.Errorf("failed to get me: %v", err)
		}
		myID := *me.Id

		qVal, ok := m["query"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("look file missing query object")
		}
		qb, _ := json.Marshal(qVal)
		var wq v4.WriteQuery
		_ = json.Unmarshal(qb, &wq)
		wq.ClientId = nil
		createdQuery, err := c.SDK.CreateQuery(wq, "", nil)
		if err != nil || createdQuery.Id == nil {
			return fmt.Errorf("failed to create query: %v", err)
		}
		qID := *createdQuery.Id

		title := ""
		if v, ok := m["title"].(string); ok { title = v }
		slug := ""
		if v, ok := m["slug"].(string); ok { slug = v }

		var existingLook *v4.LookWithQuery

		folderLooks, err := c.SDK.FolderLooks(folderID, "", nil)
		if err == nil {
			for i, fl := range folderLooks {
				if fl.Id == nil {
					continue
				}
				if slug != "" && fl.PublicSlug != nil && *fl.PublicSlug == slug {
					existingLook = &folderLooks[i]
					break
				}
				if title != "" && fl.Title != nil && *fl.Title == title {
					existingLook = &folderLooks[i]
					break
				}
			}
		}

		lb, _ := json.Marshal(m)
		var wl v4.WriteLookWithQuery
		_ = json.Unmarshal(lb, &wl)
		wl.QueryId = &qID
		wl.FolderId = &folderID
		wl.UserId = &myID

		var resultLook *v4.LookWithQuery

		if existingLook != nil {
			if !lookImportForce {
				return fmt.Errorf("look '%s' already exists in folder %s. Use --force to overwrite", title, folderID)
			}
			elID := *existingLook.Id
			updated, err := c.SDK.UpdateLook(elID, wl, "", nil)
			if err != nil {
				return fmt.Errorf("failed to update look %s: %w", elID, err)
			}
			resultLook = &updated
		} else {
			created, err := c.SDK.CreateLook(wl, "", nil)
			if err != nil {
				return fmt.Errorf("failed to create look: %w", err)
			}
			resultLook = &created
		}

		resID := *resultLook.Id

		if plansVal, ok := m["scheduled_plans"].([]interface{}); ok && len(plansVal) > 0 {
			existingPlans, _ := c.SDK.ScheduledPlansForLook(v4.RequestScheduledPlansForLook{LookId: resID, AllUsers: ptrBool(true)}, nil)
			myIDInt, _ := strconv.ParseInt(myID, 10, 64)

			for _, pv := range plansVal {
				pb, _ := json.Marshal(pv)
				var wsp v4.WriteScheduledPlan
				_ = json.Unmarshal(pb, &wsp)
				wsp.LookId = &resID
				wsp.UserId = &myID

				var matchedPlan *v4.ScheduledPlan
				pName := ""
				if wsp.Name != nil { pName = *wsp.Name }
				for _, ep := range existingPlans {
					epName := ""
					if ep.Name != nil { epName = *ep.Name }
					if epName == pName && ep.UserId != nil && *ep.UserId == strconv.FormatInt(myIDInt, 10) {
						matchedPlan = &ep
						break
					}
				}

				if matchedPlan != nil {
					mpID := *matchedPlan.Id
					_, _ = c.SDK.UpdateScheduledPlan(mpID, wsp, nil)
				} else {
					_, _ = c.SDK.CreateScheduledPlan(wsp, nil)
				}
			}
		}

		if lookImportPlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Imported look %s\n", resID)
		}
		return nil
	},
}

func UpsertLookHelper(c *client.ClientWrapper, folderID, myID string, m map[string]interface{}, force bool) (string, error) {
	title := ""
	if v, ok := m["title"].(string); ok { title = v }
	slug := ""
	if v, ok := m["slug"].(string); ok { slug = v }
	if slug == "" {
		if v, ok := m["public_slug"].(string); ok { slug = v }
	}

	var existingLook *v4.LookWithQuery
	folderLooks, err := c.SDK.FolderLooks(folderID, "", nil)
	if err == nil {
		for i, fl := range folderLooks {
			if fl.Id == nil {
				continue
			}
			if slug != "" && fl.PublicSlug != nil && *fl.PublicSlug == slug {
				existingLook = &folderLooks[i]
				break
			}
			if title != "" && fl.Title != nil && *fl.Title == title {
				existingLook = &folderLooks[i]
				break
			}
		}
	}

	if existingLook != nil && !force {
		return *existingLook.Id, nil
	}

	qVal, ok := m["query"].(map[string]interface{})
	if !ok {
		if existingLook != nil {
			return *existingLook.Id, nil
		}
		return "", fmt.Errorf("look missing query")
	}
	qb, _ := json.Marshal(qVal)
	var wq v4.WriteQuery
	_ = json.Unmarshal(qb, &wq)
	wq.ClientId = nil
	createdQuery, err := c.SDK.CreateQuery(wq, "", nil)
	if err != nil || createdQuery.Id == nil {
		return "", fmt.Errorf("failed to create query: %v", err)
	}
	qID := *createdQuery.Id

	lb, _ := json.Marshal(m)
	var wl v4.WriteLookWithQuery
	_ = json.Unmarshal(lb, &wl)
	wl.QueryId = &qID
	wl.FolderId = &folderID
	wl.UserId = &myID
	wl.Query = nil

	var resultLook *v4.LookWithQuery
	if existingLook != nil {
		elID := *existingLook.Id
		updated, err := c.SDK.UpdateLook(elID, wl, "", nil)
		if err != nil {
			return "", err
		}
		resultLook = &updated
	} else {
		created, err := c.SDK.CreateLook(wl, "", nil)
		if err != nil {
			return "", err
		}
		resultLook = &created
	}
	return *resultLook.Id, nil
}

var lookMvCmd = &cobra.Command{
	Use:   "mv [LOOK_ID] [TARGET_FOLDER_ID]",
	Short: "Move a look to the given folder",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		lookID := args[0]
		targetFolderID := args[1]

		look, err := c.SDK.Look(lookID, "id,title", nil)
		if err != nil {
			return fmt.Errorf("look with id %s does not exist: %w", lookID, err)
		}

		var matchingTitle *v4.LookWithQuery
		folderLooks, err := c.SDK.FolderLooks(targetFolderID, "", nil)
		if err == nil {
			for _, fl := range folderLooks {
				if look.Title != nil && fl.Title != nil && *fl.Title == *look.Title {
					matchingTitle = &fl
					break
				}
			}
		}

		if matchingTitle != nil {
			if !lookMvForce {
				return fmt.Errorf("look %s already exists in folder %s\nuse --force if you want to overwrite it", *look.Title, targetFolderID)
			}
			if !lookMvPlain {
				fmt.Printf("Deleting existing look %s %s in folder %s\n", *matchingTitle.Id, *matchingTitle.Title, targetFolderID)
			}
			_, err := c.SDK.DeleteLook(*matchingTitle.Id, nil)
			if err != nil {
				return fmt.Errorf("failed to delete existing look %s: %w", *matchingTitle.Id, err)
			}
		}

		_, err = c.SDK.UpdateLook(lookID, v4.WriteLookWithQuery{FolderId: &targetFolderID}, "", nil)
		if err != nil {
			return fmt.Errorf("failed to move look %s to folder %s: %w", lookID, targetFolderID, err)
		}

		if !lookMvPlain {
			fmt.Printf("Moved look %s to folder %s\n", lookID, targetFolderID)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(LookCmd)
	LookCmd.AddCommand(lookCatCmd)
	LookCmd.AddCommand(lookRmCmd)
	LookCmd.AddCommand(lookImportCmd)
	LookCmd.AddCommand(lookMvCmd)

	lookCatCmd.Flags().StringVar(&lookCatFields, "fields", "", "Fields to display")
	lookCatCmd.Flags().StringVar(&lookCatDir, "dir", "", "Directory to store output file")
	lookCatCmd.Flags().BoolVar(&lookCatTrim, "trim", false, "Trim output to minimal set of fields")
	lookCatCmd.Flags().BoolVar(&lookCatPlans, "plans", false, "Include scheduled plans")

	lookImportCmd.Flags().BoolVar(&lookImportForce, "force", false, "Overwrite existing look")
	lookImportCmd.Flags().BoolVar(&lookImportPlain, "plain", false, "Output only look id")

	lookMvCmd.Flags().BoolVar(&lookMvForce, "force", false, "Overwrite a look with the same name in the target folder")
	lookMvCmd.Flags().BoolVar(&lookMvPlain, "plain", false, "Output only look id")
}
