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
	"path/filepath"
	"sort"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	permissionLsFields       string
	permissionLsPlain        bool
	permissionLsCSV          bool
	permissionSetLsFields    string
	permissionSetLsPlain     bool
	permissionSetLsCSV       bool
	permissionSetCatFields   string
	permissionSetCatDir      string
	permissionSetCatTrim     bool
	permissionSetImportForce bool
	permissionSetImportPlain bool
)

var PermissionCmd = &cobra.Command{
	Use:   "permission",
	Short: "Command to retrieve available permissions",
}

var permissionLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list permissions",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		permissions, err := c.SDK.AllPermissions(nil)
		if err != nil {
			return fmt.Errorf("failed to list permissions: %w", err)
		}

		headers := util.ParseFieldsForHeaders(permissionLsFields)

		table := util.NewTable(headers)
		for _, p := range permissions {
			table.Append(extractFields(p, permissionLsFields))
		}

		table.Render(permissionLsPlain, permissionLsCSV)
		return nil
	},
}

var permissionTreeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Display permissions in a hierarchical tree view",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		permissions, err := c.SDK.AllPermissions(nil)
		if err != nil {
			return fmt.Errorf("failed to get permissions: %w", err)
		}

		if len(permissions) == 0 {
			fmt.Println("No permissions found")
			return nil
		}

		fmt.Print(buildTree(permissions))
		return nil
	},
}

func buildTree(data []v4.Permission) string {
	sort.Slice(data, func(i, j int) bool {
		pI, pJ := "", ""
		if data[i].Permission != nil {
			pI = *data[i].Permission
		}
		if data[j].Permission != nil {
			pJ = *data[j].Permission
		}
		return pI < pJ
	})

	var roots []string
	for _, p := range data {
		if p.Parent == nil || *p.Parent == "" {
			if p.Permission != nil {
				roots = append(roots, *p.Permission)
			}
		}
	}

	var sb strings.Builder
	for i, r := range roots {
		lastRoot := i == len(roots)-1
		sb.WriteString(renderNode(r, data, "", lastRoot))
	}
	return sb.String()
}

func renderNode(node string, data []v4.Permission, indent string, isLast bool) string {
	var sb strings.Builder
	marker := "├── "
	if isLast {
		marker = "└── "
	}
	sb.WriteString(indent + marker + node + "\n")

	var children []string
	for _, p := range data {
		if p.Parent != nil && *p.Parent == node {
			if p.Permission != nil {
				children = append(children, *p.Permission)
			}
		}
	}

	nextIndent := indent
	if isLast {
		nextIndent += "    "
	} else {
		nextIndent += "│   "
	}
	for i, child := range children {
		lastChild := i == len(children)-1
		sb.WriteString(renderNode(child, data, nextIndent, lastChild))
	}
	return sb.String()
}

var PermissionSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Commands pertaining to permission sets",
}

var permissionSetLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list all permission sets",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		sets, err := c.SDK.AllPermissionSets(permissionSetLsFields, nil)
		if err != nil {
			return fmt.Errorf("failed to list permission sets: %w", err)
		}

		headers := util.ParseFieldsForHeaders(permissionSetLsFields)

		table := util.NewTable(headers)
		for _, s := range sets {
			table.Append(extractFields(s, permissionSetLsFields))
		}

		table.Render(permissionSetLsPlain, permissionSetLsCSV)
		return nil
	},
}

