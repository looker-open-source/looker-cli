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
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

func newFolderExportExcludeMock(t *testing.T) *v4.LookerSDK {
	t.Helper()
	marshalInto := func(result interface{}, v interface{}) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, result)
	}

	return v4.NewLookerSDK(funcDoer(func(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
		switch {
		case method == "GET" && (path == "/folders/100" || path == "/folders/shared"):
			return marshalInto(result, v4.Folder{
				Id:   ptr("100"),
				Name: "RootX",
				Looks: &[]v4.LookWithDashboards{
					{Id: ptr("10"), Title: ptr("Root Look")},
				},
				Dashboards: &[]v4.DashboardBase{
					{Id: ptr("20"), Title: ptr("Root Dash")},
				},
			})
		case method == "GET" && (path == "/folders/100/children" || path == "/folders/shared/children"):
			return marshalInto(result, []v4.Folder{
				{Id: ptr("101"), Name: "KeepSub", ParentId: ptr("100")},
				{Id: ptr("102"), Name: "SkipY", ParentId: ptr("100")},
				{Id: ptr("103"), Name: "SkipZ", ParentId: ptr("100")},
			})
		case method == "GET" && path == "/folders/101":
			return marshalInto(result, v4.Folder{
				Id:   ptr("101"),
				Name: "KeepSub",
				Looks: &[]v4.LookWithDashboards{
					{Id: ptr("11"), Title: ptr("Keep Look")},
				},
			})
		case method == "GET" && path == "/folders/101/children":
			return marshalInto(result, []v4.Folder{
				{Id: ptr("201"), Name: "KeepNested", ParentId: ptr("101")},
				{Id: ptr("202"), Name: "SkipNested", ParentId: ptr("101")},
			})
		case method == "GET" && path == "/folders/201":
			return marshalInto(result, v4.Folder{
				Id:   ptr("201"),
				Name: "KeepNested",
				Dashboards: &[]v4.DashboardBase{
					{Id: ptr("21"), Title: ptr("Nested Dash")},
				},
			})
		case method == "GET" && path == "/folders/201/children":
			return marshalInto(result, []v4.Folder{})
		case method == "GET" && path == "/looks/10":
			return marshalInto(result, v4.LookWithQuery{Id: ptr("10"), Title: ptr("Root Look")})
		case method == "GET" && path == "/looks/11":
			return marshalInto(result, v4.LookWithQuery{Id: ptr("11"), Title: ptr("Keep Look")})
		case method == "GET" && path == "/dashboards/20":
			return marshalInto(result, v4.Dashboard{Id: ptr("20"), Title: ptr("Root Dash")})
		case method == "GET" && path == "/dashboards/21":
			return marshalInto(result, v4.Dashboard{Id: ptr("21"), Title: ptr("Nested Dash")})
		case method == "GET" && path == "/alerts/search":
			return marshalInto(result, []v4.Alert{})
		case strings.HasPrefix(path, "/folders/102"),
			strings.HasPrefix(path, "/folders/103"),
			strings.HasPrefix(path, "/folders/202"):
			t.Errorf("unexpected API call for excluded folder path: %s %s", method, path)
			return fmt.Errorf("excluded folder %s should not have been fetched", path)
		default:
			return fmt.Errorf("unexpected mock request: %s %s", method, path)
		}
	}))
}

