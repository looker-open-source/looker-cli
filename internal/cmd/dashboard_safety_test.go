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
	"path/filepath"
	"strings"
	"testing"

	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

// contentMockDoer is a small stateful fake of the Looker API covering looks,
// dashboards and dashboard contents, with per-call failure injection. It
// records every call so tests can assert what was (and wasn't) changed.
type contentMockDoer struct {
	t          *testing.T
	looks      map[string]v4.LookWithQuery
	dashboards map[string]v4.Dashboard
	alerts     []v4.Alert
	// hideFolderID makes GET /looks/{id} and GET /dashboards/{id} omit
	// folder_id, like a caller that didn't request that field.
	hideFolderID bool
	// fail maps "METHOD /path" (or "METHOD /path-prefix*") to an error.
	fail  map[string]error
	calls []string
	seq   int
}

func newContentMockDoer(t *testing.T) *contentMockDoer {
	return &contentMockDoer{
		t:          t,
		looks:      map[string]v4.LookWithQuery{},
		dashboards: map[string]v4.Dashboard{},
		fail:       map[string]error{},
	}
}

func (m *contentMockDoer) nextID(prefix string) string {
	m.seq++
	return fmt.Sprintf("%s_%d", prefix, m.seq)
}

func (m *contentMockDoer) mutatingCalls() []string {
	var out []string
	for _, c := range m.calls {
		if !strings.HasPrefix(c, "GET ") {
			out = append(out, c)
		}
	}
	return out
}

func (m *contentMockDoer) called(call string) bool {
	for _, c := range m.calls {
		if c == call {
			return true
		}
	}
	return false
}

func (m *contentMockDoer) injectedError(call string) error {
	if err, ok := m.fail[call]; ok {
		return err
	}
	for k, err := range m.fail {
		if strings.HasSuffix(k, "*") && strings.HasPrefix(call, strings.TrimSuffix(k, "*")) {
			return err
		}
	}
	return nil
}

func respond(result interface{}, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}

func decodeBody(body interface{}, target interface{}) {
	b, _ := json.Marshal(body)
	_ = json.Unmarshal(b, target)
}

func strParam(reqPars map[string]interface{}, key string) string {
	if p, ok := reqPars[key].(*string); ok && p != nil {
		return *p
	}
	return ""
}

