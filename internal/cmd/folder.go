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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	folderLsFields   string
	folderLsPlain    bool
	folderLsCSV      bool
	folderTopFields  string
	folderTopPlain   bool
	folderTopCSV     bool
	folderTreePlain  bool
	folderCatFields  string
	folderCatDir     string
	folderExportDir  string
	folderExportTar  string
	folderExportTgz  string
	folderExportZip  string
	folderExportTrim bool
	folderRmForce    bool
)

var FolderCmd = &cobra.Command{
	Use:     "folder",
	Aliases: []string{"space"},
	Short:   "Commands pertaining to folders/spaces",
}

func resolveFolderID(c *client.ClientWrapper, arg string) ([]string, error) {
	if arg == "" {
		me, err := c.SDK.Me("home_folder_id", nil)
		if err != nil || me.HomeFolderId == nil {
			return nil, fmt.Errorf("failed to get home_folder_id: %v", err)
		}
		return []string{*me.HomeFolderId}, nil
	}

	if arg == "lookml" {
		return []string{"lookml"}, nil
	}

	if _, err := strconv.ParseInt(arg, 10, 64); err == nil {
		return []string{arg}, nil
	}

	if arg == "~" {
		me, err := c.SDK.Me("personal_folder_id", nil)
		if err != nil || me.PersonalFolderId == nil {
			return nil, fmt.Errorf("failed to get personal_folder_id: %v", err)
		}
		return []string{*me.PersonalFolderId}, nil
	}

	if strings.HasPrefix(arg, "~") {
		sub := arg[1:]
		if _, err := strconv.ParseInt(sub, 10, 64); err == nil {
			u, err := c.SDK.User(sub, "personal_folder_id", nil)
			if err != nil || u.PersonalFolderId == nil {
				return nil, fmt.Errorf("failed to get personal_folder_id for user %s: %v", sub, err)
			}
			return []string{*u.PersonalFolderId}, nil
		}

		if strings.Contains(sub, "@") {
			users, err := c.SDK.SearchUsers(v4.RequestSearchUsers{Email: &sub, Fields: ptr("personal_folder_id")}, nil)
			if err != nil {
				return nil, err
			}
			var ids []string
			for _, u := range users {
				if u.PersonalFolderId != nil {
					ids = append(ids, *u.PersonalFolderId)
				}
			}
			return ids, nil
		}

		parts := strings.Fields(sub)
		if len(parts) == 2 {
			users, err := c.SDK.SearchUsers(v4.RequestSearchUsers{FirstName: &parts[0], LastName: &parts[1], Fields: ptr("personal_folder_id")}, nil)
			if err != nil {
				return nil, err
			}
			var ids []string
			for _, u := range users {
				if u.PersonalFolderId != nil {
					ids = append(ids, *u.PersonalFolderId)
				}
			}
			return ids, nil
		}
	}

	folders, err := c.SDK.SearchFolders(v4.RequestSearchFolders{Name: &arg, Fields: ptr("id")}, nil)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, f := range folders {
		if f.Id != nil {
			ids = append(ids, *f.Id)
		}
	}

	if arg == "Shared" {
		homeName := "Home"
		roots, err := c.SDK.SearchFolders(v4.RequestSearchFolders{Name: &homeName, Fields: ptr("id,is_shared_root")}, nil)
		if err == nil {
			for _, r := range roots {
				if r.IsSharedRoot != nil && *r.IsSharedRoot && r.Id != nil {
					ids = append(ids, *r.Id)
				}
			}
		}
	}

	return ids, nil
}

func ptr(s string) *string { return &s }

