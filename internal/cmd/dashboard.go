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

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	dashboardCatFields          string
	dashboardCatDir             string
	dashboardCatTransform       string
	dashboardCatTrim            bool
	dashboardCatPlans           bool
	dashboardImportForce        bool
	dashboardImportPlain        bool
	dashboardMvForce            bool
	dashboardMvPlain            bool
	dashboardImportLookmlForce  bool
	dashboardImportLookmlUnlink bool
	dashboardImportLookmlSync   bool
	dashboardImportLookmlPlain  bool
)

var DashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Commands pertaining to dashboards",
}

var dashboardCatCmd = &cobra.Command{
	Use:   "cat [DASHBOARD_ID]",
	Short: "output json describing a dashboard",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		dID := args[0]
		dash, err := c.SDK.Dashboard(dID, dashboardCatFields, nil)
		if err != nil {
			return fmt.Errorf("failed to get dashboard %s: %w", dID, err)
		}

		b, _ := json.Marshal(dash)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if dashboardCatPlans {
			plans, _ := c.SDK.ScheduledPlansForDashboard(v4.RequestScheduledPlansForDashboard{DashboardId: dID, AllUsers: ptrBool(true)}, nil)
			m["scheduled_plans"] = plans
		}

		alerts, _ := c.SDK.SearchAlerts(v4.RequestSearchAlerts{GroupBy: ptr("dashboard"), AllOwners: ptrBool(true)}, nil)
		if len(alerts) > 0 && dash.DashboardElements != nil {
			elemAlerts := make(map[string][]v4.Alert)
			for _, a := range alerts {
				if a.DashboardElementId != nil {
					eID := *a.DashboardElementId
					elemAlerts[eID] = append(elemAlerts[eID], a)
				}
			}
			if elements, ok := m["dashboard_elements"].([]interface{}); ok {
				for i, ev := range elements {
					if em, ok := ev.(map[string]interface{}); ok {
						if idVal, ok := em["id"].(float64); ok {
							eID := strconv.FormatInt(int64(idVal), 10)
							if aList, ok := elemAlerts[eID]; ok {
								em["alerts"] = aList
							}
						}
						elements[i] = em
					}
				}
			}
		}
		if elements, ok := m["dashboard_elements"].([]interface{}); ok {
			for i, ev := range elements {
				if em, ok := ev.(map[string]interface{}); ok {
					if mergeResultIDVal, ok := em["merge_result_id"]; ok && mergeResultIDVal != nil {
						mergeResultID := idToStr(mergeResultIDVal)
						if mergeResultID != "" {
							mq, err := c.SDK.MergeQuery(mergeResultID, "", nil)
							if err == nil {
								mqBytes, _ := json.Marshal(mq)
								var mqMap map[string]interface{}
								_ = json.Unmarshal(mqBytes, &mqMap)

								if sourceQueries, ok := mqMap["source_queries"].([]interface{}); ok {
									for j, sqv := range sourceQueries {
										if sq, ok := sqv.(map[string]interface{}); ok {
											if qIDVal, ok := sq["query_id"]; ok && qIDVal != nil {
												qID := idToStr(qIDVal)
												if qID != "" {
													q, err := c.SDK.Query(qID, "", nil)
													if err == nil {
														qBytes, _ := json.Marshal(q)
														var qMap map[string]interface{}
														_ = json.Unmarshal(qBytes, &qMap)
														sq["query"] = qMap
														sourceQueries[j] = sq
													} else {
														fmt.Fprintf(os.Stderr, "failed to get query %s: %v\n", qID, err)
													}
												}
											}
										}
									}
									mqMap["source_queries"] = sourceQueries
								}
								em["merge_result"] = mqMap
								elements[i] = em
							} else {
								fmt.Fprintf(os.Stderr, "failed to get merge query %s: %v\n", mergeResultID, err)
							}
						}
					}
				}
			}
			m["dashboard_elements"] = elements
		}

		if dashboardCatTrim {
			keepDash := map[string]bool{
				"id":                    true,
				"title":                 true,
				"description":           true,
				"background_color":      true,
				"show_title":            true,
				"title_color":           true,
				"show_filters_bar":      true,
				"tile_background_color": true,
				"tile_text_color":       true,
				"text_tile_text_color":  true,
				"crossfilter_enabled":   true,
				"preferred_viewer":      true,
				"slug":                  true,
				"folder_id":             true,
				"user_id":               true,
				"dashboard_elements":    true,
				"dashboard_filters":     true,
				"dashboard_layouts":     true,
				"scheduled_plans":       true,
			}
			for k := range m {
				if !keepDash[k] {
					delete(m, k)
				}
			}

			if elements, ok := m["dashboard_elements"].([]interface{}); ok {
				for i, ev := range elements {
					if em, ok := ev.(map[string]interface{}); ok {
						keepElem := map[string]bool{
							"id":                true,
							"body_text":         true,
							"subtitle_text":     true,
							"title":             true,
							"title_hidden":      true,
							"title_text":        true,
							"type":              true,
							"rich_content_json": true,
							"extension_id":      true,
							"aria_description":  true,
							"look_id":           true,
							"query_id":          true,
							"merge_result_id":   true,
							"result_maker_id":   true,
							"look":              true,
							"query":             true,
							"merge_result":      true,
							"alerts":            true,
						}
						for k := range em {
							if !keepElem[k] {
								delete(em, k)
							}
						}
						elements[i] = em
					}
				}
			}
			if filters, ok := m["dashboard_filters"].([]interface{}); ok {
				for i, fv := range filters {
					if fm, ok := fv.(map[string]interface{}); ok {
						delete(fm, "id")
						delete(fm, "dashboard_id")
						delete(fm, "can")
						delete(fm, "field")
						filters[i] = fm
					}
				}
			}
			if layouts, ok := m["dashboard_layouts"].([]interface{}); ok {
				for i, lv := range layouts {
					if lm, ok := lv.(map[string]interface{}); ok {
						keepLayout := map[string]bool{
							"id":                          true,
							"type":                        true,
							"active":                      true,
							"column_width":                true,
							"width":                       true,
							"label":                       true,
							"description":                 true,
							"order":                       true,
							"lookml_link_id":              true,
							"dashboard_layout_components": true,
						}
						for k := range lm {
							if !keepLayout[k] {
								delete(lm, k)
							}
						}
						if components, ok := lm["dashboard_layout_components"].([]interface{}); ok {
							for j, cv := range components {
								if cm, ok := cv.(map[string]interface{}); ok {
									delete(cm, "can")
									delete(cm, "element_title")
									delete(cm, "element_title_hidden")
									delete(cm, "vis_type")
									components[j] = cm
								}
							}
						}
						layouts[i] = lm
					}
				}
			}
		}

		replacements := make(map[string]string)
		if dashboardCatTransform != "" {
			tb, err := os.ReadFile(dashboardCatTransform)
			if err != nil {
				return fmt.Errorf("failed to read transform file %s: %w", dashboardCatTransform, err)
			}
			var tMap map[string]interface{}
			if err := json.Unmarshal(tb, &tMap); err == nil {
				if repList, ok := tMap["replacements"].([]interface{}); ok {
					for _, rv := range repList {
						if rm, ok := rv.(map[string]interface{}); ok {
							for k, v := range rm {
								if vs, ok := v.(string); ok {
									replacements[k] = vs
								}
							}
						}
					}
				}

				var maxRow int64 = 0
				if layouts, ok := m["dashboard_layouts"].([]interface{}); ok && len(layouts) > 0 {
					if lm, ok := layouts[0].(map[string]interface{}); ok {
						if components, ok := lm["dashboard_layout_components"].([]interface{}); ok {
							for _, cv := range components {
								if cm, ok := cv.(map[string]interface{}); ok {
									r := int64(cm["row"].(float64))
									h := int64(cm["height"].(float64))
									if r+h > maxRow {
										maxRow = r + h + 4
									}
								}
							}
						}
					}
				}

				if elemList, ok := tMap["dashboard_elements"].([]interface{}); ok {
					elements, _ := m["dashboard_elements"].([]interface{})
					layouts, _ := m["dashboard_layouts"].([]interface{})
					var lm map[string]interface{}
					var components []interface{}
					if len(layouts) > 0 {
						lm, _ = layouts[0].(map[string]interface{})
						components, _ = lm["dashboard_layout_components"].([]interface{})
					}

					for i, ev := range elemList {
						if em, ok := ev.(map[string]interface{}); ok {
							fakeID := int64(9900 + i + 1)
							em["id"] = fakeID
							elements = append(elements, em)

							pos, _ := em["position"].(string)
							var row int64 = 0
							h := int64(em["height"].(float64))
							w := int64(em["width"].(float64))
							t, _ := em["type"].(string)

							switch pos {
							case "top":
								for j, cv := range components {
									if cm, ok := cv.(map[string]interface{}); ok {
										cm["row"] = cm["row"].(float64) + float64(h)
										components[j] = cm
									}
								}
							case "bottom":
								row = maxRow
							}

							cMap := map[string]interface{}{
								"id":                   fakeID,
								"dashboard_layout_id":  1,
								"dashboard_element_id": fakeID,
								"row":                  row,
								"column":               0,
								"width":                w,
								"height":               h,
								"deleted":              false,
								"element_title_hidden": false,
								"vis_type":             t,
							}
							components = append(components, cMap)
						}
					}
					m["dashboard_elements"] = elements
					if lm != nil {
						lm["dashboard_layout_components"] = components
						layouts[0] = lm
						m["dashboard_layouts"] = layouts
					}
				}
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		outStr := string(outBytes)
		for k, v := range replacements {
			outStr = strings.ReplaceAll(outStr, k, v)
		}

		if dashboardCatDir != "" {
			title := ""
			if v, ok := m["title"].(string); ok {
				title = v
			}
			fn := fmt.Sprintf("%s/Dashboard_%s_%s.json", dashboardCatDir, dID, strings.ReplaceAll(title, "/", "_"))
			_ = os.WriteFile(fn, []byte(outStr), 0644)
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(outStr)
		}
		return nil
	},
}

var dashboardRmCmd = &cobra.Command{
	Use:   "rm [DASHBOARD_ID]",
	Short: "delete a dashboard",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		dID := args[0]
		_, err = c.SDK.DeleteDashboard(dID, nil)
		if err != nil {
			return fmt.Errorf("failed to delete dashboard %s: %w", dID, err)
		}
		fmt.Printf("Dashboard %s deleted.\n", dID)
		return nil
	},
}