func (m *contentMockDoer) Do(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
	call := method + " " + path
	m.calls = append(m.calls, call)
	if err := m.injectedError(call); err != nil {
		return err
	}

	switch {
	case method == "GET" && path == "/user":
		return respond(result, v4.User{Id: ptr("1234")})

	case method == "GET" && strings.HasPrefix(path, "/looks/"):
		l, ok := m.looks[strings.TrimPrefix(path, "/looks/")]
		if !ok {
			return fmt.Errorf("response error. status=404 Not Found")
		}
		if m.hideFolderID {
			l.FolderId = nil
		}
		return respond(result, l)

	case method == "GET" && strings.HasPrefix(path, "/folders/") && strings.HasSuffix(path, "/looks"):
		folderID := strings.TrimSuffix(strings.TrimPrefix(path, "/folders/"), "/looks")
		var out []v4.LookWithQuery
		for _, l := range m.looks {
			if l.FolderId != nil && *l.FolderId == folderID {
				out = append(out, l)
			}
		}
		return respond(result, out)

	case method == "DELETE" && strings.HasPrefix(path, "/looks/"):
		delete(m.looks, strings.TrimPrefix(path, "/looks/"))
		return nil

	case method == "PATCH" && strings.HasPrefix(path, "/looks/"):
		id := strings.TrimPrefix(path, "/looks/")
		l, ok := m.looks[id]
		if !ok {
			return fmt.Errorf("response error. status=404 Not Found")
		}
		var wl v4.WriteLookWithQuery
		decodeBody(body, &wl)
		if wl.FolderId != nil {
			l.FolderId = wl.FolderId
		}
		m.looks[id] = l
		return respond(result, l)

	case method == "GET" && path == "/dashboards/search":
		title := strParam(reqPars, "title")
		folderID := strParam(reqPars, "folder_id")
		slug := strParam(reqPars, "slug")
		wantDeleted := strParam(reqPars, "deleted") == "true"
		var out []v4.Dashboard
		for _, d := range m.dashboards {
			isDeleted := d.Deleted != nil && *d.Deleted
			if isDeleted != wantDeleted {
				continue
			}
			// Like Looker, title search is case-insensitive.
			if title != "" && (d.Title == nil || !strings.EqualFold(*d.Title, title)) {
				continue
			}
			if folderID != "" && (d.FolderId == nil || *d.FolderId != folderID) {
				continue
			}
			if slug != "" && (d.Slug == nil || *d.Slug != slug) {
				continue
			}
			out = append(out, d)
		}
		return respond(result, out)

	case method == "GET" && strings.HasPrefix(path, "/dashboards/"):
		d, ok := m.dashboards[strings.TrimPrefix(path, "/dashboards/")]
		if !ok {
			return fmt.Errorf("response error. status=404 Not Found")
		}
		if m.hideFolderID {
			d.FolderId = nil
		}
		return respond(result, d)

	case method == "PATCH" && strings.HasPrefix(path, "/dashboards/"):
		id := strings.TrimPrefix(path, "/dashboards/")
		d, ok := m.dashboards[id]
		if !ok {
			return fmt.Errorf("response error. status=404 Not Found")
		}
		var wd v4.WriteDashboard
		decodeBody(body, &wd)
		if wd.Deleted != nil {
			d.Deleted = wd.Deleted
		}
		if wd.FolderId != nil {
			d.FolderId = wd.FolderId
		}
		if wd.Title != nil {
			d.Title = wd.Title
		}
		m.dashboards[id] = d
		return respond(result, d)

	case method == "POST" && path == "/dashboards":
		var wd v4.WriteDashboard
		decodeBody(body, &wd)
		id := m.nextID("dash")
		d := v4.Dashboard{Id: &id, Title: wd.Title, FolderId: wd.FolderId}
		m.dashboards[id] = d
		return respond(result, d)

	case method == "POST" && path == "/dashboard_filters":
		return respond(result, v4.DashboardFilter{Id: ptr(m.nextID("filter"))})
	case method == "POST" && path == "/dashboard_layouts":
		return respond(result, v4.DashboardLayout{Id: ptr(m.nextID("layout")), Active: ptrBool(true)})
	case method == "PATCH" && strings.HasPrefix(path, "/dashboard_layouts/"):
		return respond(result, v4.DashboardLayout{Id: ptr(strings.TrimPrefix(path, "/dashboard_layouts/")), Active: ptrBool(true)})
	case method == "GET" && strings.HasSuffix(path, "/dashboard_layout_components"):
		return respond(result, []v4.DashboardLayoutComponent{})
	case method == "POST" && path == "/dashboard_elements":
		return respond(result, v4.DashboardElement{Id: ptr(m.nextID("elem"))})
	case method == "POST" && path == "/queries":
		return respond(result, v4.Query{Id: ptr(m.nextID("query"))})
	case method == "GET" && path == "/alerts/search":
		return respond(result, m.alerts)
	case method == "DELETE":
		return nil
	}
	m.t.Errorf("contentMockDoer: unhandled call %s", call)
	return fmt.Errorf("unhandled call %s", call)
}

func useContentMock(t *testing.T, m *contentMockDoer) {
	t.Helper()
	MockSDK = v4.NewLookerSDK(m)
	t.Cleanup(func() {
		MockSDK = nil
		lookMvForce, lookMvPlain = false, false
		dashboardMvForce, dashboardMvPlain = false, false
		dashboardImportForce, dashboardImportPlain = false, false
		dashboardImportBackupDir = ""
		dashboardCatTrim, dashboardCatPlans = false, false
		dashboardCatFields, dashboardCatDir, dashboardCatTransform = "", "", ""
	})
	lookMvForce, lookMvPlain = false, false
	dashboardMvForce, dashboardMvPlain = false, false
	dashboardImportForce, dashboardImportPlain = false, false
	dashboardImportBackupDir = ""
	dashboardCatTrim, dashboardCatPlans = false, false
	dashboardCatFields, dashboardCatDir, dashboardCatTransform = "", "", ""
}

