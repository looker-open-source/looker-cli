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
	"os"
	"strings"
	"testing"

	"github.com/looker-open-source/looker-cli/internal/util"
	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

type statefulMockDoer struct {
	t          *testing.T
	dashboards map[string]v4.Dashboard
}

func (m *statefulMockDoer) Do(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
	if strings.HasPrefix(path, "/user") {
		me := v4.User{
			Id: ptr("1234"),
		}
		b, _ := json.Marshal(me)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/dashboards/search" {
		var slug, title, folderID string
		if s, ok := reqPars["slug"].(*string); ok && s != nil {
			slug = *s
		}
		if t, ok := reqPars["title"].(*string); ok && t != nil {
			title = *t
		}
		if f, ok := reqPars["folder_id"].(*string); ok && f != nil {
			folderID = *f
		}

		var results []v4.Dashboard
		for _, d := range m.dashboards {
			match := true
			if slug != "" && (d.Slug == nil || *d.Slug != slug) {
				match = false
			}
			if title != "" && (d.Title == nil || *d.Title != title) {
				match = false
			}
			if folderID != "" && (d.FolderId == nil || *d.FolderId != folderID) {
				match = false
			}
			if match {
				results = append(results, d)
			}
		}
		b, _ := json.Marshal(results)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/dashboards" {
		bodyBytes, _ := json.Marshal(body)
		var wd v4.WriteDashboard
		_ = json.Unmarshal(bodyBytes, &wd)

		if wd.Slug != nil && *wd.Slug == "conflicting_hidden_slug" {
			return fmt.Errorf("response error. status=409 Conflict. error={\"message\":\"The resource already exists.\"}")
		}

		if wd.Slug != nil && *wd.Slug != "" {
			for _, d := range m.dashboards {
				if d.Slug != nil && *d.Slug == *wd.Slug {
					return fmt.Errorf("response error. status=409 Conflict. error={\"message\":\"The resource already exists.\"}")
				}
			}
		}

		id := fmt.Sprintf("new_dash_%d", len(m.dashboards)+1)
		dash := v4.Dashboard{
			Id:       &id,
			Title:    wd.Title,
			Slug:     wd.Slug,
			FolderId: wd.FolderId,
		}
		if m.dashboards == nil {
			m.dashboards = make(map[string]v4.Dashboard)
		}
		m.dashboards[id] = dash
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && strings.HasPrefix(path, "/dashboards/") {
		id := strings.TrimPrefix(path, "/dashboards/")
		bodyBytes, _ := json.Marshal(body)
		var wd v4.WriteDashboard
		_ = json.Unmarshal(bodyBytes, &wd)

		if d, ok := m.dashboards[id]; ok {
			if wd.Title != nil {
				d.Title = wd.Title
			}
			if wd.Slug != nil {
				d.Slug = wd.Slug
			}
			m.dashboards[id] = d
			b, _ := json.Marshal(d)
			return json.Unmarshal(b, result)
		}
		return fmt.Errorf("dashboard %s not found", id)
	}
	if method == "POST" && path == "/dashboard_layouts" {
		layout := v4.DashboardLayout{Id: ptr("new_layout_1")}
		b, _ := json.Marshal(layout)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/dashboard_elements" {
		elem := v4.DashboardElement{Id: ptr("new_elem_1")}
		b, _ := json.Marshal(elem)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && strings.HasSuffix(path, "/dashboard_layout_components") {
		comps := []v4.DashboardLayoutComponent{}
		b, _ := json.Marshal(comps)
		return json.Unmarshal(b, result)
	}
	return nil
}

type mockDoer struct {
	t               *testing.T
	folder801Looks  []v4.LookWithQuery
	elementsCreated int
}

func (m *mockDoer) Do(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
	if strings.HasPrefix(path, "/user") {
		me := v4.User{
			Id:               ptr("1234"),
			Email:            ptr("jsmith@mycompany.com"),
			FirstName:        ptr("John"),
			LastName:         ptr("Smith"),
			HomeFolderId:     ptr("709"),
			PersonalFolderId: ptr("1132"),
		}
		b, _ := json.Marshal(me)
		return json.Unmarshal(b, result)
	}
	if strings.HasPrefix(path, "/folders/709/children") {
		ch := []v4.Folder{
			{Id: ptr("801"), Name: "SubShared", ParentId: ptr("709")},
		}
		b, _ := json.Marshal(ch)
		return json.Unmarshal(b, result)
	}
	if strings.HasPrefix(path, "/folders/709") {
		f := v4.Folder{
			Id:   ptr("709"),
			Name: "Shared",
			Looks: &[]v4.LookWithDashboards{
				{Id: ptr("857"), Title: ptr("Daily Profit")},
			},
			Dashboards: &[]v4.DashboardBase{
				{Id: ptr("192"), Title: ptr("Daily Profit Dashboard")},
			},
		}
		b, _ := json.Marshal(f)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/connections/my_conn" {
		conn := v4.DBConnection{
			Name:        ptr("my_conn"),
			DialectName: ptr("mysql"),
			Host:        ptr("localhost"),
		}
		b, _ := json.Marshal(conn)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/connections/my_new_conn" {
		return fmt.Errorf("not found")
	}
	if method == "POST" && path == "/connections" {
		conn := v4.DBConnection{
			Name:        ptr("my_new_conn"),
			DialectName: ptr("postgres"),
		}
		b, _ := json.Marshal(conn)
		return json.Unmarshal(b, result)
	}
	if method == "DELETE" && path == "/connections/my_conn" {
		b, _ := json.Marshal("")
		return json.Unmarshal(b, result)
	}
	if method == "PUT" && path == "/connections/my_conn/test" {
		res := []v4.DBConnectionTestResult{
			{Name: ptr("connect"), Status: ptr("success"), Message: ptr("connected successfully")},
		}
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/dashboards/192" {
		dash := v4.Dashboard{Id: ptr("192"), Title: ptr("Daily Profit Dashboard")}
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/dashboards/3188" {
		dash := v4.Dashboard{
			Id:    ptr("3188"),
			Title: ptr("Merge Query Test Dashboard"),
			DashboardElements: &[]v4.DashboardElement{
				{
					Id:            ptr("elem_1"),
					MergeResultId: ptr("merge_123"),
				},
			},
		}
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/merge_queries/merge_123" {
		mq := v4.MergeQuery{
			Id: ptr("merge_123"),
			SourceQueries: &[]v4.MergeQuerySourceQuery{
				{
					QueryId: ptr("query_456"),
					Name:    ptr("source_query_1"),
				},
			},
		}
		b, _ := json.Marshal(mq)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/queries/query_456" {
		q := v4.Query{
			Id:     ptr("query_456"),
			Model:  "test_model",
			View:   "test_view",
			Fields: &[]string{"field1", "field2"},
		}
		b, _ := json.Marshal(q)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/dashboards/lookml_dash_1" {
		dash := v4.Dashboard{Id: ptr("lookml_dash_1"), Title: ptr("LookML Dash 1")}
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/dashboards/search" {
		var dashes []v4.Dashboard
		b, _ := json.Marshal(dashes)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && path == "/dashboards/192" {
		dash := v4.Dashboard{Id: ptr("192"), Title: ptr("Daily Profit Dashboard"), FolderId: ptr("801")}
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/dashboards/lookml_dash_1/import/801" {
		dash := v4.Dashboard{Id: ptr("new_udd_1"), Title: ptr("LookML Dash 1"), FolderId: ptr("801")}
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/dashboards" {
		dash := v4.Dashboard{Id: ptr("new_dash_1")}
		b, _ := json.Marshal(dash)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/dashboard_layouts" {
		layout := v4.DashboardLayout{Id: ptr("new_layout_1")}
		b, _ := json.Marshal(layout)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/dashboard_elements" {
		m.elementsCreated++
		elem := v4.DashboardElement{Id: ptr("new_elem_1")}
		b, _ := json.Marshal(elem)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/queries" {
		q := v4.Query{
			Id: ptr("new_query_456"),
		}
		b, _ := json.Marshal(q)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/merge_queries" {
		bodyBytes, _ := json.Marshal(body)
		var wmq v4.WriteMergeQuery
		_ = json.Unmarshal(bodyBytes, &wmq)
		if wmq.SourceQueries != nil && len(*wmq.SourceQueries) > 0 {
			sq := (*wmq.SourceQueries)[0]
			if sq.QueryId != nil && *sq.QueryId != "new_query_456" {
				m.t.Errorf("Expected query_id to be 'new_query_456', got '%s'", *sq.QueryId)
			}
		} else {
			m.t.Errorf("Expected source_queries to be present in WriteMergeQuery")
		}
		mq := v4.MergeQuery{
			Id: ptr("new_merge_123"),
		}
		b, _ := json.Marshal(mq)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/dashboard_layouts/new_layout_1/dashboard_layout_components" {
		comps := []v4.DashboardLayoutComponent{
			{Id: ptr("comp_1"), DashboardElementId: ptr("new_elem_1")},
		}
		b, _ := json.Marshal(comps)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && strings.HasPrefix(path, "/dashboard_layout_components/") {
		comp := v4.DashboardLayoutComponent{Id: ptr("comp_1")}
		b, _ := json.Marshal(comp)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && path == "/dashboards/lookml_dash_1/sync" {
		res := []int64{101, 102}
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/looks/857" {
		look := v4.Look{Id: ptr("857"), Title: ptr("Daily Profit")}
		b, _ := json.Marshal(look)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/folders/801/looks" {
		b, _ := json.Marshal(m.folder801Looks)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && path == "/looks/857" {
		look := v4.Look{Id: ptr("857"), Title: ptr("Daily Profit"), FolderId: ptr("801")}
		b, _ := json.Marshal(look)
		return json.Unmarshal(b, result)
	}
	if method == "DELETE" && path == "/looks/999" {
		b, _ := json.Marshal("")
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/lookml_models" {
		models := []v4.LookmlModel{
			{Name: ptr("my_model"), Label: ptr("My Model"), ProjectName: ptr("my_project")},
		}
		b, _ := json.Marshal(models)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/lookml_models/my_model" {
		model := v4.LookmlModel{Name: ptr("my_model"), Label: ptr("My Model"), ProjectName: ptr("my_project")}
		b, _ := json.Marshal(model)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/lookml_models" {
		model := v4.LookmlModel{Name: ptr("my_model"), Label: ptr("My Model"), ProjectName: ptr("my_project")}
		b, _ := json.Marshal(model)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && path == "/lookml_models/my_model" {
		model := v4.LookmlModel{Name: ptr("my_model"), Label: ptr("My Model"), ProjectName: ptr("my_project")}
		b, _ := json.Marshal(model)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/model_sets" {
		sets := []v4.ModelSet{
			{Id: ptr("5"), Name: ptr("my_model_set"), Models: &[]string{"my_model"}},
		}
		b, _ := json.Marshal(sets)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/model_sets/5" {
		set := v4.ModelSet{Id: ptr("5"), Name: ptr("my_model_set"), Models: &[]string{"my_model"}}
		b, _ := json.Marshal(set)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/model_sets" {
		set := v4.ModelSet{Id: ptr("5"), Name: ptr("my_model_set"), Models: &[]string{"my_model"}}
		b, _ := json.Marshal(set)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && path == "/model_sets/5" {
		set := v4.ModelSet{Id: ptr("5"), Name: ptr("my_model_set"), Models: &[]string{"my_model"}}
		b, _ := json.Marshal(set)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/permissions" {
		perms := []v4.Permission{
			{Permission: ptr("access_data"), Parent: nil},
			{Permission: ptr("see_looks"), Parent: ptr("access_data")},
			{Permission: ptr("see_user_dashboards"), Parent: ptr("see_looks")},
			{Permission: ptr("admin"), Parent: nil},
		}
		b, _ := json.Marshal(perms)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/permission_sets" {
		sets := []v4.PermissionSet{
			{Id: ptr("3"), Name: ptr("my_permission_set"), Permissions: &[]string{"access_data"}},
		}
		b, _ := json.Marshal(sets)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/roles/search" {
		m.t.Logf("mockDoer GET /roles/search called with reqPars: %v", reqPars)
		var id string
		if idPtr, ok := reqPars["id"].(*string); ok && idPtr != nil {
			id = *idPtr
		}
		if id == "1" {
			role := v4.Role{
				Id:   ptr("1"),
				Name: ptr("my_role"),
				PermissionSet: &v4.PermissionSet{
					Id:          ptr("2"),
					Name:        ptr("my_perm_set"),
					Permissions: &[]string{"access_data", "see_looks"},
				},
				ModelSet: &v4.ModelSet{
					Id:     ptr("3"),
					Name:   ptr("my_model_set"),
					Models: &[]string{"my_model"},
				},
			}

			var fields string
			if fieldsPtr, ok := reqPars["fields"].(*string); ok && fieldsPtr != nil {
				fields = *fieldsPtr
			}

			if fields != "" {
				filteredRole := v4.Role{}
				if strings.Contains(fields, "name") {
					filteredRole.Name = role.Name
				}
				if strings.Contains(fields, "id") {
					filteredRole.Id = role.Id
				}
				if strings.Contains(fields, "permission_set") {
					filteredRole.PermissionSet = role.PermissionSet
				}
				if strings.Contains(fields, "model_set") {
					filteredRole.ModelSet = role.ModelSet
				}
				roles := []v4.Role{filteredRole}
				b, _ := json.Marshal(roles)
				return json.Unmarshal(b, result)
			}

			roles := []v4.Role{role}
			b, _ := json.Marshal(roles)
			return json.Unmarshal(b, result)
		}
		roles := []v4.Role{}
		b, _ := json.Marshal(roles)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/permission_sets/3" {
		m.t.Logf("mockDoer GET /permission_sets/3 called with reqPars: %v", reqPars)
		if fields, ok := reqPars["fields"].(string); ok && fields != "" {
			set := v4.PermissionSet{}
			if strings.Contains(fields, "name") {
				set.Name = ptr("my_permission_set")
			}
			if strings.Contains(fields, "id") {
				set.Id = ptr("3")
			}
			b, _ := json.Marshal(set)
			return json.Unmarshal(b, result)
		}
		set := v4.PermissionSet{Id: ptr("3"), Name: ptr("my_permission_set"), Permissions: &[]string{"access_data"}}
		b, _ := json.Marshal(set)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && path == "/permission_sets" {
		set := v4.PermissionSet{Id: ptr("3"), Name: ptr("my_permission_set"), Permissions: &[]string{"access_data"}}
		b, _ := json.Marshal(set)
		return json.Unmarshal(b, result)
	}
	if method == "PATCH" && path == "/permission_sets/3" {
		set := v4.PermissionSet{Id: ptr("3"), Name: ptr("my_permission_set"), Permissions: &[]string{"access_data"}}
		b, _ := json.Marshal(set)
		return json.Unmarshal(b, result)
	}
	if method == "DELETE" && path == "/permission_sets/3" {
		b, _ := json.Marshal("")
		return json.Unmarshal(b, result)
	}
	if method == "DELETE" && path == "/model_sets/5" {
		b, _ := json.Marshal("")
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/folders" {
		folders := []v4.FolderBase{
			{Id: ptr("1"), Name: "Shared Root", IsSharedRoot: ptrBool(true)},
			{Id: ptr("2"), Name: "Users Root", IsUsersRoot: ptrBool(true)},
			{Id: ptr("3"), Name: "Personal Space", IsPersonal: ptrBool(true)},
		}
		b, _ := json.Marshal(folders)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/roles" {
		roles := []v4.Role{
			{
				Id:   ptr("1"),
				Name: ptr("my_role"),
				PermissionSet: &v4.PermissionSet{
					Id:          ptr("2"),
					Name:        ptr("my_perm_set"),
					Permissions: &[]string{"access_data", "see_looks"},
				},
				ModelSet: &v4.ModelSet{
					Id:     ptr("3"),
					Name:   ptr("my_model_set"),
					Models: &[]string{"my_model"},
				},
			},
		}
		b, _ := json.Marshal(roles)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && path == "/connections" {
		conns := []v4.DBConnection{
			{
				Name:    ptr("my_conn"),
				Dialect: &v4.Dialect{Name: ptr("mysql")},
				Host:    ptr("localhost"),
			},
		}
		b, _ := json.Marshal(conns)
		return json.Unmarshal(b, result)
	}
	if method == "GET" && strings.HasPrefix(path, "/queries/slug/") {
		slug := strings.TrimPrefix(path, "/queries/slug/")
		slug = strings.Split(slug, "?")[0]
		if slug == "my_slug" {
			q := v4.Query{Id: ptr("9999")}
			b, _ := json.Marshal(q)
			return json.Unmarshal(b, result)
		}
		return fmt.Errorf("query for slug %s not found", slug)
	}
	if method == "GET" && strings.HasPrefix(path, "/queries/") && strings.Contains(path, "/run/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 5 && parts[1] == "queries" && parts[3] == "run" {
			qID := parts[2]
			format := parts[4]
			format = strings.Split(format, "?")[0]
			resStr := fmt.Sprintf(`{"query_id": "%s", "format": "%s", "result": "mocked_data"}`, qID, format)
			if target, ok := result.(*string); ok {
				*target = resStr
				return nil
			}
			b, _ := json.Marshal(resStr)
			return json.Unmarshal(b, result)
		}
	}
	if method == "POST" && strings.HasPrefix(path, "/queries/run/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 4 && parts[1] == "queries" && parts[2] == "run" {
			format := parts[3]
			format = strings.Split(format, "?")[0]
			bodyBytes, _ := json.Marshal(body)
			resStr := fmt.Sprintf(`{"format": "%s", "query_body": %s, "result": "mocked_inline_data"}`, format, string(bodyBytes))
			if target, ok := result.(*string); ok {
				*target = resStr
				return nil
			}
			b, _ := json.Marshal(resStr)
			return json.Unmarshal(b, result)
		}
	}

	if method == "POST" && path == "/projects" {
		var wp v4.WriteProject
		bodyBytes, _ := json.Marshal(body)
		_ = json.Unmarshal(bodyBytes, &wp)

		projectName := "default_project"
		if wp.Name != nil {
			projectName = *wp.Name
		}

		project := v4.Project{
			Id:   ptr(projectName),
			Name: ptr(projectName),
		}
		b, _ := json.Marshal(project)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && strings.HasPrefix(path, "/projects/") && strings.HasSuffix(path, "/validate") {
		parts := strings.Split(path, "/")
		projectID := parts[2]

		var errors []v4.ProjectError
		if projectID == "invalid_project" {
			errors = []v4.ProjectError{
				{
					Severity:   ptr("error"),
					FilePath:   ptr("model.model.lkml"),
					LineNumber: ptrInt64(10),
					Message:    ptr("Model configuration error"),
				},
			}
		}

		validation := v4.ProjectValidation{
			Errors: &errors,
		}
		b, _ := json.Marshal(validation)
		return json.Unmarshal(b, result)
	}
	if method == "POST" && strings.HasPrefix(path, "/projects/") && strings.HasSuffix(path, "/commit") {
		b, _ := json.Marshal("Commit successful")
		return json.Unmarshal(b, result)
	}
	if method == "POST" && strings.HasPrefix(path, "/projects/") && strings.HasSuffix(path, "/push_branch") {
		b, _ := json.Marshal("Push successful")
		return json.Unmarshal(b, result)
	}

	return fmt.Errorf("mock path not found: %s", path)
}

func TestUserMeCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"user", "me", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "1234") || !strings.Contains(out, "jsmith@mycompany.com") {
		t.Errorf("expected 1234 and jsmith, got %s", out)
	}
}

func TestFolderLsCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"primary", "folder"},
		{"alias", "space"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
			defer func() { MockSDK = nil }()

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			RootCmd.SetArgs([]string{tt.cmd, "ls", "709", "--plain"})
			err := RootCmd.Execute()
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			_ = w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			out := buf.String()

			if !strings.Contains(out, "Daily Profit") || !strings.Contains(out, "Daily Profit Dashboard") {
				t.Errorf("expected Daily Profit, got %s", out)
			}
		})
	}
}

func TestConnectionCatCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"connection", "cat", "my_conn"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "my_conn") || !strings.Contains(out, "mysql") {
		t.Errorf("expected my_conn and mysql, got %s", out)
	}
}

func TestConnectionRmCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"connection", "rm", "my_conn"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "deleted") {
		t.Errorf("expected deleted message, got %s", out)
	}
}

func TestConnectionImportCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	tmpFile, _ := os.CreateTemp("", "conn*.json")
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString(`{"name":"my_new_conn","dialect_name":"postgres"}`)
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"connection", "import", tmpFile.Name(), "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "my_new_conn" {
		t.Errorf("expected my_new_conn, got %s", out)
	}
}

func TestConnectionImportCommand_Stdin(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	rStdin, wStdin, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = rStdin
	defer func() { os.Stdin = oldStdin }()

	go func() {
		defer func() { _ = wStdin.Close() }()
		_, _ = wStdin.WriteString(`{"name":"my_new_conn","dialect_name":"postgres"}`)
	}()

	oldStdout := os.Stdout
	rStdout, wStdout, _ := os.Pipe()
	os.Stdout = wStdout

	RootCmd.SetArgs([]string{"connection", "import", "-", "--plain"})
	err = RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = wStdout.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rStdout)
	out := strings.TrimSpace(buf.String())

	if out != "my_new_conn" {
		t.Errorf("expected my_new_conn, got %s", out)
	}
}

func TestConnectionTestCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"connection", "test", "my_conn", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "connect") || !strings.Contains(out, "success") || !strings.Contains(out, "connected successfully") {
		t.Errorf("expected test success results, got %s", out)
	}
}

func TestDashboardMvCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "mv", "192", "801", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "" {
		t.Errorf("expected empty output for plain mv, got %s", out)
	}
}

func TestDashboardImportLookmlCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "import", "lookml", "lookml_dash_1", "801", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "new_udd_1" {
		t.Errorf("expected new_udd_1, got %s", out)
	}
}

func TestDashboardSyncLookmlCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "sync", "lookml", "lookml_dash_1"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if !strings.Contains(out, "101") || !strings.Contains(out, "102") {
		t.Errorf("expected synced dashboard ids 101 and 102, got %s", out)
	}
}

func TestLookMvCommand(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		md := &mockDoer{
			t:              t,
			folder801Looks: []v4.LookWithQuery{},
		}
		MockSDK = v4.NewLookerSDK(md)
		defer func() { MockSDK = nil }()

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs([]string{"look", "mv", "857", "801", "--plain"})
		err := RootCmd.Execute()
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := strings.TrimSpace(buf.String())

		if out != "" {
			t.Errorf("expected empty output for plain mv, got %q", out)
		}
	})

	t.Run("DuplicateError", func(t *testing.T) {
		md := &mockDoer{
			t: t,
			folder801Looks: []v4.LookWithQuery{
				{Id: ptr("999"), Title: ptr("Daily Profit")},
			},
		}
		MockSDK = v4.NewLookerSDK(md)
		defer func() { MockSDK = nil }()

		RootCmd.SetArgs([]string{"look", "mv", "857", "801"})
		err := RootCmd.Execute()
		if err == nil {
			t.Fatal("expected error due to duplicate title, got nil")
		}
		expectedErr := "already exists in folder 801"
		if !strings.Contains(err.Error(), expectedErr) {
			t.Errorf("expected error containing %q, got %q", expectedErr, err.Error())
		}
	})

	t.Run("ForceOverwrite", func(t *testing.T) {
		md := &mockDoer{
			t: t,
			folder801Looks: []v4.LookWithQuery{
				{Id: ptr("999"), Title: ptr("Daily Profit")},
			},
		}
		MockSDK = v4.NewLookerSDK(md)
		defer func() { MockSDK = nil }()

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs([]string{"look", "mv", "857", "801", "--force", "--plain"})
		err := RootCmd.Execute()
		if err != nil {
			t.Fatalf("Execute failed with --force: %v", err)
		}

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := strings.TrimSpace(buf.String())

		if out != "" {
			t.Errorf("expected empty output for plain mv with force, got %q", out)
		}
	})
}

func TestModelCatCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"model", "cat", "my_model"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "my_model") || !strings.Contains(out, "my_project") {
		t.Errorf("expected my_model and my_project, got %s", out)
	}
}

func TestModelImportCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	tmpFile, _ := os.CreateTemp("", "model*.json")
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString(`{"name":"my_model","project_name":"my_project"}`)
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"model", "import", tmpFile.Name(), "--force", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "my_model" {
		t.Errorf("expected my_model, got %s", out)
	}
}

func TestModelSetLsCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"model", "set", "ls", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "5") || !strings.Contains(out, "my_model_set") {
		t.Errorf("expected 5 and my_model_set, got %s", out)
	}
}

func TestModelSetCatCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"model", "set", "cat", "5"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "5") || !strings.Contains(out, "my_model_set") {
		t.Errorf("expected 5 and my_model_set, got %s", out)
	}
}

func TestModelSetImportCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	tmpFile, _ := os.CreateTemp("", "modelset*.json")
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString(`{"name":"my_model_set","models":["my_model"]}`)
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"model", "set", "import", tmpFile.Name(), "--force", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "5" {
		t.Errorf("expected 5, got %s", out)
	}
}

func TestModelSetRmCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"model", "set", "rm", "5"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "deleted") {
		t.Errorf("expected deleted message, got %s", out)
	}
}

func TestPermissionTreeCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"permission", "tree"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	// Verify tree formatting:
	// should have access_data as root and see_looks as child
	if !strings.Contains(out, "├── access_data") || !strings.Contains(out, "│   └── see_looks") || !strings.Contains(out, "└── admin") {
		t.Errorf("unexpected tree output structure, got:\n%s", out)
	}
}

func TestPermissionSetLsCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"permission", "set", "ls", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "3") || !strings.Contains(out, "my_permission_set") {
		t.Errorf("expected 3 and my_permission_set, got %s", out)
	}
}

func TestPermissionSetCatCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"permission", "set", "cat", "3"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "3") || !strings.Contains(out, "my_permission_set") {
		t.Errorf("expected 3 and my_permission_set, got %s", out)
	}
}

func TestPermissionSetCatCommandFields(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() {
		MockSDK = nil
		permissionSetCatFields = ""
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"permission", "set", "cat", "3", "--fields", "name"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if strings.Contains(out, "3") {
		t.Errorf("expected NOT to contain '3' (id), got %s", out)
	}
	if !strings.Contains(out, "my_permission_set") {
		t.Errorf("expected to contain my_permission_set, got %s", out)
	}
}

func TestPermissionSetImportCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	tmpFile, _ := os.CreateTemp("", "permsset*.json")
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString(`{"name":"my_permission_set","permissions":["access_data"]}`)
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"permission", "set", "import", tmpFile.Name(), "--force", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "3" {
		t.Errorf("expected 3, got %s", out)
	}
}

func TestPermissionSetRmCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"permission", "set", "rm", "3"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "deleted") {
		t.Errorf("expected deleted message, got %s", out)
	}
}

func TestFolderTopCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"primary", "folder"},
		{"alias", "space"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
			defer func() { MockSDK = nil }()

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			RootCmd.SetArgs([]string{tt.cmd, "top", "--plain"})
			err := RootCmd.Execute()
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			_ = w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			out := buf.String()

			// Verify output contains Shared Root and Users Root but not Personal Space
			if !strings.Contains(out, "Shared Root") || !strings.Contains(out, "Users Root") {
				t.Errorf("expected Shared Root and Users Root, got:\n%s", out)
			}
			if strings.Contains(out, "Personal Space") {
				t.Errorf("unexpected Personal Space in top-level folders, got:\n%s", out)
			}
		})
	}
}