var dashboardImportCmd = &cobra.Command{
	Use:   "import [DASHBOARD_FILE] [FOLDER_ID]",
	Short: "import a dashboard from a file",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		file := args[0]
		folderID := args[1]

		if cfgDebug {
			fmt.Fprintf(os.Stderr, "Reading dashboard file %s\n", file)
		}
		b, err := util.ReadFileOrStdin(file)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}

		if cfgDebug {
			fmt.Fprintf(os.Stderr, "Parsing dashboard JSON\n")
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("invalid json in %s: %w", file, err)
		}

		if _, ok := m["dashboard_elements"]; !ok {
			return fmt.Errorf("file contains no dashboard_elements! Is this a look?")
		}

		if cfgDebug {
			fmt.Fprintf(os.Stderr, "Fetching authenticated user (Me)\n")
		}
		me, err := c.SDK.Me("id", nil)
		if err != nil || me.Id == nil {
			return fmt.Errorf("failed to get me: %v", err)
		}
		myID := *me.Id

		title := ""
		if v, ok := m["title"].(string); ok {
			title = v
		}
		slug := ""
		if v, ok := m["slug"].(string); ok {
			slug = v
		}

		var existingDash *v4.Dashboard
		var slugUsed *v4.Dashboard
		var titleUsed *v4.Dashboard

		if slug != "" {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Searching for existing dashboard by slug '%s'\n", slug)
			}

			dashes, err := c.SDK.SearchDashboards(v4.RequestSearchDashboards{Slug: &slug}, nil)
			if err != nil {
				return fmt.Errorf("failed to search dashboards by slug %s: %w", slug, err)
			}
			if len(dashes) > 0 {
				match := dashes[0]
				slugUsed = &match
				if match.FolderId != nil && *match.FolderId == folderID && (match.Deleted == nil || !*match.Deleted) {
					existingDash = &match
				}
			} else {
				deletedDashes, err := c.SDK.SearchDashboards(v4.RequestSearchDashboards{Slug: &slug, Deleted: ptr("true")}, nil)
				if err != nil {
					return fmt.Errorf("failed to search deleted dashboards by slug %s: %w", slug, err)
				}
				if len(deletedDashes) > 0 {
					match := deletedDashes[0]
					slugUsed = &match
				}
			}
		}

		if title != "" {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Searching for existing dashboard by title '%s'\n", title)
			}

			dashes, err := c.SDK.SearchDashboards(v4.RequestSearchDashboards{Title: &title, FolderId: &folderID}, nil)
			if err != nil {
				return fmt.Errorf("failed to search dashboards by title %q in folder %s: %w", title, folderID, err)
			}
			for i := range dashes {
				if dashes[i].Deleted == nil || !*dashes[i].Deleted {
					titleUsed = &dashes[i]
					break
				}
			}
		}

		if existingDash != nil && titleUsed != nil && existingDash.Id != nil && titleUsed.Id != nil && *existingDash.Id != *titleUsed.Id {
			return fmt.Errorf("dashboard '%s' already exists in folder %s. Delete it before trying to update another dashboard to have that title", title, folderID)
		}

		if existingDash == nil {
			existingDash = titleUsed
		}

		slugConflict := slugUsed != nil && (existingDash == nil || existingDash.Id == nil || slugUsed.Id == nil || *existingDash.Id != *slugUsed.Id)
		if slugConflict && !dashboardImportPlain {
			if slugUsed.Deleted != nil && *slugUsed.Deleted {
				fmt.Printf("Warning: Slug '%s' is already in use on a deleted dashboard. Generating a new slug for this import.\n", slug)
			} else {
				conflictingFolder := ""
				if slugUsed.FolderId != nil {
					conflictingFolder = *slugUsed.FolderId
				}
				fmt.Printf("Warning: Slug '%s' is already in use in folder %s. Generating a new slug for this import.\n", slug, conflictingFolder)
			}
		}

		db, _ := json.Marshal(m)
		var wd v4.WriteDashboard
		_ = json.Unmarshal(db, &wd)
		wd.Folder = nil

		var resultDash *v4.Dashboard

		if existingDash != nil {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Updating existing dashboard %s in folder %s\n", *existingDash.Id, folderID)
			}

			if !dashboardImportForce {
				return fmt.Errorf("dashboard '%s' already exists in folder %s. Use --force to overwrite", title, folderID)
			}
			wd.FolderId = nil
			wd.UserId = nil
			if slugUsed != nil {
				wd.Slug = nil
			}
			if existingDash.Deleted != nil && *existingDash.Deleted {
				wd.Deleted = ptrBool(false)
			} else {
				wd.Deleted = nil
			}

			edID := *existingDash.Id
			updated, err := c.SDK.UpdateDashboard(edID, wd, nil)
			if err != nil {
				if strings.Contains(err.Error(), "status=409") && wd.Slug != nil {
					if !dashboardImportPlain {
						fmt.Printf("Warning: Conflict occurred on update. Retrying without slug.\n")
					}
					wd.Slug = nil
					updated, err = c.SDK.UpdateDashboard(edID, wd, nil)
				}
				if err != nil {
					return fmt.Errorf("failed to update dashboard %s: %w", edID, err)
				}
			}
			resultDash = &updated

			if updated.DashboardFilters != nil {
				for _, f := range *updated.DashboardFilters {
					if f.Id != nil {
						_, _ = c.SDK.DeleteDashboardFilter(*f.Id, nil)
					}
				}
			}
			if updated.DashboardElements != nil {
				for _, e := range *updated.DashboardElements {
					if e.Id != nil {
						_, _ = c.SDK.DeleteDashboardElement(*e.Id, nil)
					}
				}
			}
			if updated.DashboardLayouts != nil {
				for _, l := range *updated.DashboardLayouts {
					if l.Id != nil && (l.Active == nil || !*l.Active) {
						_, _ = c.SDK.DeleteDashboardLayout(*l.Id, nil)
					}
				}
			}
		} else {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Creating new dashboard in folder %s\n", folderID)
			}

			wd.FolderId = &folderID
			wd.UserId = &myID
			wd.Deleted = nil
			if slugUsed != nil {
				wd.Slug = nil
			}

			created, err := c.SDK.CreateDashboard(wd, nil)
			if err != nil {
				if strings.Contains(err.Error(), "status=409") && wd.Slug != nil {
					if !dashboardImportPlain {
						fmt.Printf("Warning: Conflict occurred on create. Retrying without slug.\n")
					}
					wd.Slug = nil
					created, err = c.SDK.CreateDashboard(wd, nil)
				}
				if err != nil {
					return fmt.Errorf("failed to create dashboard: %w", err)
				}
			}
			resultDash = &created
		}

		resID := *resultDash.Id

		if filtersVal, ok := m["dashboard_filters"].([]interface{}); ok && len(filtersVal) > 0 {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Importing %d dashboard filters\n", len(filtersVal))
			}
			for _, fv := range filtersVal {
				fb, _ := json.Marshal(fv)
				var wdf v4.WriteCreateDashboardFilter
				_ = json.Unmarshal(fb, &wdf)
				wdf.DashboardId = resID
				_, _ = c.SDK.CreateDashboardFilter(wdf, "", nil)
			}
		}

		var activeLayoutID string
		if layoutsVal, ok := m["dashboard_layouts"].([]interface{}); ok && len(layoutsVal) > 0 {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Importing %d dashboard layouts\n", len(layoutsVal))
			}
			for _, lv := range layoutsVal {
				lb, _ := json.Marshal(lv)
				var wdl v4.WriteDashboardLayout
				_ = json.Unmarshal(lb, &wdl)
				wdl.DashboardId = &resID

				var layoutObj *v4.DashboardLayout
				if resultDash.DashboardLayouts != nil && len(*resultDash.DashboardLayouts) > 0 && wdl.Active != nil && *wdl.Active {
					for _, l := range *resultDash.DashboardLayouts {
						if l.Active != nil && *l.Active && l.Id != nil {
							updated, err := c.SDK.UpdateDashboardLayout(*l.Id, wdl, "", nil)
							if err == nil {
								layoutObj = &updated
							}
							break
						}
					}
				}
				if layoutObj == nil {
					created, err := c.SDK.CreateDashboardLayout(wdl, "", nil)
					if err == nil {
						layoutObj = &created
					}
				}

				if layoutObj != nil && layoutObj.Id != nil {
					activeLayoutID = *layoutObj.Id
					if lm, ok := lv.(map[string]interface{}); ok {
						if componentsVal, ok := lm["dashboard_layout_components"].([]interface{}); ok && len(componentsVal) > 0 {
							if cfgDebug {
								fmt.Fprintf(os.Stderr, "Importing %d dashboard layout components\n", len(componentsVal))
							}
							matchedElems := make(map[int]bool)
							for cIdx, cv := range componentsVal {
								cb, _ := json.Marshal(cv)
								var wdlc v4.WriteDashboardLayoutComponent
								_ = json.Unmarshal(cb, &wdlc)
								wdlc.DashboardLayoutId = &activeLayoutID

								var elemMap map[string]interface{}
								if cm, ok := cv.(map[string]interface{}); ok {
									matchIDStr := idToStr(cm["dashboard_element_id"])
									compTitle, _ := cm["element_title"].(string)

									if elementsVal, ok := m["dashboard_elements"].([]interface{}); ok {
										if matchIDStr != "" {
											for eIdx, ev := range elementsVal {
												if matchedElems[eIdx] {
													continue
												}
												if em, ok := ev.(map[string]interface{}); ok {
													if idToStr(em["id"]) == matchIDStr {
														elemMap = em
														matchedElems[eIdx] = true
														break
													}
												}
											}
										}
										if elemMap == nil && compTitle != "" {
											for eIdx, ev := range elementsVal {
												if matchedElems[eIdx] {
													continue
												}
												if em, ok := ev.(map[string]interface{}); ok {
													elemTitle, _ := em["title"].(string)
													if elemTitle != "" && elemTitle == compTitle {
														elemMap = em
														matchedElems[eIdx] = true
														break
													}
												}
											}
										}
										if elemMap == nil && cIdx < len(elementsVal) && !matchedElems[cIdx] {
											if em, ok := elementsVal[cIdx].(map[string]interface{}); ok {
												elemMap = em
												matchedElems[cIdx] = true
											}
										}
									}
								}

								if elemMap != nil {
									if cfgDebug {
										fmt.Fprintf(os.Stderr, "Creating dashboard element '%v'\n", elemMap["title"])
									}
									eb, _ := json.Marshal(elemMap)
									var wde v4.WriteDashboardElement
									_ = json.Unmarshal(eb, &wde)
									wde.DashboardId = &resID

									wde.ResultMakerId = nil
									wde.LookId = nil
									wde.QueryId = nil
									wde.MergeResultId = nil

									if lookVal, ok := elemMap["look"].(map[string]interface{}); ok {
										if qMap, ok := lookVal["query"].(map[string]interface{}); ok {
											delete(qMap, "client_id")
										}
										lID, err := UpsertLookHelper(c, folderID, myID, lookVal, dashboardImportForce)
										if err == nil && lID != "" {
											wde.LookId = &lID
											wde.ResultMaker = nil
										} else if err != nil {
											fmt.Fprintf(os.Stderr, "UpsertLookHelper failed: %v\n", err)
										}
									} else if qVal, ok := elemMap["query"].(map[string]interface{}); ok {
										qb, _ := json.Marshal(qVal)
										var wq v4.WriteQuery
										_ = json.Unmarshal(qb, &wq)
										wq.ClientId = nil
										cq, err := c.SDK.CreateQuery(wq, "", nil)
										if err == nil && cq.Id != nil {
											wde.QueryId = cq.Id
											if wde.ResultMaker != nil {
												wde.ResultMaker.SqlQueryId = nil
												wde.ResultMaker.Query = nil
											}
										} else if err != nil {
											fmt.Fprintf(os.Stderr, "CreateQuery failed: %v\n", err)
										}
									} else if mVal, ok := elemMap["merge_result"].(map[string]interface{}); ok {
										if sourceQueries, ok := mVal["source_queries"].([]interface{}); ok {
											for sqIdx, sqv := range sourceQueries {
												if sq, ok := sqv.(map[string]interface{}); ok {
													if qVal, ok := sq["query"].(map[string]interface{}); ok {
														qb, _ := json.Marshal(qVal)
														var wq v4.WriteQuery
														_ = json.Unmarshal(qb, &wq)
														wq.ClientId = nil
														cq, err := c.SDK.CreateQuery(wq, "", nil)
														if err == nil && cq.Id != nil {
															sq["query_id"] = cq.Id
															delete(sq, "query")
															sourceQueries[sqIdx] = sq
														} else {
															fmt.Fprintf(os.Stderr, "Failed to recreate source query: %v\n", err)
															delete(sq, "query_id")
															sourceQueries[sqIdx] = sq
														}
													}
												}
											}
											mVal["source_queries"] = sourceQueries
										}
										mb, _ := json.Marshal(mVal)
										var wmq v4.WriteMergeQuery
										_ = json.Unmarshal(mb, &wmq)
										cmq, err := c.SDK.CreateMergeQuery(wmq, "", nil)
										if err == nil && cmq.Id != nil {
											wde.MergeResultId = cmq.Id
											wde.ResultMaker = nil
										} else if err != nil {
											fmt.Fprintf(os.Stderr, "CreateMergeQuery failed: %v\n", err)
										}
									}

									reqElem := v4.RequestCreateDashboardElement{Body: wde}
									createdElem, err := c.SDK.CreateDashboardElement(reqElem, nil)
									if err == nil && createdElem.Id != nil {
										elemIDStr := *createdElem.Id
										wdlc.DashboardElementId = &elemIDStr

										if alertsVal, ok := elemMap["alerts"].([]interface{}); ok {
											for _, av := range alertsVal {
												ab, _ := json.Marshal(av)
												var wa v4.WriteAlert
												_ = json.Unmarshal(ab, &wa)
												wa.DashboardElementId = &elemIDStr
												wa.OwnerId = myID
												_, _ = c.SDK.CreateAlert(wa, nil)
											}
										}

										layoutComponents, _ := c.SDK.DashboardLayoutDashboardLayoutComponents(activeLayoutID, "", nil)
										for _, lc := range layoutComponents {
											if lc.DashboardElementId != nil && *lc.DashboardElementId == elemIDStr && lc.Id != nil {
												_, _ = c.SDK.UpdateDashboardLayoutComponent(*lc.Id, wdlc, "", nil)
												break
											}
										}
									} else if err != nil {
										fmt.Fprintf(os.Stderr, "CreateDashboardElement failed: %v\n", err)
									}
								}
							}
						}
					}
				}
			}
		}

		if plansVal, ok := m["scheduled_plans"].([]interface{}); ok && len(plansVal) > 0 {
			if cfgDebug {
				fmt.Fprintf(os.Stderr, "Importing %d scheduled plans\n", len(plansVal))
			}
			existingPlans, _ := c.SDK.ScheduledPlansForDashboard(v4.RequestScheduledPlansForDashboard{DashboardId: resID, AllUsers: ptrBool(true)}, nil)

			for _, pv := range plansVal {
				pb, _ := json.Marshal(pv)
				var wsp v4.WriteScheduledPlan
				_ = json.Unmarshal(pb, &wsp)
				wsp.DashboardId = &resID
				wsp.UserId = &myID

				var matchedPlan *v4.ScheduledPlan
				pName := ""
				if wsp.Name != nil {
					pName = *wsp.Name
				}
				for _, ep := range existingPlans {
					epName := ""
					if ep.Name != nil {
						epName = *ep.Name
					}
					if epName == pName && ep.UserId != nil && *ep.UserId == myID {
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

		if dashboardImportPlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Imported dashboard %s\n", resID)
		}
		return nil
	},
}

var dashboardMvCmd = &cobra.Command{
	Use:   "mv [DASHBOARD_ID] [TARGET_FOLDER_ID]",
	Short: "Move a dashboard to the given folder",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		dashboardID := args[0]
		targetFolderID := args[1]

		dash, err := c.SDK.Dashboard(dashboardID, "id,title", nil)
		if err != nil {
			return fmt.Errorf("dashboard with id %s does not exist: %w", dashboardID, err)
		}

		matches, _ := c.SDK.SearchDashboards(v4.RequestSearchDashboards{Title: dash.Title, FolderId: &targetFolderID}, nil)
		var matchingTitle *v4.Dashboard
		if len(matches) > 0 {
			matchingTitle = &matches[0]
		}

		if matchingTitle != nil {
			if !dashboardMvForce {
				return fmt.Errorf("dashboard %s already exists in folder %s\nuse --force if you want to overwrite it", *dash.Title, targetFolderID)
			}
			if !dashboardMvPlain {
				fmt.Printf("Deleting existing dashboard %s %s in folder %s\n", *matchingTitle.Id, *matchingTitle.Title, targetFolderID)
			}
			_, err := c.SDK.UpdateDashboard(*matchingTitle.Id, v4.WriteDashboard{Deleted: ptrBool(true)}, nil)
			if err != nil {
				return fmt.Errorf("failed to delete existing dashboard %s: %w", *matchingTitle.Id, err)
			}
		}

		_, err = c.SDK.UpdateDashboard(dashboardID, v4.WriteDashboard{FolderId: &targetFolderID}, nil)
		if err != nil {
			return fmt.Errorf("failed to move dashboard %s to folder %s: %w", dashboardID, targetFolderID, err)
		}

		if !dashboardMvPlain {
			fmt.Printf("Moved dashboard %s to folder %s\n", dashboardID, targetFolderID)
		}
		return nil
	},
}