var folderLsCmd = &cobra.Command{
	Use:   "ls [FOLDER_ID]",
	Short: "list the contents of a folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		fIDs, err := resolveFolderID(c, arg)
		if err != nil {
			return err
		}
		if len(fIDs) == 0 {
			fmt.Printf("No folders match %s\n", arg)
			return nil
		}

		fields := folderLsFields
		if arg == "lookml" {
			fields = "dashboards(id,title)"
		}

		var data []map[string]interface{}
		headers := util.ParseFieldsForHeaders(folderLsFields)

		for _, fID := range fIDs {
			folder, err := c.SDK.Folder(fID, fields, nil)
			if err == nil {
				var m map[string]interface{}
				b, _ := json.Marshal(folder)
				_ = json.Unmarshal(b, &m)
				data = append(data, m)
			}

			children, err := c.SDK.FolderChildren(v4.RequestFolderChildren{FolderId: fID, Fields: ptr("id,name,parent_id")}, nil)
			if err == nil {
				for _, ch := range children {
					var m map[string]interface{}
					b, _ := json.Marshal(ch)
					_ = json.Unmarshal(b, &m)
					data = append(data, m)
				}
			}
		}

		table := util.NewTable(headers)
		table.Rows = flattenData(data, headers)
		table.Render(folderLsPlain, folderLsCSV)
		return nil
	},
}

var folderTopCmd = &cobra.Command{
	Use:   "top",
	Short: "Retrieve the top level (or root) folders",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		extraFields := []string{"is_shared_root", "is_users_root", "is_embed_shared_root", "is_embed_users_root"}
		queryFieldsSlice := strings.Split(folderTopFields, ",")
		for i := range queryFieldsSlice {
			queryFieldsSlice[i] = strings.TrimSpace(queryFieldsSlice[i])
		}

		fieldMap := make(map[string]bool)
		for _, f := range queryFieldsSlice {
			fieldMap[f] = true
		}
		for _, f := range extraFields {
			if !fieldMap[f] {
				queryFieldsSlice = append(queryFieldsSlice, f)
			}
		}

		folders, err := c.SDK.AllFolders(strings.Join(queryFieldsSlice, ","), nil)
		if err != nil {
			return fmt.Errorf("failed to get top level folders: %w", err)
		}

		var topFolders []v4.FolderBase
		for _, f := range folders {
			isTop := (f.IsSharedRoot != nil && *f.IsSharedRoot) ||
				(f.IsUsersRoot != nil && *f.IsUsersRoot) ||
				(f.IsEmbedSharedRoot != nil && *f.IsEmbedSharedRoot) ||
				(f.IsEmbedUsersRoot != nil && *f.IsEmbedUsersRoot)
			if isTop {
				topFolders = append(topFolders, f)
			}
		}

		if len(topFolders) == 0 {
			fmt.Println("No top level folders found")
			return nil
		}

		headers := util.ParseFieldsForHeaders(folderTopFields)

		table := util.NewTable(headers)
		for _, f := range topFolders {
			table.Append(extractFields(f, folderTopFields))
		}

		table.Render(folderTopPlain, folderTopCSV)
		return nil
	},
}

func mapToRow(m map[string]interface{}, headers []string) []string {
	row := make([]string, len(headers))
	for i, h := range headers {
		if val, ok := m[h]; ok && val != nil {
			switch v := val.(type) {
			case *string:
				if v != nil {
					row[i] = *v
				}
			case string:
				row[i] = v
			case *int64:
				if v != nil {
					row[i] = strconv.FormatInt(*v, 10)
				}
			case int64:
				row[i] = strconv.FormatInt(v, 10)
			case *float64:
				if v != nil {
					row[i] = strconv.FormatFloat(*v, 'f', -1, 64)
				}
			case float64:
				row[i] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				b, _ := json.Marshal(v)
				row[i] = string(b)
			}
		}
	}
	return row
}

var folderTreeCmd = &cobra.Command{
	Use:   "tree [FOLDER_ID]",
	Short: "display child folders, dashboards, and looks in a tree format",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		fIDs, err := resolveFolderID(c, arg)
		if err != nil {
			return err
		}
		if len(fIDs) == 0 {
			fmt.Printf("No folders match %s\n", arg)
			return nil
		}

		for _, fID := range fIDs {
			printTree(c, fID, "")
		}
		return nil
	},
}

