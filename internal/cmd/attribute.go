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
	"strconv"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/util"
	"github.com/spf13/cobra"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

var (
	attributeLsFields       string
	attributeLsPlain        bool
	attributeLsCSV          bool
	attributeCatFields      string
	attributeCatDir         string
	attributeRmPlain        bool
	attributeImportPlain    bool
	attributeImportForce    bool
	attributeCreatePlain    bool
	attributeCreateForce    bool
	attributeCreateType     string
	attributeCreateDefault  string
	attributeCreateIsHidden bool
	attributeCreateCanView  bool
	attributeCreateCanEdit  bool
	attributeCreateDomain   string
)

var AttributeCmd = &cobra.Command{
	Use:   "attribute",
	Short: "Commands pertaining to user attributes",
}

func resolveGroupID(c *client.ClientWrapper, arg string) (string, error) {
	if _, err := strconv.ParseInt(arg, 10, 64); err == nil {
		return arg, nil
	}
	groups, err := c.SDK.SearchGroups(v4.RequestSearchGroups{Name: &arg, Fields: ptr("id")}, nil)
	if err != nil { return "", err }
	if len(groups) == 0 { return "", fmt.Errorf("no group found with name %s", arg) }
	if len(groups) > 1 { return "", fmt.Errorf("multiple groups found with name %s", arg) }
	if groups[0].Id == nil { return "", fmt.Errorf("invalid group id") }
	return *groups[0].Id, nil
}

func resolveAttrID(c *client.ClientWrapper, arg string) (string, error) {
	if _, err := strconv.ParseInt(arg, 10, 64); err == nil {
		return arg, nil
	}
	attrs, err := c.SDK.AllUserAttributes(v4.RequestAllUserAttributes{Fields: ptr("id,name")}, nil)
	if err != nil { return "", err }
	for _, a := range attrs {
		if a.Name == arg && a.Id != nil {
			return *a.Id, nil
		}
	}
	return "", fmt.Errorf("user attribute %s not found", arg)
}

var attributeLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all defined user attributes",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }

		attrs, err := c.SDK.AllUserAttributes(v4.RequestAllUserAttributes{Fields: &attributeLsFields}, nil)
		if err != nil { return err }

		headers := util.ParseFieldsForHeaders(attributeLsFields)

		table := util.NewTable(headers)
		for _, a := range attrs {
			table.Append(extractFields(a, attributeLsFields))
		}
		table.Render(attributeLsPlain, attributeLsCSV)
		return nil
	},
}

var attributeCatCmd = &cobra.Command{
	Use:   "cat [ATTR_ID|ATTR_NAME]",
	Short: "Output json information about an attribute",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		aID, err := resolveAttrID(c, args[0])
		if err != nil { return err }

		attr, err := c.SDK.UserAttribute(aID, attributeCatFields, nil)
		if err != nil { return err }

		bytes, _ := json.MarshalIndent(attr, "", "  ")
		if attributeCatDir != "" {
			fn := filepath.Join(attributeCatDir, fmt.Sprintf("Attribute_%s_%s.json", aID, sanitizeFilename(attr.Name)))
			if err := os.WriteFile(fn, bytes, 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", fn, err)
			}
			fmt.Printf("Wrote %s\n", fn)
		} else {
			fmt.Println(string(bytes))
		}
		return nil
	},
}

var attributeRmCmd = &cobra.Command{
	Use:   "rm [ATTR_ID|ATTR_NAME]",
	Short: "Delete a user attribute",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		aID, err := resolveAttrID(c, args[0])
		if err != nil { return err }

		_, err = c.SDK.DeleteUserAttribute(aID, nil)
		if err != nil { return err }
		if !attributeRmPlain {
			fmt.Printf("User attribute %s deleted.\n", aID)
		}
		return nil
	},
}

var attributeSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set attribute values",
}

var attributeSetGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Set attribute values for groups",
}