var dashboardImportLookmlCmd = &cobra.Command{
	Use:   "lookml [DASHBOARD_ID] [TARGET_FOLDER_ID]",
	Short: "Create a UDD from a lookml dashboard in the given folder",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		dashboardID := args[0]
		targetFolderID := args[1]

		dash, err := c.SDK.Dashboard(dashboardID, "id,title", nil)
		if err != nil {
			return fmt.Errorf("dashboard with id %s does not exist: %w", dashboardID, err)
		}

		matches, _ := c.SDK.SearchDashboards(v4.RequestSearchDashboards{Title: dash.Title, FolderId: &targetFolderID}, nil)
		var matchingTitle *v4.Dashboard
		if len(matches) > 0 {
			matchingTitle = &matches[0]
		}

		if matchingTitle != nil && (matchingTitle.LookmlLinkId == nil || *matchingTitle.LookmlLinkId != dashboardID) {
			if !dashboardImportLookmlForce {
				return fmt.Errorf("dashboard %s already exists in folder %s\nuse --force if you want to overwrite it", *dash.Title, targetFolderID)
			}
			if !dashboardImportLookmlPlain {
				fmt.Printf("Deleting existing dashboard %s %s in folder %s\n", *matchingTitle.Id, *matchingTitle.Title, targetFolderID)
			}
			_, err := c.SDK.UpdateDashboard(*matchingTitle.Id, v4.WriteDashboard{Deleted: ptrBool(true)}, nil)
			if err != nil {
				return fmt.Errorf("failed to delete existing dashboard %s: %w", *matchingTitle.Id, err)
			}
		}

		if matchingTitle != nil && matchingTitle.LookmlLinkId != nil && *matchingTitle.LookmlLinkId == dashboardID {
			if !dashboardImportLookmlSync {
				return fmt.Errorf("linked dashboard %s already exists in folder %s\nuse --sync if you want to synchronize it", *dash.Title, targetFolderID)
			}
			if !dashboardImportLookmlPlain {
				fmt.Printf("Syncing existing dashboard %s %s in folder %s\n", *matchingTitle.Id, *matchingTitle.Title, targetFolderID)
			}
			ids, err := c.SDK.SyncLookmlDashboard(v4.RequestSyncLookmlDashboard{LookmlDashboardId: dashboardID}, nil)
			if err != nil {
				return fmt.Errorf("failed to sync dashboard %s: %w", dashboardID, err)
			}
			if dashboardImportLookmlPlain {
				for _, id := range ids {
					fmt.Println(id)
				}
			} else {
				fmt.Printf("Synced dashboards %v\n", ids)
			}
			return nil
		}

		newDash, err := c.SDK.ImportLookmlDashboard(dashboardID, targetFolderID, v4.WriteDashboard{}, false, nil)
		if err != nil {
			return fmt.Errorf("failed to import lookml dashboard %s: %w", dashboardID, err)
		}

		if dashboardImportLookmlUnlink {
			_, err := c.SDK.UpdateDashboard(*newDash.Id, v4.WriteDashboard{LookmlLinkId: ptr("")}, nil)
			if err != nil {
				return fmt.Errorf("failed to unlink dashboard %s: %w", *newDash.Id, err)
			}
		}

		if dashboardImportLookmlPlain {
			fmt.Println(*newDash.Id)
		} else {
			fmt.Printf("Created user defined dashboard %s in folder %s\n", *newDash.Id, targetFolderID)
		}
		return nil
	},
}

var dashboardSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync dashboards",
}

var dashboardSyncLookmlCmd = &cobra.Command{
	Use:   "lookml [DASHBOARD_ID]",
	Short: "Sync any UDD from a lookml dashboard",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		dashboardID := args[0]

		_, err = c.SDK.Dashboard(dashboardID, "id,title", nil)
		if err != nil {
			return fmt.Errorf("dashboard with id %s does not exist: %w", dashboardID, err)
		}

		ids, err := c.SDK.SyncLookmlDashboard(v4.RequestSyncLookmlDashboard{LookmlDashboardId: dashboardID}, nil)
		if err != nil {
			return fmt.Errorf("failed to sync dashboard %s: %w", dashboardID, err)
		}
		fmt.Printf("Synced dashboards %v\n", ids)
		return nil
	},
}

func idToStr(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func init() {
	RootCmd.AddCommand(DashboardCmd)
	DashboardCmd.AddCommand(dashboardCatCmd)
	DashboardCmd.AddCommand(dashboardRmCmd)
	DashboardCmd.AddCommand(dashboardImportCmd)
	DashboardCmd.AddCommand(dashboardMvCmd)

	dashboardImportCmd.AddCommand(dashboardImportLookmlCmd)

	DashboardCmd.AddCommand(dashboardSyncCmd)
	dashboardSyncCmd.AddCommand(dashboardSyncLookmlCmd)

	dashboardCatCmd.Flags().StringVar(&dashboardCatDir, "dir", "", "Directory to store output file")
	dashboardCatCmd.Flags().StringVar(&dashboardCatFields, "fields", "", "Fields to display")
	dashboardCatCmd.Flags().StringVar(&dashboardCatTransform, "transform", "", "Transform file to apply")
	dashboardCatCmd.Flags().BoolVar(&dashboardCatTrim, "trim", false, "Trim output to minimal set of fields")
	dashboardCatCmd.Flags().BoolVar(&dashboardCatPlans, "plans", false, "Include scheduled plans")

	dashboardImportCmd.Flags().BoolVar(&dashboardImportForce, "force", false, "Overwrite existing dashboard")
	dashboardImportCmd.Flags().BoolVar(&dashboardImportPlain, "plain", false, "Output only dashboard id")

	dashboardMvCmd.Flags().BoolVar(&dashboardMvForce, "force", false, "Overwrite a dashboard with the same name in the target folder")
	dashboardMvCmd.Flags().BoolVar(&dashboardMvPlain, "plain", false, "Output only dashboard id")

	dashboardImportLookmlCmd.Flags().BoolVar(&dashboardImportLookmlForce, "force", false, "Overwrite a dashboard with the same name in the target folder")
	dashboardImportLookmlCmd.Flags().BoolVar(&dashboardImportLookmlUnlink, "unlink", false, "Unlink the new user defined dashboard from the LookML dashboard")
	dashboardImportLookmlCmd.Flags().BoolVar(&dashboardImportLookmlSync, "sync", false, "If linked dashboard already exists, sync it with LookML dashboard")
	dashboardImportLookmlCmd.Flags().BoolVar(&dashboardImportLookmlPlain, "plain", false, "Provide minimal response information")
}
