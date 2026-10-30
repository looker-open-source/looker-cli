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
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

type funcDoer func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error

func (f funcDoer) Do(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
	return f(result, method, ver, path, reqPars, body, options)
}

func captureStdStreams(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stdout pipe: %v", pipeErr)
	}
	rErr, wErr, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stderr pipe: %v", pipeErr)
	}
	os.Stdout = wOut
	os.Stderr = wErr

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, rOut)
		outCh <- b.String()
	}()
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, rErr)
		errCh <- b.String()
	}()

	err = fn()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	return <-outCh, <-errCh, err
}

func TestRootCmd_SilencesDuplicateErrorsAndUsage(t *testing.T) {
	if !RootCmd.SilenceUsage {
		t.Errorf("expected RootCmd.SilenceUsage = true")
	}
	if !RootCmd.SilenceErrors {
		t.Errorf("expected RootCmd.SilenceErrors = true")
	}

	var cobraOut, cobraErr bytes.Buffer
	RootCmd.SetOut(&cobraOut)
	RootCmd.SetErr(&cobraErr)
	t.Cleanup(func() {
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
	})

	RootCmd.SetArgs([]string{"query", "runquery"})
	err := RootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing query argument, got nil")
	}
	if strings.Contains(cobraOut.String(), "Usage:") || strings.Contains(cobraErr.String(), "Usage:") {
		t.Errorf("expected no automatic Usage dump on runtime error, got out=%q err=%q", cobraOut.String(), cobraErr.String())
	}
	if cobraErr.Len() != 0 {
		t.Errorf("expected Cobra not to print error before Execute() handles it, got %q", cobraErr.String())
	}
}

func TestCatDir_FailsOnUnwritableDirWithoutClaimingWrote(t *testing.T) {
	baseDoer := &mockDoer{t: t}
	MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		switch {
		case method == "GET" && path == "/alerts/42":
			return respond(result, v4.Alert{Id: ptr("42"), CustomTitle: ptr("My Alert"), Cron: "0 9 * * *"})
		case method == "GET" && path == "/scheduled_plans/77":
			return respond(result, v4.ScheduledPlan{Id: ptr("77"), Name: ptr("My Plan")})
		case method == "GET" && path == "/projects/my_proj":
			return respond(result, v4.Project{Id: ptr("my_proj"), Name: ptr("my_proj")})
		default:
			return baseDoer.Do(result, method, ver, path, reqPars, body, options)
		}
	}))
	t.Cleanup(func() {
		MockSDK = nil
		alertCatDir = ""
		connectionCatDir = ""
		dashboardCatDir = ""
		folderCatDir = ""
		lookCatDir = ""
		modelCatDir = ""
		modelSetCatDir = ""
		permissionSetCatDir = ""
		planCatDir = ""
		projectCatDir = ""
	})

	missingDir := filepath.Join(t.TempDir(), "does-not-exist")
	cases := [][]string{
		{"alert", "cat", "42", "--dir", missingDir},
		{"connection", "cat", "my_conn", "--dir", missingDir},
		{"dashboard", "cat", "192", "--dir", missingDir},
		{"folder", "cat", "709", "--dir", missingDir},
		{"look", "cat", "857", "--dir", missingDir},
		{"model", "cat", "my_model", "--dir", missingDir},
		{"model", "set", "cat", "5", "--dir", missingDir},
		{"permission", "set", "cat", "3", "--dir", missingDir},
		{"plan", "cat", "77", "--dir", missingDir},
		{"project", "cat", "my_proj", "--dir", missingDir},
	}

	for _, args := range cases {
		t.Run(strings.Join(args[:3], "_"), func(t *testing.T) {
			stdout, _, err := captureStdStreams(t, func() error {
				RootCmd.SetArgs(args)
				return RootCmd.Execute()
			})
			if err == nil {
				t.Fatalf("expected error writing to nonexistent dir for %v, got nil", args)
			}
			if strings.Contains(stdout, "Wrote ") {
				t.Fatalf("expected no 'Wrote ' message on failed write, got stdout=%q", stdout)
			}
		})
	}
}