var permissionSetCatCmd = &cobra.Command{
	Use:   "cat [PERMISSION_SET_ID]",
	Short: "Output the JSON representation of a permission set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		setID := args[0]
		set, err := c.SDK.PermissionSet(setID, permissionSetCatFields, nil)
		if err != nil {
			return fmt.Errorf("failed to get permission set %s: %w", setID, err)
		}

		b, _ := json.Marshal(set)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if permissionSetCatTrim {
			keep := map[string]bool{
				"id":          true,
				"name":        true,
				"permissions": true,
				"built_in":    true,
			}
			for k := range m {
				if !keep[k] {
					delete(m, k)
				}
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if permissionSetCatDir != "" {
			name := ""
			if v, ok := m["name"].(string); ok {
				name = v
			}
			fn := filepath.Join(permissionSetCatDir, fmt.Sprintf("Permission_Set_%s.json", name))
			if err := writeOutputFile(fn, outBytes); err != nil {
				return err
			}
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(string(outBytes))
		}
		return nil
	},
}

var permissionSetImportCmd = &cobra.Command{
	Use:   "import [PERMISSION_SET_FILE]",
	Short: "Import a permission set from a JSON file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		file := args[0]
		b, err := util.ReadFileOrStdin(file)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("invalid json in %s: %w", file, err)
		}

		nameVal, ok := m["name"].(string)
		if !ok || nameVal == "" {
			return fmt.Errorf("permission set file missing name")
		}

		var wps v4.WritePermissionSet
		if err := json.Unmarshal(b, &wps); err != nil {
			return fmt.Errorf("failed to unmarshal WritePermissionSet: %w", err)
		}

		sets, err := c.SDK.AllPermissionSets("", nil)
		if err != nil {
			return fmt.Errorf("failed to list permission sets: %w", err)
		}

		var existingSet *v4.PermissionSet
		for _, s := range sets {
			if s.Name != nil && *s.Name == nameVal {
				existingSet = &s
				break
			}
		}

		var resultSet *v4.PermissionSet
		if existingSet != nil {
			if !permissionSetImportForce {
				return fmt.Errorf("permission set '%s' already exists. Use --force to overwrite", nameVal)
			}
			esID := *existingSet.Id
			updated, err := c.SDK.UpdatePermissionSet(esID, wps, nil)
			if err != nil {
				return fmt.Errorf("failed to update permission set %s: %w", esID, err)
			}
			resultSet = &updated
		} else {
			created, err := c.SDK.CreatePermissionSet(wps, nil)
			if err != nil {
				return fmt.Errorf("failed to create permission set %s: %w", nameVal, err)
			}
			resultSet = &created
		}

		resID := ""
		if resultSet.Id != nil {
			resID = *resultSet.Id
		}

		if permissionSetImportPlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Imported permission set %s\n", resID)
		}
		return nil
	},
}

var permissionSetRmCmd = &cobra.Command{
	Use:   "rm [PERMISSION_SET_ID]",
	Short: "Delete a permission set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		setID := args[0]
		_, err = c.SDK.DeletePermissionSet(setID, nil)
		if err != nil {
			return fmt.Errorf("failed to delete permission set %s: %w", setID, err)
		}
		fmt.Printf("Permission set %s deleted.\n", setID)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(PermissionCmd)
	PermissionCmd.AddCommand(permissionLsCmd)
	PermissionCmd.AddCommand(permissionTreeCmd)

	PermissionCmd.AddCommand(PermissionSetCmd)
	PermissionSetCmd.AddCommand(permissionSetLsCmd)
	PermissionSetCmd.AddCommand(permissionSetCatCmd)
	PermissionSetCmd.AddCommand(permissionSetImportCmd)
	PermissionSetCmd.AddCommand(permissionSetRmCmd)

	permissionLsCmd.Flags().StringVar(&permissionLsFields, "fields", "permission,parent,description", "Fields to display")
	permissionLsCmd.Flags().BoolVar(&permissionLsPlain, "plain", false, "print without any extra formatting")
	permissionLsCmd.Flags().BoolVar(&permissionLsCSV, "csv", false, "output in csv format")

	permissionSetLsCmd.Flags().StringVar(&permissionSetLsFields, "fields", "id,name,permissions", "Fields to display")
	permissionSetLsCmd.Flags().BoolVar(&permissionSetLsPlain, "plain", false, "print without any extra formatting")
	permissionSetLsCmd.Flags().BoolVar(&permissionSetLsCSV, "csv", false, "output in csv format")

	permissionSetCatCmd.Flags().StringVar(&permissionSetCatDir, "dir", "", "Directory to store output file")
	permissionSetCatCmd.Flags().StringVar(&permissionSetCatFields, "fields", "", "Fields to display")
	permissionSetCatCmd.Flags().BoolVar(&permissionSetCatTrim, "trim", false, "Trim output to minimal set of fields")

	permissionSetImportCmd.Flags().BoolVar(&permissionSetImportForce, "force", false, "Overwrite existing permission set")
	permissionSetImportCmd.Flags().BoolVar(&permissionSetImportPlain, "plain", false, "Output only permission set ID")
}