func printTree(c *client.ClientWrapper, folderID, indent string) {
	folder, err := c.SDK.Folder(folderID, "id,name,looks(id,title),dashboards(id,title)", nil)
	if err != nil {
		return
	}
	name := folder.Name
	if indent == "" {
		fmt.Println(name)
	}

	var items []string
	if folder.Looks != nil {
		for _, l := range *folder.Looks {
			t := ""
			if l.Title != nil {
				t = *l.Title
			}
			items = append(items, "(l) "+t)
		}
	}
	if folder.Dashboards != nil {
		for _, d := range *folder.Dashboards {
			t := ""
			if d.Title != nil {
				t = *d.Title
			}
			items = append(items, "(d) "+t)
		}
	}
	children, _ := c.SDK.FolderChildren(v4.RequestFolderChildren{FolderId: folderID, Fields: ptr("id,name")}, nil)
	for _, ch := range children {
		items = append(items, ch.Name)
	}

	for i, item := range items {
		isLast := i == len(items)-1
		prefix := "├── "
		childIndent := indent + "│   "
		if isLast {
			prefix = "└── "
			childIndent = indent + "    "
		}
		fmt.Println(indent + prefix + item)

		if i >= len(items)-len(children) {
			ch := children[i-(len(items)-len(children))]
			if ch.Id != nil {
				printTree(c, *ch.Id, childIndent)
			}
		}
	}
}

var folderCatCmd = &cobra.Command{
	Use:   "cat [FOLDER_ID]",
	Short: "output json describing a folder",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		fID := args[0]
		folder, err := c.SDK.Folder(fID, folderCatFields, nil)
		if err != nil {
			return fmt.Errorf("failed to get folder %s: %w", fID, err)
		}

		bytes, _ := json.MarshalIndent(folder, "", "  ")
		if folderCatDir != "" {
			fn := filepath.Join(folderCatDir, fmt.Sprintf("Folder_%s_%s.json", fID, sanitizeFilename(folder.Name)))
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

var folderRmCmd = &cobra.Command{
	Use:   "rm [FOLDER_ID]",
	Short: "delete a folder",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		fID := args[0]

		if !folderRmForce {
			folder, err := c.SDK.Folder(fID, "id,name,looks(id),dashboards(id)", nil)
			if err != nil {
				return fmt.Errorf("folder %s not found: %w", fID, err)
			}
			children, err := c.SDK.FolderChildren(v4.RequestFolderChildren{FolderId: fID, Fields: ptr("id")}, nil)
			if err != nil {
				return fmt.Errorf("failed to check subfolders of folder %s: %w", fID, err)
			}
			hasLooks := folder.Looks != nil && len(*folder.Looks) > 0
			hasDash := folder.Dashboards != nil && len(*folder.Dashboards) > 0
			hasChild := len(children) > 0
			if hasLooks || hasDash || hasChild {
				return fmt.Errorf("folder '%s' is not empty. Cannot delete unless --force is specified", folder.Name)
			}
		}

		_, err = c.SDK.DeleteFolder(fID, nil)
		if err != nil {
			return fmt.Errorf("failed to delete folder %s: %w", fID, err)
		}
		fmt.Printf("Folder %s deleted.\n", fID)
		return nil
	},
}

var folderCreateCmd = &cobra.Command{
	Use:   "create [NAME] [PARENT_ID]",
	Short: "create a new subfolder",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		name := args[0]
		parentID := args[1]

		body := v4.CreateFolder{Name: name, ParentId: parentID}
		folder, err := c.SDK.CreateFolder(body, nil)
		if err != nil {
			return fmt.Errorf("failed to create folder %s: %w", name, err)
		}
		id := ""
		if folder.Id != nil {
			id = *folder.Id
		}
		fmt.Printf("Folder created with ID %s\n", id)
		return nil
	},
}