var attributeSetGroupValueCmd = &cobra.Command{
	Use:   "value [GROUP] [ATTR] [VALUE]",
	Short: "Set a user attribute value for a group",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		gID, err := resolveGroupID(c, args[0])
		if err != nil { return err }
		aID, err := resolveAttrID(c, args[1])
		if err != nil { return err }
		val := args[2]

		body := v4.UserAttributeGroupValue{Value: &val}
		res, err := c.SDK.UpdateUserAttributeGroupValue(gID, aID, body, nil)
		if err != nil { return err }
		resID := aID
		if res.Id != nil {
			resID = *res.Id
		}
		displayVal := "<hidden>"
		if res.Value != nil {
			displayVal = *res.Value
		}
		fmt.Printf("Group attribute %s set to %s\n", resID, displayVal)
		return nil
	},
}

var attributeGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get attribute values",
}

var attributeGetGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Get attribute values for groups",
}

var attributeGetGroupValueCmd = &cobra.Command{
	Use:   "value [GROUP] [ATTR]",
	Short: "Retrieve a user attribute value for a group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		gID, err := resolveGroupID(c, args[0])
		if err != nil { return err }
		aID, err := resolveAttrID(c, args[1])
		if err != nil { return err }

		values, err := c.SDK.AllUserAttributeGroupValues(aID, "", nil)
		if err != nil { return err }

		for _, v := range values {
			if v.GroupId != nil && *v.GroupId == gID && v.Value != nil {
				fmt.Println(*v.Value)
				return nil
			}
		}
		return fmt.Errorf("attribute value not set for group %s", gID)
	},
}

var attributeCreateCmd = &cobra.Command{
	Use:   "create [ATTR_NAME] [ATTR_LABEL]",
	Short: "Create or modify an attribute",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		name := args[0]
		label := name
		if len(args) > 1 { label = args[1] }

		var existingAttr *v4.UserAttribute
		attrs, err := c.SDK.AllUserAttributes(v4.RequestAllUserAttributes{Fields: ptr("id,name,label,is_system")}, nil)
		if err != nil {
			return fmt.Errorf("failed to list user attributes: %w", err)
		}
		for i := range attrs {
			if attrs[i].Name == name {
				existingAttr = &attrs[i]
				break
			}
		}

		wua := v4.WriteUserAttribute{
			Name:          name,
			Label:         label,
			Type:          attributeCreateType,
			ValueIsHidden: &attributeCreateIsHidden,
			UserCanView:   &attributeCreateCanView,
			UserCanEdit:   &attributeCreateCanEdit,
		}
		if attributeCreateDefault != "" { wua.DefaultValue = &attributeCreateDefault }
		if attributeCreateDomain != "" && attributeCreateIsHidden { wua.HiddenValueDomainWhitelist = &attributeCreateDomain }

		var resultAttr *v4.UserAttribute

		if existingAttr != nil {
			if existingAttr.IsSystem != nil && *existingAttr.IsSystem {
				return fmt.Errorf("attribute %s is a system attribute and cannot be modified", name)
			}
			if !attributeCreateForce {
				return fmt.Errorf("attribute %s already exists. Use --force to modify", name)
			}
			if existingAttr.Id == nil {
				return fmt.Errorf("existing attribute %s has no id", name)
			}
			aID := *existingAttr.Id
			updated, err := c.SDK.UpdateUserAttribute(aID, wua, "", nil)
			if err != nil { return err }
			resultAttr = &updated
		} else {
			created, err := c.SDK.CreateUserAttribute(wua, "", nil)
			if err != nil { return err }
			resultAttr = &created
		}

		resID := getStringValue(resultAttr.Id)
		if attributeCreatePlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Attribute %s (%s) created/modified.\n", resID, resultAttr.Name)
		}
		return nil
	},
}

