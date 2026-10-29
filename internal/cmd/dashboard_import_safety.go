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
	"time"

	"github.com/looker-open-source/looker-cli/internal/client"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

// findDashboardTitleConflict returns a live dashboard in folderID whose title
// is exactly title, skipping selfID so a dashboard never conflicts with
// itself. Looker's title search is case-insensitive and treats _ and % as
// wildcards, so results are filtered to exact matches. A search failure is
// returned as an error rather than treated as "no conflict".
func findDashboardTitleConflict(c *client.ClientWrapper, folderID, title, selfID string) (*v4.Dashboard, error) {
	if title == "" {
		return nil, nil
	}
	matches, err := c.SDK.SearchDashboards(v4.RequestSearchDashboards{Title: &title, FolderId: &folderID}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to search dashboards in folder %s: %w", folderID, err)
	}
	for i := range matches {
		d := matches[i]
		if d.Id == nil || *d.Id == selfID || d.Title == nil || *d.Title != title {
			continue
		}
		if d.Deleted != nil && *d.Deleted {
			continue
		}
		return &d, nil
	}
	return nil, nil
}

// importErrors collects failures that happen after a dashboard import has
// started changing the server. The import keeps going so the dashboard ends
// up as complete as possible, then the command fails with all of them.
type importErrors struct {
	errs []error
}

func (e *importErrors) addf(format string, args ...any) {
	e.errs = append(e.errs, fmt.Errorf(format, args...))
}

func (e *importErrors) empty() bool { return len(e.errs) == 0 }

// elementLabel names a dashboard element from an import file in messages.
func elementLabel(em map[string]interface{}, idx int) string {
	for _, key := range []string{"title", "title_text"} {
		if v, ok := em[key].(string); ok && v != "" {
			return fmt.Sprintf("element %d (%q)", idx, v)
		}
	}
	return fmt.Sprintf("element %d", idx)
}

// summary builds the final command error. backupPath is the pre-import backup
// of an overwritten dashboard, if one was made.
func (e *importErrors) summary(dashID, folderID, backupPath string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "dashboard %s was imported with %d error(s); it is probably incomplete:", dashID, len(e.errs))
	for _, err := range e.errs {
		fmt.Fprintf(&b, "\n  - %v", err)
	}
	if backupPath != "" {
		fmt.Fprintf(&b, "\nThe dashboard as it was before this import is saved at %s\nTo restore it, run: looker-cli dashboard import %q %s --force", backupPath, backupPath, folderID)
	}
	return fmt.Errorf("%s", b.String())
}

// defaultDashboardBackupDir is where `dashboard import --force` saves the
// previous version of a dashboard before overwriting it.
func defaultDashboardBackupDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "looker-cli", "backups"), nil
}

// backupDashboard exports dashboard dashID in the same format `dashboard cat`
// produces (which `dashboard import` accepts) and writes it to dir. It
// returns the path of the backup file.
func backupDashboard(c *client.ClientWrapper, dashID, dir string) (string, error) {
	if dir == "" {
		d, err := defaultDashboardBackupDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine backup directory (use --backup-dir): %w", err)
		}
		dir = d
	}
	// Scheduled plans are not included: import never deletes them, and
	// restoring other users' plans would recreate them as the importer's.
	data, err := CatDashboardHelper(c, dashID, false, false, "")
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to encode backup: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create backup directory %s: %w", dir, err)
	}
	name := fmt.Sprintf("Dashboard_%s_%s.json", sanitizeFilename(dashID), time.Now().UTC().Format("20060102T150405.000000000Z"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("failed to write backup %s: %w", path, err)
	}
	return path, nil
}

// preflightDashboardImport rejects import files that would silently lose
// content, before anything on the server is changed. Elements are only
// created through the layout components that place them, so a file with
// elements but no layout components would import (or, with --force,
// overwrite the dashboard with) none of them.
func preflightDashboardImport(m map[string]interface{}) error {
	elements, _ := m["dashboard_elements"].([]interface{})
	if len(elements) == 0 {
		return nil
	}
	components := 0
	layouts, _ := m["dashboard_layouts"].([]interface{})
	for _, l := range layouts {
		lm, _ := l.(map[string]interface{})
		comps, _ := lm["dashboard_layout_components"].([]interface{})
		components += len(comps)
	}
	if components == 0 {
		return fmt.Errorf("file has %d dashboard_elements but no dashboard_layouts with dashboard_layout_components, so no elements would be imported; re-export the dashboard including its layouts (for example without a restrictive --fields)", len(elements))
	}
	return nil
}
