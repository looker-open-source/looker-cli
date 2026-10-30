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
	"net/url"
	"path/filepath"
	"strings"

	"github.com/looker-open-source/looker-cli/internal/util"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	connectionLsFields       string
	connectionLsPlain        bool
	connectionLsCSV          bool
	connectionDialectsFields string
	connectionDialectsPlain  bool
	connectionDialectsCSV    bool
	connectionCatFields      string
	connectionCatDir         string
	connectionCatTrim        bool
	connectionImportForce    bool
	connectionImportPlain    bool
	connectionTestTests      string
	connectionTestPlain      bool
	connectionTestCSV        bool
)

var ConnectionCmd = &cobra.Command{
	Use:   "connection",
	Short: "Commands pertaining to database connections and dialects",
}

var connectionLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list all database connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		connections, err := c.SDK.AllConnections(connectionLsFields, nil)
		if err != nil {
			return fmt.Errorf("failed to list connections: %w", err)
		}

		headers := util.ParseFieldsForHeaders(connectionLsFields)

		table := util.NewTable(headers)
		for _, conn := range connections {
			table.Append(extractFields(conn, connectionLsFields))
		}

		table.Render(connectionLsPlain, connectionLsCSV)
		return nil
	},
}

var connectionDialectsCmd = &cobra.Command{
	Use:   "dialects",
	Short: "list all supported sql dialects",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}

		dialects, err := c.SDK.AllDialectInfos(connectionDialectsFields, nil)
		if err != nil {
			return fmt.Errorf("failed to list dialects: %w", err)
		}

		headers := strings.Split(connectionDialectsFields, ",")
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		table := util.NewTable(headers)
		for _, d := range dialects {
			table.Append(extractFields(d, connectionDialectsFields))
		}

		table.Render(connectionDialectsPlain, connectionDialectsCSV)
		return nil
	},
}

var connectionCatCmd = &cobra.Command{
	Use:   "cat [CONNECTION_NAME]",
	Short: "Output the JSON representation of a database connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		cName := args[0]
		conn, err := c.SDK.Connection(cName, connectionCatFields, nil)
		if err != nil {
			return fmt.Errorf("failed to get connection %s: %w", cName, err)
		}

		b, _ := json.Marshal(conn)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)

		if connectionCatTrim {
			remove := []string{
				"can", "dialect", "snippets", "pdts_enabled", "created_at", "user_id",
				"example", "last_regen_at", "last_reap_at", "managed",
				"named_driver_version_actual", "has_password", "uses_oauth",
				"uses_instance_oauth", "uses_service_auth", "supports_data_studio_link",
				"default_bq_connection", "p4sa_name",
			}
			for _, k := range remove {
				delete(m, k)
			}
		}

		outBytes, _ := json.MarshalIndent(m, "", "  ")
		if connectionCatDir != "" {
			fn := filepath.Join(connectionCatDir, fmt.Sprintf("Connection_%s.json", cName))
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

var connectionRmCmd = &cobra.Command{
	Use:   "rm [CONNECTION_NAME]",
	Short: "Delete a database connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		cName := args[0]
		_, err = c.SDK.DeleteConnection(cName, nil)
		if err != nil {
			return fmt.Errorf("failed to delete connection %s: %w", cName, err)
		}
		fmt.Printf("Connection %s deleted.\n", cName)
		return nil
	},
}

var connectionImportCmd = &cobra.Command{
	Use:   "import [CONNECTION_FILE]",
	Short: "Import a database connection from a JSON file",
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
			return fmt.Errorf("connection file missing name")
		}

		var wc v4.WriteDBConnection
		if err := json.Unmarshal(b, &wc); err != nil {
			return fmt.Errorf("failed to unmarshal WriteDBConnection: %w", err)
		}

		var existingConn *v4.DBConnection
		if conn, err := c.SDK.Connection(nameVal, "", nil); err == nil && conn.Name != nil {
			existingConn = &conn
		}

		var resultConn *v4.DBConnection
		if existingConn != nil {
			if !connectionImportForce {
				return fmt.Errorf("connection '%s' already exists. Use --force to overwrite", nameVal)
			}
			updated, err := c.SDK.UpdateConnection(nameVal, wc, nil)
			if err != nil {
				return fmt.Errorf("failed to update connection %s: %w", nameVal, err)
			}
			resultConn = &updated
		} else {
			created, err := c.SDK.CreateConnection(wc, nil)
			if err != nil {
				return fmt.Errorf("failed to create connection %s: %w", nameVal, err)
			}
			resultConn = &created
		}

		if connectionImportPlain {
			fmt.Println(*resultConn.Name)
		} else {
			fmt.Printf("Imported connection %s\n", *resultConn.Name)
		}
		return nil
	},
}