var attributeImportCmd = &cobra.Command{
	Use:   "import [FILE]",
	Short: "Import a user attribute from a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil { return err }
		file := args[0]

		b, err := util.ReadFileOrStdin(file)
		if err != nil { return err }

		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil { return err }

		name, _ := m["name"].(string)
		if name == "" {
			return fmt.Errorf("attribute file missing name")
		}

		var existingAttr *v4.UserAttribute
		attrs, err := c.SDK.AllUserAttributes(v4.RequestAllUserAttributes{Fields: ptr("id,name,label,is_system")}, nil)
		if err != nil {
			return fmt.Errorf("failed to list user attributes: %w", err)
		}
		for i := range attrs {
			if attrs[i].Name == name {
				existingAttr = &attrs[i]
				break
			}
		}

		mb, _ := json.Marshal(m)
		var wua v4.WriteUserAttribute
		_ = json.Unmarshal(mb, &wua)

		var resultAttr *v4.UserAttribute

		if existingAttr != nil {
			if existingAttr.IsSystem != nil && *existingAttr.IsSystem {
				return fmt.Errorf("attribute %s is a system attribute and cannot be modified", name)
			}
			if !attributeImportForce {
				return fmt.Errorf("attribute %s already exists. Use --force to modify", name)
			}
			if existingAttr.Id == nil {
				return fmt.Errorf("existing attribute %s has no id", name)
			}
			aID := *existingAttr.Id
			updated, err := c.SDK.UpdateUserAttribute(aID, wua, "", nil)
			if err != nil { return err }
			resultAttr = &updated
		} else {
			created, err := c.SDK.CreateUserAttribute(wua, "", nil)
			if err != nil { return err }
			resultAttr = &created
		}

		resID := getStringValue(resultAttr.Id)
		if attributeImportPlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Imported attribute %s (%s)\n", resID, resultAttr.Name)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(AttributeCmd)
	AttributeCmd.AddCommand(attributeLsCmd)
	AttributeCmd.AddCommand(attributeCatCmd)
	AttributeCmd.AddCommand(attributeRmCmd)
	AttributeCmd.AddCommand(attributeCreateCmd)
	AttributeCmd.AddCommand(attributeImportCmd)

	AttributeCmd.AddCommand(attributeSetCmd)
	attributeSetCmd.AddCommand(attributeSetGroupCmd)
	attributeSetGroupCmd.AddCommand(attributeSetGroupValueCmd)

	AttributeCmd.AddCommand(attributeGetCmd)
	attributeGetCmd.AddCommand(attributeGetGroupCmd)
	attributeGetGroupCmd.AddCommand(attributeGetGroupValueCmd)

	attributeLsCmd.Flags().StringVar(&attributeLsFields, "fields", "id,name,label,type,default_value", "Fields to display")
	attributeLsCmd.Flags().BoolVar(&attributeLsPlain, "plain", false, "print without formatting")
	attributeLsCmd.Flags().BoolVar(&attributeLsCSV, "csv", false, "output in csv format")

	attributeCatCmd.Flags().StringVar(&attributeCatFields, "fields", "", "Fields to display")
	attributeCatCmd.Flags().StringVar(&attributeCatDir, "dir", "", "Directory to store output file")

	attributeRmCmd.Flags().BoolVar(&attributeRmPlain, "plain", false, "Provide minimal response")

	attributeCreateCmd.Flags().BoolVar(&attributeCreatePlain, "plain", false, "Provide minimal response")
	attributeCreateCmd.Flags().BoolVar(&attributeCreateForce, "force", false, "Modify if exists")
	attributeCreateCmd.Flags().StringVar(&attributeCreateType, "type", "string", "Attribute type")
	attributeCreateCmd.Flags().StringVar(&attributeCreateDefault, "default-value", "", "Default value")
	attributeCreateCmd.Flags().BoolVar(&attributeCreateIsHidden, "is-hidden", false, "Is hidden")
	attributeCreateCmd.Flags().BoolVar(&attributeCreateCanView, "can-view", true, "User can view")
	attributeCreateCmd.Flags().BoolVar(&attributeCreateCanEdit, "can-edit", true, "User can edit")
	attributeCreateCmd.Flags().StringVar(&attributeCreateDomain, "domain-allowlist", "", "Domain allowlist")

	attributeImportCmd.Flags().BoolVar(&attributeImportPlain, "plain", false, "Provide minimal response")
	attributeImportCmd.Flags().BoolVar(&attributeImportForce, "force", false, "Modify if exists")
}
