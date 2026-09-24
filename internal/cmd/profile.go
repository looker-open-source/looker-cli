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
	"time"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	profHost         string
	profPort         string
	profClientID     string
	profClientSecret string
	profToken        string
	profRefreshToken string
)

func init() {
	profileCmd.AddCommand(profileLsCmd)
	profileCmd.AddCommand(profileAddCmd)
	profileCmd.AddCommand(profileUseCmd)
	profileCmd.AddCommand(profileRmCmd)

	profileAddCmd.Flags().StringVar(&profHost, "host", "", "Looker Host (required)")
	profileAddCmd.Flags().StringVar(&profPort, "port", "19999", "Looker API Port")
	profileAddCmd.Flags().StringVar(&profClientID, "client-id", "", "API Client Id")
	profileAddCmd.Flags().StringVar(&profClientSecret, "client-secret", "", "API Client Secret")
	profileAddCmd.Flags().StringVar(&profToken, "token", "", "Access token")
	profileAddCmd.Flags().StringVar(&profRefreshToken, "refresh-token", "", "Refresh token")
	_ = profileAddCmd.MarkFlagRequired("host")

	RootCmd.AddCommand(profileCmd)
}

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage Looker profiles",
	Long:  `Manage Looker profiles in config.yaml`,
}

var profileLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if len(cfg.Profiles) == 0 {
			fmt.Println("No profiles found.")
			return nil
		}

		for name, prof := range cfg.Profiles {
			defMarker := " "
			if name == cfg.Default {
				defMarker = "*"
			}
			fmt.Printf("%s %s (%s:%s)\n", defMarker, name, prof.Host, prof.Port)
		}
		return nil
	},
}

var profileAddCmd = &cobra.Command{
	Use:   "add [name]",
	Short: "Add a new profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if _, exists := cfg.Profiles[name]; exists {
			return fmt.Errorf("profile %q already exists", name)
		}

		var ssl *bool
		if flag := cmd.Flags().Lookup("ssl"); flag != nil && flag.Changed {
			val, _ := cmd.Flags().GetBool("ssl")
			ssl = &val
		}

		var verifySSL *bool
		if flag := cmd.Flags().Lookup("verify-ssl"); flag != nil && flag.Changed {
			val, _ := cmd.Flags().GetBool("verify-ssl")
			verifySSL = &val
		}

		refreshExp := ""
		if profRefreshToken != "" {
			refreshExp = time.Now().Add(client.RefreshTokenDuration).Format(client.TimeFormat)
		}

		prof := config.Profile{
			Host:              profHost,
			Port:              profPort,
			ClientID:          profClientID,
			ClientSecret:      profClientSecret,
			AccessToken:       profToken,
			RefreshToken:      profRefreshToken,
			RefreshExpiration: refreshExp,
			SSL:               ssl,
			VerifySSL:         verifySSL,
		}

		cfg.Profiles[name] = prof

		if cfg.Default == "" {
			cfg.Default = name
		}

		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Printf("Profile %q added.\n", name)
		return nil
	},
}

var profileUseCmd = &cobra.Command{
	Use:   "use [name]",
	Short: "Set a profile as default",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if _, exists := cfg.Profiles[name]; !exists {
			return fmt.Errorf("profile %q does not exist", name)
		}

		cfg.Default = name
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Printf("Using profile %q as default.\n", name)
		return nil
	},
}

var profileRmCmd = &cobra.Command{
	Use:   "rm [name]",
	Short: "Delete a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		if _, exists := cfg.Profiles[name]; !exists {
			return fmt.Errorf("profile %q does not exist", name)
		}

		delete(cfg.Profiles, name)

		if cfg.Default == name {
			cfg.Default = ""
			// Set another profile as default if available
			for k := range cfg.Profiles {
				cfg.Default = k
				break
			}
		}

		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Printf("Profile %q deleted.\n", name)
		return nil
	},
}