// ---------------------------------------------------------------------------
// look mv / dashboard mv
// ---------------------------------------------------------------------------

func TestLookMvForce_SameFolderIsNoOp(t *testing.T) {
	m := newContentMockDoer(t)
	m.looks["857"] = v4.LookWithQuery{Id: ptr("857"), Title: ptr("Daily Profit"), FolderId: ptr("801")}
	useContentMock(t, m)

	if _, err := executeCommand("look", "mv", "857", "801", "--force"); err != nil {
		t.Fatalf("look mv failed: %v", err)
	}
	if _, ok := m.looks["857"]; !ok {
		t.Fatal("look 857 was deleted by moving it into its own folder")
	}
	if calls := m.mutatingCalls(); len(calls) != 0 {
		t.Errorf("expected no changes for a no-op move, got %v", calls)
	}
}

// Regression test: when the look's own folder wasn't known, `--force` found
// the look itself as a title conflict in the target folder and hard-deleted it.
func TestLookMvForce_NeverDeletesItself(t *testing.T) {
	m := newContentMockDoer(t)
	m.hideFolderID = true
	m.looks["857"] = v4.LookWithQuery{Id: ptr("857"), Title: ptr("Daily Profit"), FolderId: ptr("801")}
	useContentMock(t, m)

	if _, err := executeCommand("look", "mv", "857", "801", "--force"); err != nil {
		t.Fatalf("look mv failed: %v", err)
	}
	if m.called("DELETE /looks/857") {
		t.Fatal("look mv --force deleted the look being moved")
	}
	if _, ok := m.looks["857"]; !ok {
		t.Fatal("look 857 no longer exists")
	}
}

func TestLookMvForce_ReplacesOtherLookWithSameTitle(t *testing.T) {
	m := newContentMockDoer(t)
	m.looks["857"] = v4.LookWithQuery{Id: ptr("857"), Title: ptr("Daily Profit"), FolderId: ptr("700")}
	m.looks["999"] = v4.LookWithQuery{Id: ptr("999"), Title: ptr("Daily Profit"), FolderId: ptr("801")}
	useContentMock(t, m)

	if _, err := executeCommand("look", "mv", "857", "801", "--force", "--plain"); err != nil {
		t.Fatalf("look mv failed: %v", err)
	}
	if _, ok := m.looks["999"]; ok {
		t.Error("expected the other look with the same title to be replaced")
	}
	if l := m.looks["857"]; l.FolderId == nil || *l.FolderId != "801" {
		t.Errorf("expected look 857 to be moved to folder 801, got %+v", l)
	}
}

func TestLookMv_ConflictLookupFailureAborts(t *testing.T) {
	m := newContentMockDoer(t)
	m.looks["857"] = v4.LookWithQuery{Id: ptr("857"), Title: ptr("Daily Profit"), FolderId: ptr("700")}
	m.fail["GET /folders/801/looks"] = fmt.Errorf("response error. status=500")
	useContentMock(t, m)

	if _, err := executeCommand("look", "mv", "857", "801", "--force"); err == nil {
		t.Fatal("expected look mv to fail when the target folder can't be checked for conflicts")
	}
	if calls := m.mutatingCalls(); len(calls) != 0 {
		t.Errorf("expected no changes after a failed conflict check, got %v", calls)
	}
}

func TestDashboardMvForce_SameFolderIsNoOp(t *testing.T) {
	m := newContentMockDoer(t)
	m.dashboards["192"] = v4.Dashboard{Id: ptr("192"), Title: ptr("Sales"), FolderId: ptr("801")}
	useContentMock(t, m)

	if _, err := executeCommand("dashboard", "mv", "192", "801", "--force"); err != nil {
		t.Fatalf("dashboard mv failed: %v", err)
	}
	if d := m.dashboards["192"]; d.Deleted != nil && *d.Deleted {
		t.Fatal("dashboard 192 was trashed by moving it into its own folder")
	}
	if calls := m.mutatingCalls(); len(calls) != 0 {
		t.Errorf("expected no changes for a no-op move, got %v", calls)
	}
}

