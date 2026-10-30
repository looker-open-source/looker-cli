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

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	roleLsFields      string
	roleLsPlain       bool
	roleLsCSV         bool
	roleCatFields     string
	roleCatDir        string
	roleCatTrim       bool
	roleCreatePlain   bool
	roleGroupLsFields string
	roleGroupLsPlain  bool
	roleGroupLsCSV    bool
	roleUserLsFields  string
	roleUserLsPlain   bool
	roleUserLsCSV     bool
	roleUserLsAll     bool
)

var RoleCmd = &cobra.Command{
	Use:   "role",
	Short: "Commands pertaining to roles",
}

var roleLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "Display all roles",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		req := v4.RequestAllRoles{
			Fields: &roleLsFields,
		}
		roles, err := c.SDK.AllRoles(req, nil)
		if err != nil {
			return fmt.Errorf("failed to list roles: %w", err)
		}

		headers := util.ParseFieldsForHeaders(roleLsFields)

		table := util.NewTable(headers)
		for _, r := range roles {
			table.Append(extractFields(r, roleLsFields))
		}
		table.Render(roleLsPlain, roleLsCSV)
		return nil
	},
}

var roleCatCmd = &cobra.Command{
	Use:   "cat [ROLE_ID]",
	Short: "Output the JSON representation of a role",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		req := v4.RequestSearchRoles{
			Id: &rID,
		}
		if roleCatFields != "" {
			req.Fields = &roleCatFields
		}
		roles, err := c.SDK.SearchRoles(req, nil)
		if err != nil {
			return err
		}
		if len(roles) == 0 {
			return fmt.Errorf("role %s not found", rID)
		}
		role := roles[0]

		b, _ := json.Marshal(role)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if roleCatTrim {
			keep := map[string]bool{
				"name":              true,
				"permission_set_id": true,
				"model_set_id":      true,
			}
			for k := range m {
				if !keep[k] {
					delete(m, k)
				}
			}
			if role.PermissionSet != nil && role.PermissionSet.Id != nil {
				m["permission_set_id"] = *role.PermissionSet.Id
			}
			if role.ModelSet != nil && role.ModelSet.Id != nil {
				m["model_set_id"] = *role.ModelSet.Id
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if roleCatDir != "" {
			name := ""
			if role.Name != nil {
				name = sanitizeFilename(*role.Name)
			}
			fn := filepath.Join(roleCatDir, fmt.Sprintf("Role_%s_%s.json", sanitizeFilename(rID), name))
			if err := os.WriteFile(fn, outBytes, 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", fn, err)
			}
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(string(outBytes))
		}
		return nil
	},
}

var roleRmCmd = &cobra.Command{
	Use:   "rm [ROLE_ID]",
	Short: "Delete a role",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		_, err = c.SDK.DeleteRole(rID, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Role %s deleted.\n", rID)
		return nil
	},
}

var roleCreateCmd = &cobra.Command{
	Use:   "create [ROLE_NAME] [PERMISSION_SET_ID] [MODEL_SET_ID]",
	Short: "Create new role",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		name := args[0]
		pSet := args[1]
		mSet := args[2]

		wr := v4.WriteRole{
			Name:            &name,
			PermissionSetId: &pSet,
			ModelSetId:      &mSet,
		}
		role, err := c.SDK.CreateRole(wr, nil)
		if err != nil {
			return err
		}

		resID := getStringValue(role.Id)
		if roleCreatePlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Role %s (%s) created.\n", resID, getStringValue(role.Name))
		}
		return nil
	},
}

var roleGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage groups associated with a role",
}

var roleUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users associated with a role",
}

var roleGroupLsCmd = &cobra.Command{
	Use:   "ls [ROLE_ID]",
	Short: "List the groups assigned to a role",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		groups, err := c.SDK.RoleGroups(rID, roleGroupLsFields, nil)
		if err != nil {
			return err
		}

		headers := strings.Split(roleGroupLsFields, ",")
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		table := util.NewTable(headers)
		for _, g := range groups {
			table.Append(extractFields(g, roleGroupLsFields))
		}
		table.Render(roleGroupLsPlain, roleGroupLsCSV)
		return nil
	},
}

var roleUserLsCmd = &cobra.Command{
	Use:   "ls [ROLE_ID]",
	Short: "List the users assigned to a role",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]

		directOnly := !roleUserLsAll
		req := v4.RequestRoleUsers{
			RoleId:                rID,
			Fields:                &roleUserLsFields,
			DirectAssociationOnly: &directOnly,
		}
		users, err := c.SDK.RoleUsers(req, nil)
		if err != nil {
			return err
		}

		headers := strings.Split(roleUserLsFields, ",")
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		table := util.NewTable(headers)
		for _, u := range users {
			table.Append(extractFields(u, roleUserLsFields))
		}
		table.Render(roleUserLsPlain, roleUserLsCSV)
		return nil
	},
}

var roleGroupAddCmd = &cobra.Command{
	Use:   "add [ROLE_ID] [GROUP_ID...]",
	Short: "Add indicated groups to role",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		newGroups := args[1:]

		existing, err := c.SDK.RoleGroups(rID, "id", nil)
		if err != nil {
			return err
		}

		groupMap := make(map[string]bool)
		for _, g := range existing {
			if g.Id != nil {
				groupMap[*g.Id] = true
			}
		}
		for _, g := range newGroups {
			groupMap[g] = true
		}

		combined := make([]string, 0, len(groupMap))
		for g := range groupMap {
			combined = append(combined, g)
		}

		_, err = c.SDK.SetRoleGroups(rID, combined, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Groups added to role %s.\n", rID)
		return nil
	},
}

