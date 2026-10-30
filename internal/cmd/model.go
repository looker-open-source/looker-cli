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

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	modelLsFields       string
	modelLsPlain        bool
	modelLsCSV          bool
	modelCatFields      string
	modelCatDir         string
	modelCatTrim        bool
	modelImportForce    bool
	modelImportPlain    bool
	modelSetLsFields    string
	modelSetLsPlain     bool
	modelSetLsCSV       bool
	modelSetCatDir      string
	modelSetCatTrim     bool
	modelSetImportForce bool
	modelSetImportPlain bool
)

var ModelCmd = &cobra.Command{
	Use:   "model",
	Short: "Commands pertaining to LookML Models",
}

var modelLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list all models",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		req := v4.RequestAllLookmlModels{
			Fields: &modelLsFields,
		}
		models, err := c.SDK.AllLookmlModels(req, nil)
		if err != nil {
			return fmt.Errorf("failed to list models: %w", err)
		}

		headers := util.ParseFieldsForHeaders(modelLsFields)

		table := util.NewTable(headers)
		for _, m := range models {
			table.Append(extractFields(m, modelLsFields))
		}

		table.Render(modelLsPlain, modelLsCSV)
		return nil
	},
}

var modelCatCmd = &cobra.Command{
	Use:   "cat [MODEL_NAME]",
	Short: "Output the JSON representation of a LookML model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		mName := args[0]
		model, err := c.SDK.LookmlModel(mName, modelCatFields, nil)
		if err != nil {
			return fmt.Errorf("failed to get model %s: %w", mName, err)
		}

		b, _ := json.Marshal(model)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if modelCatTrim {
			keep := map[string]bool{
				"name":                        true,
				"project_name":                true,
				"allowed_db_connection_names": true,
				"unlimited_db_connections":    true,
			}
			for k := range m {
				if !keep[k] {
					delete(m, k)
				}
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if modelCatDir != "" {
			fn := filepath.Join(modelCatDir, fmt.Sprintf("Model_%s.json", mName))
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

var modelImportCmd = &cobra.Command{
	Use:   "import [MODEL_FILE]",
	Short: "Import a LookML model from a JSON file",
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
			return fmt.Errorf("model file missing name")
		}

		var wm v4.WriteLookmlModel
		if err := json.Unmarshal(b, &wm); err != nil {
			return fmt.Errorf("failed to unmarshal WriteLookmlModel: %w", err)
		}

		var existingModel *v4.LookmlModel
		if model, err := c.SDK.LookmlModel(nameVal, "name", nil); err == nil && model.Name != nil {
			existingModel = &model
		}

		var resultModel *v4.LookmlModel
		if existingModel != nil {
			if !modelImportForce {
				return fmt.Errorf("model '%s' already exists. Use --force to overwrite", nameVal)
			}
			updated, err := c.SDK.UpdateLookmlModel(nameVal, wm, nil)
			if err != nil {
				return fmt.Errorf("failed to update model %s: %w", nameVal, err)
			}
			resultModel = &updated
		} else {
			created, err := c.SDK.CreateLookmlModel(wm, nil)
			if err != nil {
				return fmt.Errorf("failed to create model %s: %w", nameVal, err)
			}
			resultModel = &created
		}

		if modelImportPlain {
			fmt.Println(*resultModel.Name)
		} else {
			fmt.Printf("Imported model %s\n", *resultModel.Name)
		}
		return nil
	},
}

var ModelSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Commands pertaining to model sets",
}

var modelSetLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list all model sets",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		sets, err := c.SDK.AllModelSets(modelSetLsFields, nil)
		if err != nil {
			return fmt.Errorf("failed to list model sets: %w", err)
		}

		headers := util.ParseFieldsForHeaders(modelSetLsFields)

		table := util.NewTable(headers)
		for _, s := range sets {
			table.Append(extractFields(s, modelSetLsFields))
		}

		table.Render(modelSetLsPlain, modelSetLsCSV)
		return nil
	},
}