// Regression test: `dashboard mv --force` soft-deleted the dashboard being
// moved when it showed up in the target folder's title search.
func TestDashboardMvForce_NeverTrashesItself(t *testing.T) {
	m := newContentMockDoer(t)
	m.hideFolderID = true
	m.dashboards["192"] = v4.Dashboard{Id: ptr("192"), Title: ptr("Sales"), FolderId: ptr("801")}
	useContentMock(t, m)

	if _, err := executeCommand("dashboard", "mv", "192", "801", "--force"); err != nil {
		t.Fatalf("dashboard mv failed: %v", err)
	}
	if d := m.dashboards["192"]; d.Deleted != nil && *d.Deleted {
		t.Fatal("dashboard mv --force trashed the dashboard being moved")
	}
}

func TestDashboardMvForce_OnlyExactTitleIsAConflict(t *testing.T) {
	m := newContentMockDoer(t)
	m.dashboards["192"] = v4.Dashboard{Id: ptr("192"), Title: ptr("Sales"), FolderId: ptr("700")}
	// Looker's search would return this for title "Sales" (case-insensitive).
	m.dashboards["300"] = v4.Dashboard{Id: ptr("300"), Title: ptr("SALES"), FolderId: ptr("801")}
	useContentMock(t, m)

	if _, err := executeCommand("dashboard", "mv", "192", "801", "--force"); err != nil {
		t.Fatalf("dashboard mv failed: %v", err)
	}
	if d := m.dashboards["300"]; d.Deleted != nil && *d.Deleted {
		t.Error("dashboard with a different title was trashed")
	}
	if d := m.dashboards["192"]; d.FolderId == nil || *d.FolderId != "801" {
		t.Errorf("expected dashboard 192 to be moved to folder 801, got %+v", d)
	}
}

func TestDashboardMv_SearchFailureAborts(t *testing.T) {
	m := newContentMockDoer(t)
	m.dashboards["192"] = v4.Dashboard{Id: ptr("192"), Title: ptr("Sales"), FolderId: ptr("700")}
	m.fail["GET /dashboards/search"] = fmt.Errorf("response error. status=500")
	useContentMock(t, m)

	if _, err := executeCommand("dashboard", "mv", "192", "801", "--force"); err == nil {
		t.Fatal("expected dashboard mv to fail when the conflict search fails")
	}
	if calls := m.mutatingCalls(); len(calls) != 0 {
		t.Errorf("expected no changes after a failed conflict search, got %v", calls)
	}
}

// ---------------------------------------------------------------------------
// dashboard import --force
// ---------------------------------------------------------------------------

const importDashJSON = `{
  "title": "Sales",
  "dashboard_filters": [
    {"name": "region", "title": "Region", "type": "field_filter"}
  ],
  "dashboard_elements": [
    {"id": "1", "title": "Revenue", "type": "vis", "query": {"model": "m", "view": "v", "fields": ["v.revenue"]}},
    {"id": "2", "title": "Orders", "type": "vis", "query": {"model": "m", "view": "v", "fields": ["v.orders"]}}
  ],
  "dashboard_layouts": [
    {"active": true, "type": "newspaper", "dashboard_layout_components": [
      {"dashboard_element_id": "1", "row": 0, "column": 0, "width": 12, "height": 6},
      {"dashboard_element_id": "2", "row": 6, "column": 0, "width": 12, "height": 6}
    ]}
  ]
}`

func seedExistingSalesDashboard(m *contentMockDoer) {
	m.dashboards["500"] = v4.Dashboard{
		Id:       ptr("500"),
		Title:    ptr("Sales"),
		FolderId: ptr("801"),
		DashboardElements: &[]v4.DashboardElement{
			{Id: ptr("old_e1"), Title: ptr("Old Revenue Tile")},
			{Id: ptr("old_e2"), Title: ptr("Old Orders Tile")},
		},
		DashboardFilters: &[]v4.DashboardFilter{
			{Id: ptr("old_f1"), Name: ptr("old_region")},
		},
		DashboardLayouts: &[]v4.DashboardLayout{
			{Id: ptr("layout_500"), Active: ptrBool(true)},
		},
	}
	m.alerts = []v4.Alert{
		{Id: ptr("alert_1"), CustomTitle: ptr("Revenue Drop Alert"), DashboardElementId: ptr("old_e1")},
	}
}

func writeImportFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Dashboard_Sales.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write import file: %v", err)
	}
	return p
}

func TestDashboardImportForce_BacksUpBeforeOverwriting(t *testing.T) {
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	useContentMock(t, m)
	backupDir := filepath.Join(t.TempDir(), "backups")

	out, err := executeCommand("dashboard", "import", writeImportFile(t, importDashJSON), "801", "--force", "--backup-dir", backupDir)
	if err != nil {
		t.Fatalf("dashboard import failed: %v", err)
	}
	if !strings.Contains(out, "Imported dashboard 500") {
		t.Errorf("expected success message, got %q", out)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one backup file in %s, got %v (err %v)", backupDir, entries, err)
	}
	b, err := os.ReadFile(filepath.Join(backupDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	for _, want := range []string{"Old Revenue Tile", "Old Orders Tile", "old_region", "Revenue Drop Alert"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("backup is missing %q from the previous version:\n%s", want, b)
		}
	}

	// The backup must be taken before the first change to the dashboard.
	backupIdx, firstChangeIdx := -1, -1
	for i, c := range m.calls {
		if c == "GET /dashboards/500" && backupIdx == -1 {
			backupIdx = i
		}
		if !strings.HasPrefix(c, "GET ") && firstChangeIdx == -1 {
			firstChangeIdx = i
		}
	}
	if backupIdx == -1 || firstChangeIdx == -1 || backupIdx > firstChangeIdx {
		t.Errorf("expected backup read before any change, calls: %v", m.calls)
	}
}

func TestDashboardImportForce_BackupFailureChangesNothing(t *testing.T) {
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	useContentMock(t, m)

	// A regular file where the backup directory should be makes MkdirAll fail.
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := executeCommand("dashboard", "import", writeImportFile(t, importDashJSON), "801", "--force", "--backup-dir", notADir)
	if err == nil || !strings.Contains(err.Error(), "backing it up failed") {
		t.Fatalf("expected import to abort because the backup failed, got %v", err)
	}
	if calls := m.mutatingCalls(); len(calls) != 0 {
		t.Errorf("expected no changes when the backup fails, got %v", calls)
	}
}

// Regression test: a --force import deleted the dashboard's contents, then
// ignored every failure while recreating them and still printed success.
func TestDashboardImportForce_PartialFailureIsReported(t *testing.T) {
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	m.fail["POST /dashboard_elements"] = fmt.Errorf("response error. status=422 Validation Failed")
	m.fail["POST /dashboard_filters"] = fmt.Errorf("response error. status=422 Validation Failed")
	useContentMock(t, m)
	backupDir := filepath.Join(t.TempDir(), "backups")

	out, err := executeCommand("dashboard", "import", writeImportFile(t, importDashJSON), "801", "--force", "--backup-dir", backupDir)
	if err == nil {
		t.Fatalf("expected a non-nil error after failed element/filter creation; stdout: %q", out)
	}
	if strings.Contains(out, "Imported dashboard") {
		t.Errorf("must not print success after a partial failure, got %q", out)
	}
	msg := err.Error()
	for _, want := range []string{
		"dashboard 500 was imported with 3 error(s)",
		`element 0 ("Revenue"): failed to create element`,
		`element 1 ("Orders"): failed to create element`,
		`failed to create dashboard filter "region"`,
		backupDir,
		"dashboard import",
		"--force",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error is missing %q:\n%s", want, msg)
		}
	}
}

func TestDashboardImportForce_DeleteFailureIsReported(t *testing.T) {
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	m.fail["DELETE /dashboard_elements/old_e2"] = fmt.Errorf("response error. status=500")
	useContentMock(t, m)

	_, err := executeCommand("dashboard", "import", writeImportFile(t, importDashJSON), "801", "--force", "--backup-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "failed to delete old dashboard element old_e2") {
		t.Fatalf("expected the failed delete to be reported, got %v", err)
	}
}