var roleGroupRmCmd = &cobra.Command{
	Use:   "rm [ROLE_ID] [GROUP_ID...]",
	Short: "Remove indicated groups from role",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		rmGroups := args[1:]

		existing, err := c.SDK.RoleGroups(rID, "id", nil)
		if err != nil {
			return err
		}

		rmMap := make(map[string]bool)
		for _, g := range rmGroups {
			rmMap[g] = true
		}

		remaining := make([]string, 0)
		for _, g := range existing {
			if g.Id != nil && !rmMap[*g.Id] {
				remaining = append(remaining, *g.Id)
			}
		}

		_, err = c.SDK.SetRoleGroups(rID, remaining, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Groups removed from role %s.\n", rID)
		return nil
	},
}

var roleUserAddCmd = &cobra.Command{
	Use:   "add [ROLE_ID] [USER_ID...]",
	Short: "Add indicated users to role",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		newUsers := args[1:]

		existing, err := c.SDK.RoleUsers(v4.RequestRoleUsers{RoleId: rID, Fields: ptr("id"), DirectAssociationOnly: ptrBool(true)}, nil)
		if err != nil {
			return err
		}

		userMap := make(map[string]bool)
		for _, u := range existing {
			if u.Id != nil {
				userMap[*u.Id] = true
			}
		}
		for _, u := range newUsers {
			userMap[u] = true
		}

		combined := make([]string, 0, len(userMap))
		for u := range userMap {
			combined = append(combined, u)
		}

		_, err = c.SDK.SetRoleUsers(rID, combined, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Users added to role %s.\n", rID)
		return nil
	},
}

var roleUserRmCmd = &cobra.Command{
	Use:   "rm [ROLE_ID] [USER_ID...]",
	Short: "Remove indicated users from role",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		rID := args[0]
		rmUsers := args[1:]

		existing, err := c.SDK.RoleUsers(v4.RequestRoleUsers{RoleId: rID, Fields: ptr("id"), DirectAssociationOnly: ptrBool(true)}, nil)
		if err != nil {
			return err
		}

		rmMap := make(map[string]bool)
		for _, u := range rmUsers {
			rmMap[u] = true
		}

		remaining := make([]string, 0)
		for _, u := range existing {
			if u.Id != nil && !rmMap[*u.Id] {
				remaining = append(remaining, *u.Id)
			}
		}

		_, err = c.SDK.SetRoleUsers(rID, remaining, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Users removed from role %s.\n", rID)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(RoleCmd)
	RoleCmd.AddCommand(roleLsCmd)
	RoleCmd.AddCommand(roleCatCmd)
	RoleCmd.AddCommand(roleRmCmd)
	RoleCmd.AddCommand(roleCreateCmd)

	RoleCmd.AddCommand(roleGroupCmd)
	roleGroupCmd.AddCommand(roleGroupLsCmd)
	roleGroupCmd.AddCommand(roleGroupAddCmd)
	roleGroupCmd.AddCommand(roleGroupRmCmd)

	RoleCmd.AddCommand(roleUserCmd)
	roleUserCmd.AddCommand(roleUserLsCmd)
	roleUserCmd.AddCommand(roleUserAddCmd)
	roleUserCmd.AddCommand(roleUserRmCmd)

	roleLsCmd.Flags().StringVar(&roleLsFields, "fields", "id,name,permission_set(id,name,permissions),model_set(id,name,models)", "Fields to display")
	roleLsCmd.Flags().BoolVar(&roleLsPlain, "plain", false, "print without formatting")
	roleLsCmd.Flags().BoolVar(&roleLsCSV, "csv", false, "output in csv format")

	roleCatCmd.Flags().StringVar(&roleCatDir, "dir", "", "Directory to store output file")
	roleCatCmd.Flags().StringVar(&roleCatFields, "fields", "", "Fields to display")
	roleCatCmd.Flags().BoolVar(&roleCatTrim, "trim", false, "Trim output to minimal set of fields")

	roleCreateCmd.Flags().BoolVar(&roleCreatePlain, "plain", false, "Provide minimal response")

	roleGroupLsCmd.Flags().StringVar(&roleGroupLsFields, "fields", "id,name,external_group_id", "Fields to display")
	roleGroupLsCmd.Flags().BoolVar(&roleGroupLsPlain, "plain", false, "print without formatting")
	roleGroupLsCmd.Flags().BoolVar(&roleGroupLsCSV, "csv", false, "output in csv format")

	roleUserLsCmd.Flags().StringVar(&roleUserLsFields, "fields", "id,first_name,last_name,email", "Fields to display")
	roleUserLsCmd.Flags().BoolVar(&roleUserLsPlain, "plain", false, "print without formatting")
	roleUserLsCmd.Flags().BoolVar(&roleUserLsCSV, "csv", false, "output in csv format")
	roleUserLsCmd.Flags().BoolVar(&roleUserLsAll, "all-users", false, "Show users with this role through group membership")
}