func TestQueryRun_DoesNotTruncateExistingOutputFileOnFailure(t *testing.T) {
	queryRunInputFile = ""
	queryRunOutputFile = ""
	queryRunFormat = "json"
	t.Cleanup(func() {
		MockSDK = nil
		queryRunInputFile = ""
		queryRunOutputFile = ""
		queryRunFormat = "json"
	})

	outPath := filepath.Join(t.TempDir(), "existing_results.json")
	const original = `{"preserved":true}`
	if err := os.WriteFile(outPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write original output file: %v", err)
	}

	failNext := true
	MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		if failNext {
			return errors.New("backend query timeout")
		}
		if target, ok := result.(*string); ok {
			*target = `[{"orders.count":10}]`
			return nil
		}
		return respond(result, `[{"orders.count":10}]`)
	}))

	stdout, _, err := captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"query", "runquery", "123", "--output", outPath})
		return RootCmd.Execute()
	})
	if err == nil {
		t.Fatal("expected error when RunQuery fails, got nil")
	}
	if strings.Contains(stdout, "Wrote ") {
		t.Errorf("unexpected 'Wrote' on failed query: %q", stdout)
	}

	gotBytes, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("read output file after failed query: %v", readErr)
	}
	if string(gotBytes) != original {
		t.Fatalf("existing output file was clobbered on failed query: got %q, want %q", string(gotBytes), original)
	}

	// Succeeding run atomically replaces the file.
	failNext = false
	stdout, _, err = captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"query", "runquery", "123", "--output", outPath})
		return RootCmd.Execute()
	})
	if err != nil {
		t.Fatalf("expected successful query run, got: %v", err)
	}
	if !strings.Contains(stdout, "Wrote ") {
		t.Errorf("expected 'Wrote' message on stdout, got %q", stdout)
	}
	gotBytes, readErr = os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("read output file after successful query: %v", readErr)
	}
	if string(gotBytes) != `[{"orders.count":10}]` {
		t.Fatalf("unexpected output file contents: %q", string(gotBytes))
	}
}

func TestFolderExport_ArchivesAndErrorPropagation(t *testing.T) {
	resetExportFlags := func() {
		folderExportDir = ""
		folderExportZip = ""
		folderExportTar = ""
		folderExportTgz = ""
		folderExportTrim = false
	}
	t.Cleanup(func() {
		MockSDK = nil
		resetExportFlags()
	})

	t.Run("ValidZipAndTgzArchives", func(t *testing.T) {
		resetExportFlags()
		MockSDK = v4.NewLookerSDK(&mockDoer{t: t})

		zipPath := filepath.Join(t.TempDir(), "export.zip")
		_, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "709", "--zip", zipPath})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("zip export failed: %v", err)
		}

		zr, err := zip.OpenReader(zipPath)
		if err != nil {
			t.Fatalf("open zip: %v", err)
		}
		defer func() { _ = zr.Close() }()
		names := make(map[string]bool)
		for _, f := range zr.File {
			names[f.Name] = true
		}
		for _, want := range []string{
			"Folder_709_Shared/Folder_709_Shared.json",
			"Folder_709_Shared/Look_857_Daily Profit.json",
			"Folder_709_Shared/Dashboard_192_Daily Profit Dashboard.json",
			"Folder_709_Shared/Folder_801_SubShared/Folder_801_SubShared.json",
		} {
			if !names[want] {
				t.Errorf("missing %q in zip entries %v", want, names)
			}
		}

		resetExportFlags()
		tgzPath := filepath.Join(t.TempDir(), "export.tgz")
		_, _, err = captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "709", "--tgz", tgzPath})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("tgz export failed: %v", err)
		}

		f, err := os.Open(tgzPath)
		if err != nil {
			t.Fatalf("open tgz: %v", err)
		}
		defer func() { _ = f.Close() }()
		gzr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		defer func() { _ = gzr.Close() }()
		tr := tar.NewReader(gzr)
		tarNames := make(map[string]bool)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("tar next: %v", err)
			}
			tarNames[hdr.Name] = true
		}
		if !tarNames["Folder_709_Shared/Look_857_Daily Profit.json"] {
			t.Errorf("missing Look_857_Daily Profit.json in tgz entries %v", tarNames)
		}
	})

	t.Run("BrokenLookOrSubfolderFailsExport", func(t *testing.T) {
		resetExportFlags()
		baseDoer := &mockDoer{t: t}
		MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
			if method == "GET" && path == "/looks/857" {
				return errors.New("look 857 permission denied")
			}
			return baseDoer.Do(result, method, ver, path, reqPars, body, options)
		}))

		outDir := t.TempDir()
		_, stderr, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "709", "--dir", outDir})
			return RootCmd.Execute()
		})
		if err == nil || !strings.Contains(err.Error(), "857") {
			t.Fatalf("expected look 857 export error, got %v", err)
		}
		if !strings.Contains(stderr, "857") {
			t.Errorf("expected look 857 diagnostic on stderr, got %q", stderr)
		}

		resetExportFlags()
		zipPath := filepath.Join(t.TempDir(), "incomplete.zip")
		stdout, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "709", "--zip", zipPath})
			return RootCmd.Execute()
		})
		if err == nil {
			t.Fatal("expected zip export to fail when a look fails to export")
		}
		if strings.Contains(stdout, "Wrote zip") {
			t.Errorf("unexpected 'Wrote zip' on failed archive export: %q", stdout)
		}
		if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
			t.Errorf("expected incomplete zip file not to be written, statErr=%v", statErr)
		}
	})
}