var connectionTestCmd = &cobra.Command{
	Use:   "test [CONNECTION_NAME]",
	Short: "Test a database connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := initClient(cmd.Context(), false)
		if err != nil {
			return err
		}
		cName := args[0]
		testsSlice := strings.Split(connectionTestTests, ",")
		for i := range testsSlice {
			testsSlice[i] = strings.TrimSpace(testsSlice[i])
		}

		var results []v4.DBConnectionTestResult
		path := fmt.Sprintf("/connections/%s/test", url.PathEscape(cName))
		err = c.SDK.AuthSession.Do(&results, "PUT", "/4.0", path, map[string]interface{}{"tests": strings.Join(testsSlice, ",")}, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to test connection %s: %w", cName, err)
		}

		headers := []string{"Name", "Status", "Message"}
		table := util.NewTable(headers)
		failed := 0
		for _, r := range results {
			n := ""
			if r.Name != nil {
				n = *r.Name
			}
			s := ""
			if r.Status != nil {
				s = *r.Status
			}
			msg := ""
			if r.Message != nil {
				msg = *r.Message
			}
			if strings.EqualFold(s, "error") {
				failed++
			}
			table.Append([]string{n, s, msg})
		}
		table.Render(connectionTestPlain, connectionTestCSV)
		if failed > 0 {
			return fmt.Errorf("%d connection test(s) failed for %s", failed, cName)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(ConnectionCmd)
	ConnectionCmd.AddCommand(connectionLsCmd)
	ConnectionCmd.AddCommand(connectionDialectsCmd)
	ConnectionCmd.AddCommand(connectionCatCmd)
	ConnectionCmd.AddCommand(connectionRmCmd)
	ConnectionCmd.AddCommand(connectionImportCmd)
	ConnectionCmd.AddCommand(connectionTestCmd)

	connectionLsCmd.Flags().StringVar(&connectionLsFields, "fields", "name,dialect(name),host,port,database,schema", "Fields to display")
	connectionLsCmd.Flags().BoolVar(&connectionLsPlain, "plain", false, "print without any extra formatting")
	connectionLsCmd.Flags().BoolVar(&connectionLsCSV, "csv", false, "output in csv format")

	connectionDialectsCmd.Flags().StringVar(&connectionDialectsFields, "fields", "name,label", "Fields to display")
	connectionDialectsCmd.Flags().BoolVar(&connectionDialectsPlain, "plain", false, "print without any extra formatting")
	connectionDialectsCmd.Flags().BoolVar(&connectionDialectsCSV, "csv", false, "output in csv format")

	connectionCatCmd.Flags().StringVar(&connectionCatFields, "fields", "", "Fields to display")
	connectionCatCmd.Flags().StringVar(&connectionCatDir, "dir", "", "Directory to store output file")
	connectionCatCmd.Flags().BoolVar(&connectionCatTrim, "trim", false, "Trim output to minimal set of fields")

	connectionImportCmd.Flags().BoolVar(&connectionImportForce, "force", false, "Overwrite existing connection")
	connectionImportCmd.Flags().BoolVar(&connectionImportPlain, "plain", false, "Output only connection name")

	connectionTestCmd.Flags().StringVar(&connectionTestTests, "tests", "connect", "Comma-separated tests to run")
	connectionTestCmd.Flags().BoolVar(&connectionTestPlain, "plain", false, "print without any extra formatting")
	connectionTestCmd.Flags().BoolVar(&connectionTestCSV, "csv", false, "output in csv format")
}
