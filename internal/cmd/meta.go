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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	metaTreeNoun   string
	metaTreeOutput string
	metaSearchOut  string
)

var MetaCmd = &cobra.Command{
	Use:   "meta",
	Short: "Meta commands for CLI discovery",
	Long:  `Meta commands to discover available commands, their structures, and search through them.`,
}

type FlagInfo struct {
	Name       string `json:"name"`
	Shorthand  string `json:"shorthand,omitempty"`
	Usage      string `json:"usage"`
	DefaultVal string `json:"default_value,omitempty"`
}

type CommandNode struct {
	Name        string        `json:"name"`
	Use         string        `json:"use"`
	Short       string        `json:"short"`
	Long        string        `json:"long"`
	Aliases     []string      `json:"aliases,omitempty"`
	Flags       []FlagInfo    `json:"flags,omitempty"`
	Subcommands []CommandNode `json:"subcommands,omitempty"`
}

var metaTreeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Output the Cobra command tree",
	RunE: func(cmd *cobra.Command, args []string) error {
		targetCmd := RootCmd
		if metaTreeNoun != "" {
			found, _, err := RootCmd.Find(strings.Fields(metaTreeNoun))
			if err != nil || found == nil {
				return fmt.Errorf("noun %q not found: %w", metaTreeNoun, err)
			}
			targetCmd = found
		}

		if metaTreeOutput == "json" {
			node := exportCmd(targetCmd)
			bytes, err := json.MarshalIndent(node, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal to json: %w", err)
			}
			fmt.Println(string(bytes))
		} else {
			printCmdTree(targetCmd, "")
		}
		return nil
	},
}

var metaSearchCmd = &cobra.Command{
	Use:   "search [keyword]",
	Short: "Search commands",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword := args[0]
		matches := searchCmds(RootCmd, keyword)

		if metaSearchOut == "json" {
			var nodes []CommandNode
			for _, m := range matches {
				nodes = append(nodes, exportCmd(m))
			}
			bytes, err := json.MarshalIndent(nodes, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal to json: %w", err)
			}
			fmt.Println(string(bytes))
		} else {
			if len(matches) == 0 {
				fmt.Printf("No commands found matching %q\n", keyword)
				return nil
			}
			fmt.Printf("Found %d matching commands:\n", len(matches))
			for _, m := range matches {
				aliases := ""
				if len(m.Aliases) > 0 {
					aliases = fmt.Sprintf(" (aliases: %s)", strings.Join(m.Aliases, ", "))
				}
				fmt.Printf("  %s%s - %s\n", m.CommandPath(), aliases, m.Short)
			}
		}
		return nil
	},
}

func exportCmd(cmd *cobra.Command) CommandNode {
	node := CommandNode{
		Name:  cmd.Name(),
		Use:   cmd.Use,
		Short: cmd.Short,
		Long:  cmd.Long,
	}
	node.Aliases = cmd.Aliases

	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		node.Flags = append(node.Flags, FlagInfo{
			Name:       flag.Name,
			Shorthand:  flag.Shorthand,
			Usage:      flag.Usage,
			DefaultVal: flag.DefValue,
		})
	})

	for _, sub := range cmd.Commands() {
		// Avoid infinite recursion if meta is somehow registered under itself,
		// though it shouldn't be.
		if sub.Name() == "help" {
			continue
		}
		node.Subcommands = append(node.Subcommands, exportCmd(sub))
	}

	return node
}

func printCmdTree(cmd *cobra.Command, indent string) {
	name := cmd.Name()
	if len(cmd.Aliases) > 0 {
		name = fmt.Sprintf("%s (%s)", name, strings.Join(cmd.Aliases, ", "))
	}

	if indent == "" {
		fmt.Println(name)
	}

	subCommands := cmd.Commands()
	// Filter out "help" command to keep tree clean
	var filteredSubs []*cobra.Command
	for _, sub := range subCommands {
		if sub.Name() != "help" {
			filteredSubs = append(filteredSubs, sub)
		}
	}

	for i, sub := range filteredSubs {
		isLast := i == len(filteredSubs)-1
		prefix := "├── "
		childIndent := indent + "│   "
		if isLast {
			prefix = "└── "
			childIndent = indent + "    "
		}
		fmt.Println(indent + prefix + sub.Name())
		printCmdTree(sub, childIndent)
	}
}

func searchCmds(cmd *cobra.Command, keyword string) []*cobra.Command {
	var matches []*cobra.Command
	if cmd.Name() != "help" && matchesKeyword(cmd, keyword) {
		matches = append(matches, cmd)
	}
	for _, sub := range cmd.Commands() {
		matches = append(matches, searchCmds(sub, keyword)...)
	}
	return matches
}

func matchesKeyword(cmd *cobra.Command, keyword string) bool {
	keyword = strings.ToLower(keyword)
	if strings.Contains(strings.ToLower(cmd.Name()), keyword) {
		return true
	}
	if strings.Contains(strings.ToLower(cmd.Use), keyword) {
		return true
	}
	if strings.Contains(strings.ToLower(cmd.Short), keyword) {
		return true
	}
	if strings.Contains(strings.ToLower(cmd.Long), keyword) {
		return true
	}
	for _, alias := range cmd.Aliases {
		if strings.Contains(strings.ToLower(alias), keyword) {
			return true
		}
	}
	return false
}

func init() {
	RootCmd.AddCommand(MetaCmd)
	MetaCmd.AddCommand(metaTreeCmd)
	MetaCmd.AddCommand(metaSearchCmd)

	metaTreeCmd.Flags().StringVar(&metaTreeNoun, "noun", "", "Start tree from this noun")
	metaTreeCmd.Flags().StringVar(&metaTreeOutput, "output", "", "Output format (e.g. json)")
	metaSearchCmd.Flags().StringVar(&metaSearchOut, "output", "", "Output format (e.g. json)")
}