func TestConnectionTest_ExitsNonZeroWhenCheckFails(t *testing.T) {
	MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		if method == "PUT" && path == "/connections/broken_conn/test" {
			return respond(result, []v4.DBConnectionTestResult{
				{Name: ptr("connect"), Status: ptr("success"), Message: ptr("tcp ok")},
				{Name: ptr("query"), Status: ptr("error"), Message: ptr("authentication failed")},
			})
		}
		return fmt.Errorf("unexpected %s %s", method, path)
	}))
	t.Cleanup(func() {
		MockSDK = nil
		connectionTestPlain = false
		connectionTestCSV = false
	})

	stdout, _, err := captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"connection", "test", "broken_conn", "--plain"})
		return RootCmd.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "1 connection test(s) failed") {
		t.Fatalf("expected non-zero error for failed connection test, got %v", err)
	}
	if !strings.Contains(stdout, "authentication failed") {
		t.Errorf("expected rendered test results before returning error, got %q", stdout)
	}
}

func TestRandomizeCron_AndRandomizeCommandErrors(t *testing.T) {
	// Restricted day-of-week must stay on the same calendar day (00:00..23:59)
	// rather than wrapping 00:05 Monday to 23:xx Monday night.
	for i := 0; i < 200; i++ {
		got := randomizeCron("5 0 * * 1", 60)
		fields := strings.Fields(got)
		if len(fields) != 5 {
			t.Fatalf("unexpected cron fields: %q", got)
		}
		min, err1 := strconv.Atoi(fields[0])
		hour, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			t.Fatalf("invalid cron numbers in %q", got)
		}
		if hour != 0 || min < 0 || min > 35 {
			t.Fatalf("restricted day-of-week cron wrapped unexpectedly: %q", got)
		}
	}

	// Daily unrestricted cron can wrap across midnight cleanly within 0..23 / 0..59.
	for i := 0; i < 200; i++ {
		got := randomizeCron("5 0 * * *", 60)
		fields := strings.Fields(got)
		min, _ := strconv.Atoi(fields[0])
		hour, _ := strconv.Atoi(fields[1])
		if min < 0 || min > 59 || hour < 0 || hour > 23 {
			t.Fatalf("out-of-range cron generated: %q", got)
		}
	}

	t.Cleanup(func() {
		MockSDK = nil
		alertRandWindow = 60
		alertRandAll = false
		planRandWindow = 60
		planRandAll = false
	})

	MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		switch {
		case method == "GET" && path == "/alerts/a1":
			return respond(result, v4.Alert{Id: ptr("a1"), Cron: "15 10 * * *"})
		case method == "PUT" && path == "/alerts/a1":
			return errors.New("alert update forbidden")
		case method == "GET" && path == "/scheduled_plans/p1":
			return respond(result, v4.ScheduledPlan{Id: ptr("p1"), Crontab: ptr("15 10 * * *")})
		case method == "PATCH" && path == "/scheduled_plans/p1":
			return errors.New("plan update forbidden")
		default:
			return fmt.Errorf("unexpected %s %s", method, path)
		}
	}))

	stdout, stderr, err := captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"alert", "randomize", "a1"})
		return RootCmd.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "alert update forbidden") {
		t.Fatalf("expected alert randomize error, got %v", err)
	}
	if strings.Contains(stdout, "Randomized alert") {
		t.Errorf("should not claim alert was randomized on failure: %q", stdout)
	}
	if !strings.Contains(stderr, "a1") {
		t.Errorf("expected failure diagnostic on stderr, got %q", stderr)
	}

	stdout, stderr, err = captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"plan", "randomize", "p1"})
		return RootCmd.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "plan update forbidden") {
		t.Fatalf("expected plan randomize error, got %v", err)
	}
	if strings.Contains(stdout, "Randomized plan") {
		t.Errorf("should not claim plan was randomized on failure: %q", stdout)
	}
	if !strings.Contains(stderr, "p1") {
		t.Errorf("expected failure diagnostic on stderr, got %q", stderr)
	}
}