func TestInitClient_EnvVars(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "looker_cli_test_cmd_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	origHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)

	// Clean up existing environment variables at end of test
	origBase := os.Getenv("LOOKERSDK_BASE_URL")
	origVerify := os.Getenv("LOOKERSDK_VERIFY_SSL")
	defer func() {
		_ = os.Setenv("LOOKERSDK_BASE_URL", origBase)
		_ = os.Setenv("LOOKERSDK_VERIFY_SSL", origVerify)
		_ = os.Setenv("HOME", origHome)
	}()

	// Reset flags to their default values
	_ = RootCmd.PersistentFlags().Set("host", "localhost")
	_ = RootCmd.PersistentFlags().Set("port", "19999")
	_ = RootCmd.PersistentFlags().Set("verify-ssl", "true")

	// Reset flag "changed" statuses to false by creating a fresh Command instance or modifying manually
	// Cobra doesn't let us easily reset "Changed" without clearing flags, but we can check the behavior
	// by parsing a clean argument list or just executing in isolation.
	// Let's make sure they are considered UNCHANGED. We can do this by resetting the Command state.
	RootCmd.PersistentFlags().Lookup("host").Changed = false
	RootCmd.PersistentFlags().Lookup("port").Changed = false
	RootCmd.PersistentFlags().Lookup("verify-ssl").Changed = false

	t.Run("Environment variables fallbacks when flags are omitted", func(t *testing.T) {
		_ = os.Setenv("LOOKERSDK_BASE_URL", "https://env-host-name.com:8888")
		_ = os.Setenv("LOOKERSDK_VERIFY_SSL", "false")

		// Initialize client wrapper (uses mock since MockSDK is nil, it tries to create a real one,
		// but wait: if MockSDK is nil, client.NewClient will try to read from token file or login,
		// which might error if there is no active login.
		// Let's temporarily set MockSDK to bypass actual login, but wait:
		// if MockSDK != nil, initClient bypasses NewClient completely and returns:
		// &client.ClientWrapper{SDK: MockSDK, Host: cfgHost, SuUser: cfgSuUser}
		// In that case, it doesn't use the parsed env values!
		// So to test NewClient's parsing logic, we must allow client.NewClient to run.
		// To allow client.NewClient to run without actual API calls or file reads, we can mock the token environment.
		// Let's point HOME to a temp dir so it sees no token file, and bypass login since we aren't using oauth.
		// If oauth=false and no client ID is set, client.NewClient will check netrc or env.
		// Let's make sure we set clientID and clientSecret in env so it compiles settings successfully.
		_ = os.Setenv("LOOKERSDK_CLIENT_ID", "dummy")
		_ = os.Setenv("LOOKERSDK_CLIENT_SECRET", "dummy")
		defer func() {
			_ = os.Unsetenv("LOOKERSDK_CLIENT_ID")
			_ = os.Unsetenv("LOOKERSDK_CLIENT_SECRET")
		}()

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Host != "env-host-name.com" {
			t.Errorf("expected host 'env-host-name.com' from env, got '%s'", wrapper.Host)
		}
		if wrapper.Session.Config.BaseUrl != "https://env-host-name.com:8888" {
			t.Errorf("expected base URL 'https://env-host-name.com:8888', got '%s'", wrapper.Session.Config.BaseUrl)
		}
		if wrapper.Session.Config.VerifySsl {
			t.Errorf("expected verifySSL to be false from env, got true")
		}
	})

	t.Run("Explicit flags take precedence over environment variables", func(t *testing.T) {
		_ = os.Setenv("LOOKERSDK_BASE_URL", "https://env-host-name.com:8888")
		_ = os.Setenv("LOOKERSDK_VERIFY_SSL", "false")
		_ = os.Setenv("LOOKERSDK_CLIENT_ID", "dummy")
		_ = os.Setenv("LOOKERSDK_CLIENT_SECRET", "dummy")
		defer func() {
			_ = os.Unsetenv("LOOKERSDK_CLIENT_ID")
			_ = os.Unsetenv("LOOKERSDK_CLIENT_SECRET")
		}()

		// Explicitly set flags
		_ = RootCmd.PersistentFlags().Set("host", "flag-host.com")
		_ = RootCmd.PersistentFlags().Set("port", "1111")
		_ = RootCmd.PersistentFlags().Set("verify-ssl", "true")

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Host != "flag-host.com" {
			t.Errorf("expected host 'flag-host.com' from flag, got '%s'", wrapper.Host)
		}
		if wrapper.Session.Config.BaseUrl != "https://flag-host.com:1111" {
			t.Errorf("expected base URL 'https://flag-host.com:1111', got '%s'", wrapper.Session.Config.BaseUrl)
		}
		if !wrapper.Session.Config.VerifySsl {
			t.Errorf("expected verifySSL to be true from flag, got false")
		}
	})
}

func TestRoleCatCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"role", "cat", "1"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "my_role") || !strings.Contains(out, "my_perm_set") {
		t.Errorf("expected my_role and my_perm_set, got %s", out)
	}
}

func TestRoleCatCommandFields(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() {
		MockSDK = nil
		roleCatFields = ""
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"role", "cat", "1", "--fields", "name"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "my_role") {
		t.Errorf("expected to contain my_role, got %s", out)
	}
	if strings.Contains(out, "my_perm_set") {
		t.Errorf("expected NOT to contain my_perm_set, got %s", out)
	}
	if strings.Contains(out, `"id"`) {
		t.Errorf("expected NOT to contain 'id', got %s", out)
	}
}