var folderExportCmd = &cobra.Command{
	Use:   "export [FOLDER_ID]",
	Short: "export a folder and its subfolders, looks, and dashboards",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		fID := args[0]

		if folderExportTar != "" || folderExportTgz != "" || folderExportZip != "" {
			var buf bytes.Buffer
			var archiver interface{}
			var gzw *gzip.Writer

			if folderExportZip != "" {
				archiver = zip.NewWriter(&buf)
			} else {
				var w io.Writer = &buf
				if folderExportTgz != "" {
					gzw = gzip.NewWriter(&buf)
					w = gzw
				}
				archiver = tar.NewWriter(w)
			}

			err = exportFolderArchive(c, fID, archiver, "")
			if err != nil {
				return err
			}

			if zw, ok := archiver.(*zip.Writer); ok {
				fn := folderExportZip
				if err := zw.Close(); err != nil {
					return fmt.Errorf("failed to finalize zip %s: %w", fn, err)
				}
				if err := writeOutputFile(fn, buf.Bytes()); err != nil {
					return err
				}
				fmt.Printf("Wrote zip %s\n", fn)
			} else if tw, ok := archiver.(*tar.Writer); ok {
				fn := folderExportTar
				if folderExportTgz != "" {
					fn = folderExportTgz
				}
				if err := tw.Close(); err != nil {
					return fmt.Errorf("failed to finalize tar %s: %w", fn, err)
				}
				if gzw != nil {
					if err := gzw.Close(); err != nil {
						return fmt.Errorf("failed to finalize gzip %s: %w", fn, err)
					}
				}
				if err := writeOutputFile(fn, buf.Bytes()); err != nil {
					return err
				}
				fmt.Printf("Wrote archive %s\n", fn)
			}
			return nil
		}

		outDir := folderExportDir
		if outDir == "" {
			outDir = "."
		}
		return exportFolderDir(c, fID, outDir)
	},
}