func TestPlanAndLookImport_PropagateErrorsAndPreserveStringDashboardID(t *testing.T) {
	t.Cleanup(func() {
		MockSDK = nil
		planImportPlain = false
		lookImportForce = false
		lookImportPlain = false
	})

	planFile := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(planFile, []byte(`{"name":"WeeklyExec","crontab":"0 8 * * 1"}`), 0o600); err != nil {
		t.Fatalf("write plan file: %v", err)
	}

	var seenDashLookupPath string
	var updatedPlanID string
	MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		switch {
		case method == "GET" && strings.HasPrefix(path, "/user"):
			return respond(result, v4.User{Id: ptr("1234")})
		case method == "GET" && strings.HasPrefix(path, "/scheduled_plans/dashboard/"):
			seenDashLookupPath = path
			return respond(result, []v4.ScheduledPlan{
				{Id: ptr("plan_existing_9"), Name: ptr("WeeklyExec"), UserId: ptr("1234")},
			})
		case method == "PATCH" && strings.HasPrefix(path, "/scheduled_plans/"):
			updatedPlanID = strings.TrimPrefix(path, "/scheduled_plans/")
			return respond(result, v4.ScheduledPlan{Id: &updatedPlanID})
		default:
			return fmt.Errorf("unexpected %s %s", method, path)
		}
	}))

	_, _, err := captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"plan", "import", planFile, "dashboard", "thelook::exec_overview", "--plain"})
		return RootCmd.Execute()
	})
	if err != nil {
		t.Fatalf("plan import for LookML dashboard failed: %v", err)
	}
	if !strings.Contains(seenDashLookupPath, "exec_overview") {
		t.Errorf("expected dashboard ID 'thelook::exec_overview' in lookup path, got %q", seenDashLookupPath)
	}
	if updatedPlanID != "plan_existing_9" {
		t.Errorf("expected existing plan_existing_9 to be updated, got %q", updatedPlanID)
	}

	// look import: FolderLooks failure aborts before creating an orphaned query,
	// and CreateScheduledPlan failure surfaces non-zero error.
	lookFile := filepath.Join(t.TempDir(), "look.json")
	lookPayload := `{
		"title": "Revenue Look",
		"query": {"model": "thelook", "view": "orders", "fields": ["orders.count"]},
		"scheduled_plans": [{"name": "Morning Send", "crontab": "0 7 * * *"}]
	}`
	if err := os.WriteFile(lookFile, []byte(lookPayload), 0o600); err != nil {
		t.Fatalf("write look file: %v", err)
	}

	createdQueries := 0
	failFolderLooks := true
	MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		switch {
		case method == "GET" && strings.HasPrefix(path, "/user"):
			return respond(result, v4.User{Id: ptr("1234")})
		case method == "GET" && path == "/folders/801/looks":
			if failFolderLooks {
				return errors.New("folder permission error")
			}
			return respond(result, []v4.LookWithQuery{})
		case method == "POST" && path == "/queries":
			createdQueries++
			return respond(result, v4.Query{Id: ptr("q_1")})
		case method == "POST" && path == "/looks":
			return respond(result, v4.LookWithQuery{Id: ptr("l_1"), Title: ptr("Revenue Look")})
		case method == "GET" && strings.HasPrefix(path, "/scheduled_plans/look/"):
			return respond(result, []v4.ScheduledPlan{})
		case method == "POST" && path == "/scheduled_plans":
			return errors.New("invalid destination address")
		default:
			return fmt.Errorf("unexpected %s %s", method, path)
		}
	}))

	_, _, err = captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"look", "import", lookFile, "801"})
		return RootCmd.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "folder permission error") {
		t.Fatalf("expected FolderLooks error from look import, got %v", err)
	}
	if createdQueries != 0 {
		t.Errorf("expected 0 queries created when FolderLooks fails, got %d", createdQueries)
	}

	failFolderLooks = false
	_, _, err = captureStdStreams(t, func() error {
		RootCmd.SetArgs([]string{"look", "import", lookFile, "801"})
		return RootCmd.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "invalid destination address") {
		t.Fatalf("expected scheduled plan failure to fail look import, got %v", err)
	}
}