func TestDashboardImport_ElementsWithoutLayoutsAreRejectedUpFront(t *testing.T) {
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	useContentMock(t, m)

	noLayouts := `{"title": "Sales", "dashboard_elements": [{"id": "1", "title": "Revenue", "type": "vis"}]}`
	_, err := executeCommand("dashboard", "import", writeImportFile(t, noLayouts), "801", "--force", "--backup-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no elements would be imported") {
		t.Fatalf("expected the import to be refused, got %v", err)
	}
	if calls := m.mutatingCalls(); len(calls) != 0 {
		t.Errorf("expected no changes for a refused import, got %v", calls)
	}
}

func TestDashboardImport_UnreferencedElementIsReported(t *testing.T) {
	m := newContentMockDoer(t)
	useContentMock(t, m)

	oneComponent := `{
  "title": "New Dash",
  "dashboard_elements": [
    {"id": "1", "title": "Revenue", "type": "vis"},
    {"id": "2", "title": "Orders", "type": "vis"}
  ],
  "dashboard_layouts": [
    {"active": true, "dashboard_layout_components": [{"dashboard_element_id": "1"}]}
  ]
}`
	_, err := executeCommand("dashboard", "import", writeImportFile(t, oneComponent), "801")
	if err == nil || !strings.Contains(err.Error(), `element 1 ("Orders"): not imported because no dashboard layout component references it`) {
		t.Fatalf("expected the unreferenced element to be reported, got %v", err)
	}
}

func setIsolatedCacheDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(k, tmp)
	}
}

func TestDashboardCat_PreservesElementAlertsWithStringIDs(t *testing.T) {
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	useContentMock(t, m)

	out, err := executeCommand("dashboard", "cat", "500", "--trim")
	if err != nil {
		t.Fatalf("dashboard cat failed: %v", err)
	}
	if !strings.Contains(out, "Revenue Drop Alert") {
		t.Errorf("expected dashboard cat --trim to preserve element alerts on string element IDs, got:\n%s", out)
	}
}

func TestDashboardImportForce_DefaultBackupRemovedOnCleanSuccess(t *testing.T) {
	setIsolatedCacheDir(t)
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	useContentMock(t, m)

	if _, err := executeCommand("dashboard", "import", writeImportFile(t, importDashJSON), "801", "--force"); err != nil {
		t.Fatalf("dashboard import failed: %v", err)
	}

	defaultDir, err := defaultDashboardBackupDir()
	if err != nil {
		t.Fatalf("defaultDashboardBackupDir failed: %v", err)
	}
	entries, err := os.ReadDir(defaultDir)
	if err != nil {
		t.Fatalf("expected default backup dir %s to exist, got err: %v", defaultDir, err)
	}
	if len(entries) != 0 {
		t.Errorf("expected temporary default backup to be removed after clean success, found %d file(s): %v", len(entries), entries)
	}
}

func TestDashboardImportForce_DefaultBackupKeptOnPartialFailure(t *testing.T) {
	setIsolatedCacheDir(t)
	m := newContentMockDoer(t)
	seedExistingSalesDashboard(m)
	m.fail["POST /dashboard_elements"] = fmt.Errorf("response error. status=422 Validation Failed")
	useContentMock(t, m)

	_, err := executeCommand("dashboard", "import", writeImportFile(t, importDashJSON), "801", "--force")
	if err == nil {
		t.Fatal("expected dashboard import to fail on element creation error")
	}

	defaultDir, dirErr := defaultDashboardBackupDir()
	if dirErr != nil {
		t.Fatalf("defaultDashboardBackupDir failed: %v", dirErr)
	}
	entries, readErr := os.ReadDir(defaultDir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("expected 1 backup file retained in %s after partial failure, got %v (err %v)", defaultDir, entries, readErr)
	}
	backupFile := filepath.Join(defaultDir, entries[0].Name())
	if !strings.Contains(err.Error(), backupFile) {
		t.Errorf("expected error to mention retained backup %s, got:\n%s", backupFile, err.Error())
	}
}