func exportFolderDir(c *client.ClientWrapper, fID, baseDir string) error {
	folder, err := c.SDK.Folder(fID, "", nil)
	if err != nil {
		return fmt.Errorf("failed to get folder %s: %w", fID, err)
	}
	name := folder.Name
	cleanName := sanitizeFilename(name)
	dirName := filepath.Join(baseDir, fmt.Sprintf("Folder_%s_%s", fID, cleanName))
	if err := os.MkdirAll(dirName, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dirName, err)
	}

	b, _ := json.Marshal(folder)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	delete(m, "looks")
	delete(m, "dashboards")
	fb, _ := json.MarshalIndent(m, "", "  ")
	if err := writeOutputFile(filepath.Join(dirName, fmt.Sprintf("Folder_%s_%s.json", fID, cleanName)), fb); err != nil {
		return err
	}

	var errs []error
	if folder.Looks != nil {
		for _, l := range *folder.Looks {
			if l.Id != nil {
				lookMap, err := CatLookHelper(c, *l.Id, folderExportTrim, false, "")
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to export look %s: %v\n", *l.Id, err)
					errs = append(errs, fmt.Errorf("failed to export look %s: %w", *l.Id, err))
					continue
				}
				lb, _ := json.MarshalIndent(lookMap, "", "  ")
				lt := ""
				if v, ok := lookMap["title"].(string); ok {
					lt = v
				}
				if err := writeOutputFile(filepath.Join(dirName, fmt.Sprintf("Look_%s_%s.json", *l.Id, sanitizeFilename(lt))), lb); err != nil {
					return err
				}
			}
		}
	}
	if folder.Dashboards != nil {
		for _, d := range *folder.Dashboards {
			if d.Id != nil {
				dashMap, err := CatDashboardHelper(c, *d.Id, folderExportTrim, false, "")
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to export dashboard %s: %v\n", *d.Id, err)
					errs = append(errs, fmt.Errorf("failed to export dashboard %s: %w", *d.Id, err))
					continue
				}
				db, _ := json.MarshalIndent(dashMap, "", "  ")
				dt := ""
				if v, ok := dashMap["title"].(string); ok {
					dt = v
				}
				if err := writeOutputFile(filepath.Join(dirName, fmt.Sprintf("Dashboard_%s_%s.json", *d.Id, sanitizeFilename(dt))), db); err != nil {
					return err
				}
			}
		}
	}

	children, err := c.SDK.FolderChildren(v4.RequestFolderChildren{FolderId: fID, Fields: ptr("id")}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list children of folder %s: %v\n", fID, err)
		errs = append(errs, fmt.Errorf("failed to list children of folder %s: %w", fID, err))
		return errors.Join(errs...)
	}
	for _, ch := range children {
		if ch.Id != nil {
			if err := exportFolderDir(c, *ch.Id, dirName); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func exportFolderArchive(c *client.ClientWrapper, fID string, archiver interface{}, pathPrefix string) error {
	folder, err := c.SDK.Folder(fID, "", nil)
	if err != nil {
		return fmt.Errorf("failed to get folder %s: %w", fID, err)
	}
	name := folder.Name
	cleanName := sanitizeFilename(name)
	dirName := fmt.Sprintf("Folder_%s_%s/", fID, cleanName)
	if pathPrefix != "" {
		dirName = pathPrefix + dirName
	}

	b, _ := json.Marshal(folder)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	delete(m, "looks")
	delete(m, "dashboards")
	fb, _ := json.MarshalIndent(m, "", "  ")

	fn := dirName + fmt.Sprintf("Folder_%s_%s.json", fID, cleanName)
	if err := writeArchiveFile(archiver, fn, fb); err != nil {
		return err
	}

	var errs []error
	if folder.Looks != nil {
		for _, l := range *folder.Looks {
			if l.Id != nil {
				lookMap, err := CatLookHelper(c, *l.Id, folderExportTrim, false, "")
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to export look %s to archive: %v\n", *l.Id, err)
					errs = append(errs, fmt.Errorf("failed to export look %s: %w", *l.Id, err))
					continue
				}
				lb, _ := json.MarshalIndent(lookMap, "", "  ")
				lt := ""
				if v, ok := lookMap["title"].(string); ok {
					lt = v
				}
				lfn := dirName + fmt.Sprintf("Look_%s_%s.json", *l.Id, sanitizeFilename(lt))
				if err := writeArchiveFile(archiver, lfn, lb); err != nil {
					return err
				}
			}
		}
	}
	if folder.Dashboards != nil {
		for _, d := range *folder.Dashboards {
			if d.Id != nil {
				dashMap, err := CatDashboardHelper(c, *d.Id, folderExportTrim, false, "")
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to export dashboard %s to archive: %v\n", *d.Id, err)
					errs = append(errs, fmt.Errorf("failed to export dashboard %s: %w", *d.Id, err))
					continue
				}
				db, _ := json.MarshalIndent(dashMap, "", "  ")
				dt := ""
				if v, ok := dashMap["title"].(string); ok {
					dt = v
				}
				dfn := dirName + fmt.Sprintf("Dashboard_%s_%s.json", *d.Id, sanitizeFilename(dt))
				if err := writeArchiveFile(archiver, dfn, db); err != nil {
					return err
				}
			}
		}
	}

	children, err := c.SDK.FolderChildren(v4.RequestFolderChildren{FolderId: fID, Fields: ptr("id")}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list children of folder %s: %v\n", fID, err)
		errs = append(errs, fmt.Errorf("failed to list children of folder %s: %w", fID, err))
		return errors.Join(errs...)
	}
	for _, ch := range children {
		if ch.Id != nil {
			if err := exportFolderArchive(c, *ch.Id, archiver, dirName); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func writeArchiveFile(archiver interface{}, name string, data []byte) error {
	switch a := archiver.(type) {
	case *zip.Writer:
		f, err := a.Create(name)
		if err != nil {
			return fmt.Errorf("failed to create zip entry %s: %w", name, err)
		}
		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("failed to write zip entry %s: %w", name, err)
		}
		return nil
	case *tar.Writer:
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if err := a.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write tar header %s: %w", name, err)
		}
		if _, err := a.Write(data); err != nil {
			return fmt.Errorf("failed to write tar entry %s: %w", name, err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported archive writer %T", archiver)
	}
}

func init() {
	RootCmd.AddCommand(FolderCmd)
	FolderCmd.AddCommand(folderLsCmd)
	FolderCmd.AddCommand(folderTopCmd)
	FolderCmd.AddCommand(folderTreeCmd)
	FolderCmd.AddCommand(folderCatCmd)
	FolderCmd.AddCommand(folderRmCmd)
	FolderCmd.AddCommand(folderCreateCmd)
	FolderCmd.AddCommand(folderExportCmd)

	folderLsCmd.Flags().StringVar(&folderLsFields, "fields", "parent_id,id,name,looks(id,title),dashboards(id,title)", "Fields to display")
	folderLsCmd.Flags().BoolVar(&folderLsPlain, "plain", false, "print without any extra formatting")
	folderLsCmd.Flags().BoolVar(&folderLsCSV, "csv", false, "output in csv format")

	folderTopCmd.Flags().StringVar(&folderTopFields, "fields", "id,name,is_shared_root,is_users_root,is_embed_shared_root,is_embed_users_root", "Fields to display")
	folderTopCmd.Flags().BoolVar(&folderTopPlain, "plain", false, "print without any extra formatting")
	folderTopCmd.Flags().BoolVar(&folderTopCSV, "csv", false, "output in csv format")

	folderTreeCmd.Flags().BoolVar(&folderTreePlain, "plain", false, "print without any extra formatting")

	folderCatCmd.Flags().StringVar(&folderCatFields, "fields", "", "Fields to display")
	folderCatCmd.Flags().StringVar(&folderCatDir, "dir", "", "Directory to store output file")

	folderRmCmd.Flags().BoolVar(&folderRmForce, "force", false, "Delete folder even if not empty")

	folderExportCmd.Flags().StringVar(&folderExportDir, "dir", "", "Directory to store folder tree")
	folderExportCmd.Flags().StringVar(&folderExportTar, "tar", "", "Tar file to store folder tree")
	folderExportCmd.Flags().StringVar(&folderExportTgz, "tgz", "", "Targz file to store folder tree")
	folderExportCmd.Flags().StringVar(&folderExportZip, "zip", "", "Zip file to store folder tree")
	folderExportCmd.Flags().BoolVar(&folderExportTrim, "trim", false, "Trim output to minimal set of fields")
}

func sanitizeFilename(name string) string {
	invalidChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
	clean := name
	for _, char := range invalidChars {
		clean = strings.ReplaceAll(clean, char, "_")
	}
	var sb strings.Builder
	for _, r := range clean {
		if r >= 32 {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

func flattenData(data []map[string]interface{}, headers []string) [][]string {
	var rows [][]string

	// 1. Map basic info (non-array, non-map fields)
	for _, entry := range data {
		basicEntry := make(map[string]interface{})
		for k, v := range entry {
			if v == nil {
				continue
			}
			switch v.(type) {
			case []interface{}, map[string]interface{}:
				// skip
			default:
				basicEntry[k] = v
			}
		}
		rows = append(rows, mapToRow(basicEntry, headers))
	}

	// 2. Map array fields
	for _, entry := range data {
		for k, v := range entry {
			if v == nil {
				continue
			}
			if sliceVal, ok := v.([]interface{}); ok {
				for _, item := range sliceVal {
					if mapItem, ok := item.(map[string]interface{}); ok {
						h := make(map[string]interface{})
						for subK, subV := range mapItem {
							h[fmt.Sprintf("%s(%s)", k, subK)] = subV
						}
						rows = append(rows, mapToRow(h, headers))
					}
				}
			}
		}
	}

	return rows
}
