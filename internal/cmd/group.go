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
	"fmt"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	groupLsFields           string
	groupLsPlain            bool
	groupLsCSV              bool
	groupMemberGroupsFields string
	groupMemberGroupsPlain  bool
	groupMemberGroupsCSV    bool
	groupMemberUsersFields  string
	groupMemberUsersPlain   bool
	groupMemberUsersCSV     bool
)

var GroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Commands pertaining to groups",
}

var groupLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list all groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		var allGroups []v4.Group
		var limit int64 = 64
		var offset int64 = 0

		for {
			req := v4.RequestAllGroups{
				Fields: &groupLsFields,
				Limit:  &limit,
				Offset: &offset,
			}
			groups, err := c.SDK.AllGroups(req, nil)
			if err != nil {
				return fmt.Errorf("failed to list groups: %w", err)
			}
			allGroups = append(allGroups, groups...)
			if int64(len(groups)) < limit {
				break
			}
			offset += limit
		}

		headers := util.ParseFieldsForHeaders(groupLsFields)

		table := util.NewTable(headers)
		for _, g := range allGroups {
			table.Append(extractFields(g, groupLsFields))
		}

		table.Render(groupLsPlain, groupLsCSV)
		return nil
	},
}

var groupMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage group members",
}

var groupMemberGroupsCmd = &cobra.Command{
	Use:   "groups [GROUP_ID]",
	Short: "list the groups that have been added as members of the given group id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		groupID := args[0]
		groups, err := c.SDK.AllGroupGroups(groupID, groupMemberGroupsFields, nil)
		if err != nil {
			return fmt.Errorf("failed to get member groups for %s: %w", groupID, err)
		}

		headers := strings.Split(groupMemberGroupsFields, ",")
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		table := util.NewTable(headers)
		for _, g := range groups {
			table.Append(extractFields(g, groupMemberGroupsFields))
		}

		table.Render(groupMemberGroupsPlain, groupMemberGroupsCSV)
		return nil
	},
}

var groupMemberUsersCmd = &cobra.Command{
	Use:   "users [GROUP_ID]",
	Short: "list the users that have been added as members of the given group id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		groupID := args[0]

		var allUsers []v4.User
		var limit int64 = 64
		var offset int64 = 0

		for {
			req := v4.RequestAllGroupUsers{
				GroupId: groupID,
				Fields:  &groupMemberUsersFields,
				Limit:   &limit,
				Offset:  &offset,
			}
			users, err := c.SDK.AllGroupUsers(req, nil)
			if err != nil {
				return fmt.Errorf("failed to list member users for %s: %w", groupID, err)
			}
			allUsers = append(allUsers, users...)
			if int64(len(users)) < limit {
				break
			}
			offset += limit
		}

		headers := strings.Split(groupMemberUsersFields, ",")
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		table := util.NewTable(headers)
		for _, u := range allUsers {
			table.Append(extractFields(u, groupMemberUsersFields))
		}

		table.Render(groupMemberUsersPlain, groupMemberUsersCSV)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(GroupCmd)
	GroupCmd.AddCommand(groupLsCmd)

	GroupCmd.AddCommand(groupMemberCmd)
	groupMemberCmd.AddCommand(groupMemberGroupsCmd)
	groupMemberCmd.AddCommand(groupMemberUsersCmd)

	groupLsCmd.Flags().StringVar(&groupLsFields, "fields", "id,name,user_count,contains_current_user,externally_managed,external_group_id", "Fields to display")
	groupLsCmd.Flags().BoolVar(&groupLsPlain, "plain", false, "print without any extra formatting")
	groupLsCmd.Flags().BoolVar(&groupLsCSV, "csv", false, "output in csv format")

	groupMemberGroupsCmd.Flags().StringVar(&groupMemberGroupsFields, "fields", "id,name,user_count,contains_current_user,externally_managed,external_group_id", "Fields to display")
	groupMemberGroupsCmd.Flags().BoolVar(&groupMemberGroupsPlain, "plain", false, "print without any extra formatting")
	groupMemberGroupsCmd.Flags().BoolVar(&groupMemberGroupsCSV, "csv", false, "output in csv format")

	groupMemberUsersCmd.Flags().StringVar(&groupMemberUsersFields, "fields", "id,email,last_name,first_name,personal_folder_id,home_folder_id", "Fields to display")
	groupMemberUsersCmd.Flags().BoolVar(&groupMemberUsersPlain, "plain", false, "print without any extra formatting")
	groupMemberUsersCmd.Flags().BoolVar(&groupMemberUsersCSV, "csv", false, "output in csv format")
}