func TestHealthCommands_SeparateStderrAndFailOnQueryErrors(t *testing.T) {
	t.Cleanup(func() {
		MockSDK = nil
		vacuumModel = ""
		vacuumProject = ""
		vacuumExplore = ""
		vacuumPlain = false
		vacuumCSV = false
		healthPulsePlain = false
		healthPulseCSV = false
	})

	t.Run("VacuumModelsNeverFlagsExploreUnusedWhenQueryErrors", func(t *testing.T) {
		inlineRes := `{"looker_error":"model compilation error"}`
		MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
			switch {
			case method == "POST" && strings.HasPrefix(path, "/queries/run/"):
				var wq v4.WriteQuery
				decodeBody(body, &wq)
				if wq.Filters != nil {
					if _, perExplore := (*wq.Filters)["query.view"]; perExplore {
						if target, ok := result.(*string); ok {
							*target = inlineRes
							return nil
						}
					}
				}
				if target, ok := result.(*string); ok {
					*target = `[{"query.model":"sales","history.query_run_count":12}]`
					return nil
				}
				return nil
			case method == "GET" && path == "/lookml_models":
				return respond(result, []v4.LookmlModel{
					{Name: ptr("sales"), ProjectName: ptr("proj_a")},
				})
			case method == "GET" && path == "/lookml_models/sales":
				return respond(result, v4.LookmlModel{
					Name: ptr("sales"),
					Explores: &[]v4.LookmlModelNavExplore{
						{Name: ptr("orders")},
					},
				})
			default:
				return fmt.Errorf("unexpected %s %s", method, path)
			}
		}))

		stdout, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"health", "vacuum", "models", "--csv"})
			return RootCmd.Execute()
		})
		if err == nil {
			t.Fatal("expected error when explore usage query returns non-array error JSON, got nil")
		}
		if strings.Contains(stdout, "orders") {
			t.Fatalf("explore 'orders' was falsely reported as unused on query error: %q", stdout)
		}
	})

	t.Run("VacuumExploresSortsJoinsAndSeparatesStderr", func(t *testing.T) {
		MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
			switch {
			case method == "GET" && path == "/lookml_models":
				return respond(result, []v4.LookmlModel{
					{
						Name: ptr("sales"),
						Explores: &[]v4.LookmlModelNavExplore{
							{Name: ptr("good_explore")},
							{Name: ptr("broken_explore")},
						},
					},
				})
			case method == "GET" && path == "/lookml_models/sales/explores/good_explore":
				return respond(result, v4.LookmlModelExplore{
					Name: ptr("good_explore"),
					Joins: &[]v4.LookmlModelExploreJoins{
						{Name: ptr("zeta_join")},
						{Name: ptr("alpha_join")},
					},
					Fields: &v4.LookmlModelExploreFieldset{
						Dimensions: &[]v4.LookmlModelExploreField{
							{Name: ptr("orders.id"), Hidden: ptrBool(false)},
						},
					},
				})
			case method == "GET" && path == "/lookml_models/sales/explores/broken_explore":
				return errors.New("explore LookML broken")
			case method == "POST" && strings.HasPrefix(path, "/queries/run/"):
				if target, ok := result.(*string); ok {
					*target = `[{"query.formatted_fields":"[\"orders.id\"]","history.query_run_count":5}]`
					return nil
				}
				return nil
			default:
				return fmt.Errorf("unexpected %s %s", method, path)
			}
		}))

		stdout, stderr, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"health", "vacuum", "explores", "--csv"})
			return RootCmd.Execute()
		})
		if err == nil || !strings.Contains(err.Error(), "broken_explore") {
			t.Fatalf("expected partial explore failure error, got %v", err)
		}
		if !strings.Contains(stderr, "broken_explore") {
			t.Errorf("expected broken_explore diagnostic on stderr, got %q", stderr)
		}

		records, parseErr := csv.NewReader(strings.NewReader(stdout)).ReadAll()
		if parseErr != nil {
			t.Fatalf("stdout was not clean CSV (%v):\n%s", parseErr, stdout)
		}
		if len(records) != 2 || records[1][2] != "alpha_join, zeta_join" {
			t.Fatalf("expected deterministic sorted unused joins 'alpha_join, zeta_join', got records=%v", records)
		}
	})

	t.Run("PulseCSVWritesBannersToStderrAndFailsOnCheckError", func(t *testing.T) {
		MockSDK = v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
			switch {
			case method == "GET" && path == "/connections":
				return respond(result, []v4.DBConnection{})
			case method == "POST" && strings.HasPrefix(path, "/queries/run/"):
				if target, ok := result.(*string); ok {
					*target = `[{"dashboard.title":"Exec","query.count":2,"history.query_run_count":1,"query.model":"m","query.view":"v","history.average_runtime":3.5,"scheduled_job.name":"JobA","scheduled_job.count":1}]`
					return nil
				}
				return nil
			case method == "GET" && path == "/legacy_features":
				return errors.New("legacy endpoint failure")
			default:
				return fmt.Errorf("unexpected %s %s", method, path)
			}
		}))

		stdout, stderr, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"health", "pulse", "--csv"})
			return RootCmd.Execute()
		})
		if err == nil || !strings.Contains(err.Error(), "legacy-features") {
			t.Fatalf("expected pulse to return error when legacy-features check fails, got %v", err)
		}
		if strings.Contains(stdout, "Running all health pulse checks") || strings.Contains(stdout, "Test 1/6") || strings.Contains(stdout, "For context") {
			t.Errorf("expected progress banners and context off stdout, got stdout=%q", stdout)
		}
		if !strings.Contains(stderr, "Running all health pulse checks") || !strings.Contains(stderr, "Error checking legacy features") {
			t.Errorf("expected progress banners and error summary on stderr, got stderr=%q", stderr)
		}
	})
}