func TestQueryRunQuery(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	captureStdout := func(args []string) (string, error) {
		queryRunInputFile = ""
		queryRunOutputFile = ""
		queryRunFormat = "json"

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs(args)
		err := RootCmd.Execute()

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		return buf.String(), err
	}

	t.Run("Run by ID", func(t *testing.T) {
		out, err := captureStdout([]string{"query", "runquery", "1234"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !strings.Contains(out, `"query_id": "1234"`) || !strings.Contains(out, `"format": "json"`) {
			t.Errorf("unexpected output: %s", out)
		}
	})

	t.Run("Run by Slug", func(t *testing.T) {
		out, err := captureStdout([]string{"query", "runquery", "my_slug"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !strings.Contains(out, `"query_id": "9999"`) {
			t.Errorf("expected query_id 9999 for slug my_slug, got: %s", out)
		}
	})

	t.Run("Run by JSON inline", func(t *testing.T) {
		out, err := captureStdout([]string{"query", "runquery", `{"model":"test_model","view":"test_view"}`})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !strings.Contains(out, "mocked_inline_data") || !strings.Contains(out, "test_model") {
			t.Errorf("unexpected output: %s", out)
		}
	})

	t.Run("Run by File", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "query*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer func() { _ = os.Remove(tmpFile.Name()) }()

		queryDef := `{"model":"file_model","view":"file_view"}`
		_, _ = tmpFile.WriteString(queryDef)
		_ = tmpFile.Close()

		out, err := captureStdout([]string{"query", "runquery", "--file", tmpFile.Name()})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !strings.Contains(out, "mocked_inline_data") || !strings.Contains(out, "file_model") {
			t.Errorf("unexpected output: %s", out)
		}
	})

	t.Run("Error both args and file", func(t *testing.T) {
		_, err := captureStdout([]string{"query", "runquery", "1234", "--file", "somefile.json"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "cannot provide both QUERY_DEF argument and --file flag") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Error neither args nor file", func(t *testing.T) {
		_, err := captureStdout([]string{"query", "runquery"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "either QUERY_DEF argument or --file flag must be provided") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Error file not found", func(t *testing.T) {
		_, err := captureStdout([]string{"query", "runquery", "--file", "non_existent_file.json"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to read input file") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestParseFieldsForHeaders(t *testing.T) {
	tests := []struct {
		fields   string
		expected []string
	}{
		{
			"parent_id,id,name",
			[]string{"parent_id", "id", "name"},
		},
		{
			"parent_id,id,name,looks(id,title)",
			[]string{"parent_id", "id", "name", "looks(id)", "looks(title)"},
		},
		{
			"parent_id,id,name,looks(id,title),dashboards(id,title)",
			[]string{"parent_id", "id", "name", "looks(id)", "looks(title)", "dashboards(id)", "dashboards(title)"},
		},
		{
			"looks(id,title),dashboards(id,title)",
			[]string{"looks(id)", "looks(title)", "dashboards(id)", "dashboards(title)"},
		},
		{
			"id,name,permission_set(name,permissions),model_set(name,models)",
			[]string{"id", "name", "permission_set(name)", "permission_set(permissions)", "model_set(name)", "model_set(models)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.fields, func(t *testing.T) {
			actual := util.ParseFieldsForHeaders(tt.fields)
			if len(actual) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, actual)
			}
			for i, v := range actual {
				if v != tt.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestHeaderToParts(t *testing.T) {
	tests := []struct {
		header   string
		expected []string
	}{
		{"name", []string{"name"}},
		{"permission_set(id)", []string{"permission_set", "id"}},
		{"model_set(name)", []string{"model_set", "name"}},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			actual := util.HeaderToParts(tt.header)
			if len(actual) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, actual)
			}
			for i, v := range actual {
				if v != tt.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestRoleLsCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"role", "ls", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	expected := []string{"1", "my_role", "2", "my_perm_set", "access_data\nsee_looks", "3", "my_model_set", "my_model"}
	for _, exp := range expected {
		if !strings.Contains(out, exp) {
			t.Errorf("expected to contain %q, got %q", exp, out)
		}
	}
}

func TestConnectionLsCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"connection", "ls", "--plain"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	expected := []string{"my_conn", "mysql", "localhost"}
	for _, exp := range expected {
		if !strings.Contains(out, exp) {
			t.Errorf("expected to contain %q, got %q", exp, out)
		}
	}
}

func TestExtractFieldsSlice(t *testing.T) {
	role := v4.Role{
		Id:   ptr("1"),
		Name: ptr("my_role"),
		PermissionSet: &v4.PermissionSet{
			Name:        ptr("my_perm_set"),
			Permissions: &[]string{"access_data", "see_looks"},
		},
	}

	fields := "permission_set(permissions)"
	row := extractFields(role, fields)
	expected := "access_data\nsee_looks"
	if row[0] != expected {
		t.Errorf("expected %q, got %q", expected, row[0])
	}
}
func ptrInt64(i int64) *int64 { return &i }

func TestProjectCreateCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"project", "create", "new_project"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	expected := "Created project new_project"
	if !strings.Contains(out, expected) {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestProjectValidateCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	t.Run("valid project", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs([]string{"project", "validate", "valid_project"})
		err := RootCmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()

		expected := "Project is valid."
		if !strings.Contains(out, expected) {
			t.Errorf("expected %q, got %q", expected, out)
		}
	})

	t.Run("invalid project", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs([]string{"project", "validate", "invalid_project"})
		err := RootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()

		expectedErr := "project validation failed with 1 errors"
		if !strings.Contains(err.Error(), expectedErr) {
			t.Errorf("expected error %q, got %q", expectedErr, err.Error())
		}

		// It should print the table with the error
		if !strings.Contains(out, "model.model.lkml") {
			t.Errorf("expected output to contain file path, got %q", out)
		}
	})
}

func TestProjectCommitCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	t.Run("commit with message and files", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs([]string{"project", "commit", "my_proj", "file1.lkml", "file2.lkml", "-m", "Initial commit", "--amend"})
		err := RootCmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()

		if !strings.Contains(out, "Commit successful") {
			t.Errorf("expected Commit successful, got %q", out)
		}
	})

	t.Run("commit missing required message flag", func(t *testing.T) {
		projectCommitMessage = ""
		_ = projectCommitCmd.Flags().Lookup("message").Value.Set("")
		projectCommitCmd.Flags().Lookup("message").Changed = false
		RootCmd.SetArgs([]string{"project", "commit", "my_proj"})
		err := RootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error for missing --message flag, got nil")
		}
	})
}

func TestProjectPushBranchCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	t.Run("push-branch command", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		RootCmd.SetArgs([]string{"project", "push-branch", "my_proj"})
		err := RootCmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()

		if !strings.Contains(out, "Push successful") {
			t.Errorf("expected Push successful, got %q", out)
		}
	})
}

func TestDashboardImportCommand(t *testing.T) {
	doer := &mockDoer{t: t}
	MockSDK = v4.NewLookerSDK(doer)
	defer func() { MockSDK = nil }()

	dashJSON := `{
  "title": "Test Dash",
  "dashboard_elements": [
    {
      "id": "7523",
      "type": "text",
      "body_text": "hello"
    }
  ],
  "dashboard_layouts": [
    {
      "id": "layout_1",
      "active": true,
      "dashboard_layout_components": [
        {
          "id": "comp_1",
          "dashboard_element_id": "7523"
        }
      ]
    }
  ]
}`

	tmpFile, err := os.CreateTemp("", "dashboard_import_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write([]byte(dashJSON)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain"})
	err = RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "new_dash_1" {
		t.Errorf("expected new_dash_1, got %s", out)
	}

	if doer.elementsCreated != 1 {
		t.Errorf("expected 1 element created, got %d", doer.elementsCreated)
	}
}

func TestDashboardImportFallbackCommand(t *testing.T) {
	doer := &mockDoer{t: t}
	MockSDK = v4.NewLookerSDK(doer)
	defer func() { MockSDK = nil }()

	dashJSON := `{
  "title": "Test Fallback Dash",
  "dashboard_elements": [
    {
      "title": "My Boxplot",
      "type": "text",
      "body_text": "hello"
    }
  ],
  "dashboard_layouts": [
    {
      "id": "layout_1",
      "active": true,
      "dashboard_layout_components": [
        {
          "id": "comp_1",
          "dashboard_element_id": "270",
          "element_title": "My Boxplot"
        }
      ]
    }
  ]
}`

	tmpFile, err := os.CreateTemp("", "dashboard_import_fallback_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write([]byte(dashJSON)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain"})
	err = RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "new_dash_1" {
		t.Errorf("expected new_dash_1, got %s", out)
	}

	if doer.elementsCreated != 1 {
		t.Errorf("expected 1 element created via fallback, got %d", doer.elementsCreated)
	}
}

func TestDashboardCatCommand(t *testing.T) {
	MockSDK = v4.NewLookerSDK(&mockDoer{t: t})
	defer func() { MockSDK = nil }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "cat", "3188"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	// Verify that merge_result is present and contains the source query with its definition
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("Failed to unmarshal output: %v\nOutput was: %s", err, out)
	}

	elements, ok := m["dashboard_elements"].([]interface{})
	if !ok || len(elements) == 0 {
		t.Fatalf("No dashboard elements found in output: %s", out)
	}

	elem := elements[0].(map[string]interface{})
	mergeResult, ok := elem["merge_result"].(map[string]interface{})
	if !ok {
		t.Fatalf("merge_result missing from dashboard element: %s", out)
	}

	sourceQueries, ok := mergeResult["source_queries"].([]interface{})
	if !ok || len(sourceQueries) == 0 {
		t.Fatalf("source_queries missing or empty in merge_result: %s", out)
	}

	sq := sourceQueries[0].(map[string]interface{})
	query, ok := sq["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query definition missing from source query: %s", out)
	}

	if query["model"] != "test_model" || query["view"] != "test_view" {
		t.Errorf("Unexpected query definition: %v", query)
	}
}