var modelSetCatCmd = &cobra.Command{
	Use:   "cat [MODEL_SET_ID]",
	Short: "Output the JSON representation of a model set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		setID := args[0]
		set, err := c.SDK.ModelSet(setID, "", nil)
		if err != nil {
			return fmt.Errorf("failed to get model set %s: %w", setID, err)
		}

		b, _ := json.Marshal(set)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if modelSetCatTrim {
			keep := map[string]bool{
				"id":       true,
				"name":     true,
				"models":   true,
				"built_in": true,
			}
			for k := range m {
				if !keep[k] {
					delete(m, k)
				}
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if modelSetCatDir != "" {
			name := ""
			if v, ok := m["name"].(string); ok {
				name = v
			}
			fn := filepath.Join(modelSetCatDir, fmt.Sprintf("Model_Set_%s.json", name))
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

var modelSetImportCmd = &cobra.Command{
	Use:   "import [MODEL_SET_FILE]",
	Short: "Import a model set from a JSON file",
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
			return fmt.Errorf("model set file missing name")
		}

		var wms v4.WriteModelSet
		if err := json.Unmarshal(b, &wms); err != nil {
			return fmt.Errorf("failed to unmarshal WriteModelSet: %w", err)
		}

		sets, err := c.SDK.AllModelSets("", nil)
		if err != nil {
			return fmt.Errorf("failed to list model sets: %w", err)
		}

		var existingSet *v4.ModelSet
		for _, s := range sets {
			if s.Name != nil && *s.Name == nameVal {
				existingSet = &s
				break
			}
		}

		var resultSet *v4.ModelSet
		if existingSet != nil {
			if !modelSetImportForce {
				return fmt.Errorf("model set '%s' already exists. Use --force to overwrite", nameVal)
			}
			esID := *existingSet.Id
			updated, err := c.SDK.UpdateModelSet(esID, wms, nil)
			if err != nil {
				return fmt.Errorf("failed to update model set %s: %w", esID, err)
			}
			resultSet = &updated
		} else {
			created, err := c.SDK.CreateModelSet(wms, nil)
			if err != nil {
				return fmt.Errorf("failed to create model set %s: %w", nameVal, err)
			}
			resultSet = &created
		}

		resID := ""
		if resultSet.Id != nil {
			resID = *resultSet.Id
		}

		if modelSetImportPlain {
			fmt.Println(resID)
		} else {
			fmt.Printf("Imported model set %s\n", resID)
		}
		return nil
	},
}

var modelSetRmCmd = &cobra.Command{
	Use:   "rm [MODEL_SET_ID]",
	Short: "Delete a model set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		setID := args[0]
		_, err = c.SDK.DeleteModelSet(setID, nil)
		if err != nil {
			return fmt.Errorf("failed to delete model set %s: %w", setID, err)
		}
		fmt.Printf("Model set %s deleted.\n", setID)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(ModelCmd)
	ModelCmd.AddCommand(modelLsCmd)
	ModelCmd.AddCommand(modelCatCmd)
	ModelCmd.AddCommand(modelImportCmd)

	ModelCmd.AddCommand(ModelSetCmd)
	ModelSetCmd.AddCommand(modelSetLsCmd)
	ModelSetCmd.AddCommand(modelSetCatCmd)
	ModelSetCmd.AddCommand(modelSetImportCmd)
	ModelSetCmd.AddCommand(modelSetRmCmd)

	modelLsCmd.Flags().StringVar(&modelLsFields, "fields", "name,label,project_name", "Fields to display")
	modelLsCmd.Flags().BoolVar(&modelLsPlain, "plain", false, "print without any extra formatting")
	modelLsCmd.Flags().BoolVar(&modelLsCSV, "csv", false, "output in csv format")

	modelCatCmd.Flags().StringVar(&modelCatFields, "fields", "", "Fields to display")
	modelCatCmd.Flags().StringVar(&modelCatDir, "dir", "", "Directory to store output file")
	modelCatCmd.Flags().BoolVar(&modelCatTrim, "trim", false, "Trim output to minimal set of fields")

	modelImportCmd.Flags().BoolVar(&modelImportForce, "force", false, "Overwrite existing model")
	modelImportCmd.Flags().BoolVar(&modelImportPlain, "plain", false, "Output only model name")

	modelSetLsCmd.Flags().StringVar(&modelSetLsFields, "fields", "id,name,models", "Fields to display")
	modelSetLsCmd.Flags().BoolVar(&modelSetLsPlain, "plain", false, "print without any extra formatting")
	modelSetLsCmd.Flags().BoolVar(&modelSetLsCSV, "csv", false, "output in csv format")

	modelSetCatCmd.Flags().StringVar(&modelSetCatDir, "dir", "", "Directory to store output file")
	modelSetCatCmd.Flags().BoolVar(&modelSetCatTrim, "trim", false, "Trim output to minimal set of fields")

	modelSetImportCmd.Flags().BoolVar(&modelSetImportForce, "force", false, "Overwrite existing model set")
	modelSetImportCmd.Flags().BoolVar(&modelSetImportPlain, "plain", false, "Output only model set ID")
}