func TestFolderExport_ExcludeFilter(t *testing.T) {
	resetFlags := func() {
		folderExportDir = ""
		folderExportZip = ""
		folderExportTar = ""
		folderExportTgz = ""
		folderExportTrim = false
		folderExportExclude = nil
	}
	t.Cleanup(func() {
		MockSDK = nil
		resetFlags()
	})

	wantRelativeFiles := []string{
		filepath.Join("Folder_100_RootX", "Folder_100_RootX.json"),
		filepath.Join("Folder_100_RootX", "Look_10_Root Look.json"),
		filepath.Join("Folder_100_RootX", "Dashboard_20_Root Dash.json"),
		filepath.Join("Folder_100_RootX", "Folder_101_KeepSub", "Folder_101_KeepSub.json"),
		filepath.Join("Folder_100_RootX", "Folder_101_KeepSub", "Look_11_Keep Look.json"),
		filepath.Join("Folder_100_RootX", "Folder_101_KeepSub", "Folder_201_KeepNested", "Folder_201_KeepNested.json"),
		filepath.Join("Folder_100_RootX", "Folder_101_KeepSub", "Folder_201_KeepNested", "Dashboard_21_Nested Dash.json"),
	}
	forbiddenDirs := []string{
		filepath.Join("Folder_100_RootX", "Folder_102_SkipY"),
		filepath.Join("Folder_100_RootX", "Folder_103_SkipZ"),
		filepath.Join("Folder_100_RootX", "Folder_101_KeepSub", "Folder_202_SkipNested"),
	}

	t.Run("DirExport_CommaSeparatedExclude", func(t *testing.T) {
		resetFlags()
		MockSDK = newFolderExportExcludeMock(t)
		outDir := t.TempDir()

		_, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "100", "--dir", outDir, "--exclude", "102,103,202"})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("folder export with --exclude failed: %v", err)
		}

		for _, rel := range wantRelativeFiles {
			full := filepath.Join(outDir, rel)
			if _, statErr := os.Stat(full); statErr != nil {
				t.Errorf("expected exported file %s to exist: %v", rel, statErr)
			}
		}
		for _, rel := range forbiddenDirs {
			full := filepath.Join(outDir, rel)
			if _, statErr := os.Stat(full); !os.IsNotExist(statErr) {
				t.Errorf("expected excluded folder %s not to exist, got err=%v", rel, statErr)
			}
		}
	})

	t.Run("DirExport_RepeatedFlagsAndWhitespaceViaSpaceAlias", func(t *testing.T) {
		resetFlags()
		MockSDK = newFolderExportExcludeMock(t)
		outDir := t.TempDir()

		_, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{
				"space", "export", "100",
				"--dir", outDir,
				"--exclude", "102, 103",
				"--exclude", " 202 ",
			})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("space export with repeated --exclude failed: %v", err)
		}

		for _, rel := range wantRelativeFiles {
			full := filepath.Join(outDir, rel)
			if _, statErr := os.Stat(full); statErr != nil {
				t.Errorf("expected exported file %s to exist: %v", rel, statErr)
			}
		}
		for _, rel := range forbiddenDirs {
			full := filepath.Join(outDir, rel)
			if _, statErr := os.Stat(full); !os.IsNotExist(statErr) {
				t.Errorf("expected excluded folder %s not to exist, got err=%v", rel, statErr)
			}
		}
	})

	wantArchiveEntries := []string{
		"Folder_100_RootX/Folder_100_RootX.json",
		"Folder_100_RootX/Look_10_Root Look.json",
		"Folder_100_RootX/Dashboard_20_Root Dash.json",
		"Folder_100_RootX/Folder_101_KeepSub/Folder_101_KeepSub.json",
		"Folder_100_RootX/Folder_101_KeepSub/Look_11_Keep Look.json",
		"Folder_100_RootX/Folder_101_KeepSub/Folder_201_KeepNested/Folder_201_KeepNested.json",
		"Folder_100_RootX/Folder_101_KeepSub/Folder_201_KeepNested/Dashboard_21_Nested Dash.json",
	}
	assertArchiveEntries := func(t *testing.T, got map[string]bool) {
		t.Helper()
		for _, want := range wantArchiveEntries {
			if !got[want] {
				t.Errorf("missing expected archive entry %q in %v", want, got)
			}
		}
		for name := range got {
			if strings.Contains(name, "Folder_102_") || strings.Contains(name, "Folder_103_") || strings.Contains(name, "Folder_202_") {
				t.Errorf("unexpected excluded archive entry %q", name)
			}
		}
	}

	t.Run("ZipExport_ExcludeSubfolders", func(t *testing.T) {
		resetFlags()
		MockSDK = newFolderExportExcludeMock(t)
		zipPath := filepath.Join(t.TempDir(), "export.zip")

		_, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "100", "--zip", zipPath, "--exclude", "102,103,202"})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("zip export with --exclude failed: %v", err)
		}

		zr, err := zip.OpenReader(zipPath)
		if err != nil {
			t.Fatalf("open zip: %v", err)
		}
		defer func() { _ = zr.Close() }()

		entries := make(map[string]bool)
		for _, f := range zr.File {
			entries[f.Name] = true
		}
		assertArchiveEntries(t, entries)
	})

	t.Run("TarAndTgzExport_ExcludeSubfolders", func(t *testing.T) {
		resetFlags()
		MockSDK = newFolderExportExcludeMock(t)
		tarPath := filepath.Join(t.TempDir(), "export.tar")

		_, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "100", "--tar", tarPath, "--exclude", "102,103", "--exclude", "202"})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("tar export with --exclude failed: %v", err)
		}

		tf, err := os.Open(tarPath)
		if err != nil {
			t.Fatalf("open tar: %v", err)
		}
		defer func() { _ = tf.Close() }()

		tr := tar.NewReader(tf)
		tarEntries := make(map[string]bool)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("tar next: %v", err)
			}
			tarEntries[hdr.Name] = true
		}
		assertArchiveEntries(t, tarEntries)

		resetFlags()
		tgzPath := filepath.Join(t.TempDir(), "export.tgz")
		_, _, err = captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "100", "--tgz", tgzPath, "--exclude", "102,103,202"})
			return RootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("tgz export with --exclude failed: %v", err)
		}

		gzf, err := os.Open(tgzPath)
		if err != nil {
			t.Fatalf("open tgz: %v", err)
		}
		defer func() { _ = gzf.Close() }()
		gzr, err := gzip.NewReader(gzf)
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		defer func() { _ = gzr.Close() }()

		tgzTr := tar.NewReader(gzr)
		tgzEntries := make(map[string]bool)
		for {
			hdr, err := tgzTr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("tgz tar next: %v", err)
			}
			tgzEntries[hdr.Name] = true
		}
		assertArchiveEntries(t, tgzEntries)
	})

	t.Run("RejectsExcludingRootFolder", func(t *testing.T) {
		resetFlags()
		MockSDK = newFolderExportExcludeMock(t)
		zipPath := filepath.Join(t.TempDir(), "should_not_exist.zip")

		stdout, _, err := captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "100", "--zip", zipPath, "--exclude", "100,102"})
			return RootCmd.Execute()
		})
		if err == nil || !strings.Contains(err.Error(), "cannot exclude root folder") {
			t.Fatalf("expected root folder exclusion error, got %v", err)
		}
		if strings.Contains(stdout, "Wrote") {
			t.Errorf("unexpected Wrote message on failed root exclusion: %q", stdout)
		}
		if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
			t.Errorf("expected zip file not to be created on error, statErr=%v", statErr)
		}

		// Also reject when root is addressed by alias ("shared") whose resolved Folder.Id ("100") is in --exclude.
		resetFlags()
		outDir := t.TempDir()
		_, _, err = captureStdStreams(t, func() error {
			RootCmd.SetArgs([]string{"folder", "export", "shared", "--dir", outDir, "--exclude", "100"})
			return RootCmd.Execute()
		})
		if err == nil || !strings.Contains(err.Error(), "cannot exclude root folder") {
			t.Fatalf("expected root folder exclusion error for resolved folder ID, got %v", err)
		}
	})
}