func TestDashboardImportWithMergeQueryCommand(t *testing.T) {
	doer := &mockDoer{t: t}
	MockSDK = v4.NewLookerSDK(doer)
	defer func() {
		MockSDK = nil
		dashboardImportPlain = false
	}()

	dashJSON := `{
  "title": "Import Merge Query Dash",
  "dashboard_elements": [
    {
      "title": "Merge Element",
      "type": "vis",
      "merge_result_id": "old_merge_123",
      "merge_result": {
        "source_queries": [
          {
            "query_id": "old_query_456",
            "name": "source_query_1",
            "query": {
              "model": "test_model",
              "view": "test_view",
              "fields": ["field1", "field2"]
            }
          }
        ]
      }
    }
  ],
  "dashboard_layouts": [
    {
      "id": "layout_1",
      "active": true,
      "dashboard_layout_components": [
        {
          "id": "comp_1",
          "dashboard_element_id": "elem_1",
          "element_title": "Merge Element"
        }
      ]
    }
  ]
}`

	tmpFile, err := os.CreateTemp("", "dashboard_import_merge_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write([]byte(dashJSON)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain"})
	err = RootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	if out != "new_dash_1" {
		t.Errorf("expected new_dash_1, got %s", out)
	}
}

func TestDashboardImportForce(t *testing.T) {
	doer := &statefulMockDoer{
		t:          t,
		dashboards: make(map[string]v4.Dashboard),
	}
	MockSDK = v4.NewLookerSDK(doer)
	defer func() {
		MockSDK = nil
		dashboardImportForce = false
		dashboardImportPlain = false
	}()

	dashJSON := `{
  "title": "Force Test Dash",
  "slug": "force_test_slug",
  "dashboard_elements": []
}`

	tmpFile, err := os.CreateTemp("", "dashboard_import_force_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write([]byte(dashJSON)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	// First import (create)
	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain"})
	err = RootCmd.Execute()
	if err != nil {
		t.Fatalf("First import failed: %v", err)
	}

	if len(doer.dashboards) != 1 {
		t.Errorf("Expected 1 dashboard, got %d", len(doer.dashboards))
	}

	// Second import without force (should fail)
	// We need to reset flags or command state if needed, but Cobra might retain state if we don't.
	// Actually, SetArgs overrides args.
	// But --force flag is a package variable in dashboard.go?
	// Let's check dashboard.go:
	// var dashboardImportForce bool
	// Yes, it is a package variable.
	// Cobra binding:
	// dashboardImportCmd.Flags().BoolVar(&dashboardImportForce, "force", false, "Overwrite existing dashboard")
	// If we run Execute again, Cobra should reset it if we use a new command, or if we reset it manually.
	// Since we reuse RootCmd, the flag value might persist if we don't reset it.
	// Actually, Cobra parses flags every time, but if it's not present, it might keep the previous value?
	// Usually Cobra resets flags to default values before parsing if we use ParseFlags, but Execute might not reset global variables.
	// Actually, BoolVar binds to a pointer. Cobra will set it to true if flag is present, but if it is NOT present, does it set it to false?
	// Yes, Cobra default value is applied if not present, but it might not reset the variable if it was already set.
	// Wait, Cobra's default value is applied by setting the variable.
	// Let's reset it manually in test to be safe.
	dashboardImportForce = false

	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain"})
	err = RootCmd.Execute()
	if err == nil {
		t.Errorf("Expected second import without force to fail, but it succeeded")
	}

	// Second import with force (should succeed and update)
	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain", "--force"})
	err = RootCmd.Execute()
	if err != nil {
		t.Errorf("Second import with force failed: %v", err)
	}

	if len(doer.dashboards) != 1 {
		t.Errorf("Expected still 1 dashboard after force import, got %d", len(doer.dashboards))
	}
}

func TestDashboardImportHiddenSlugConflict(t *testing.T) {
	doer := &statefulMockDoer{
		t:          t,
		dashboards: make(map[string]v4.Dashboard),
	}
	MockSDK = v4.NewLookerSDK(doer)
	defer func() {
		MockSDK = nil
		dashboardImportPlain = false
	}()

	dashJSON := `{
  "title": "Hidden Conflict Dash",
  "slug": "conflicting_hidden_slug",
  "dashboard_elements": []
}`

	tmpFile, err := os.CreateTemp("", "dashboard_import_hidden_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write([]byte(dashJSON)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	RootCmd.SetArgs([]string{"dashboard", "import", tmpFile.Name(), "801", "--plain"})
	err = RootCmd.Execute()
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if len(doer.dashboards) != 1 {
		t.Errorf("Expected 1 dashboard, got %d", len(doer.dashboards))
	}

	for _, d := range doer.dashboards {
		if d.Slug != nil {
			t.Errorf("Expected slug to be nil (generated), got %s", *d.Slug)
		}
	}
}
