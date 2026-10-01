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

	"github.com/spf13/cobra"
	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/config"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

var (
	sessionLoginOAuth bool
	sessionLoginText  bool
)

var SessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Commands pertaining to sessions",
}

var sessionLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Create a persistent session",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// If oauth is set, NewClient handles it if we pass oauth=true
		// But wait, NewClient creates a client and stores token if oauth=true.
		// If we want standard login, NewClient uses client_id/secret but doesn't explicitly call Login() to get token details unless we tell it to, OR we call ExplicitLogin.
		// Let's see: if oauth, initClient(oauth=true) already performed login and stored token!
		if sessionLoginOAuth {
			_, err := initClient(ctx, true)
			if err != nil {
				return err
			}
			if !sessionLoginText {
				fmt.Println("OAuth login successful and token stored.")
			}
			return nil
		}

		// Standard login (client_id/secret or netrc)
		// We initialize client without oauth, then call ExplicitLogin to get token & expiration to store.
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}

		// ExplicitLogin uses clientID/secret from settings
		// Where do we get clientID/secret? c.Session.Config has them if they were set.
		cID := c.Session.Config.ClientId
		cSec := c.Session.Config.ClientSecret
		if cID == "" || cSec == "" {
			return fmt.Errorf("login requires client_id and client_secret (via flags, env, or netrc)")
		}

		tok, exp, err := c.ExplicitLogin(cID, cSec)
		if err != nil {
			return fmt.Errorf("explicit login failed: %w", err)
		}

		if sessionLoginText {
			fmt.Println(tok)
			return nil
		}

		if c.ActiveProfile != "" {
			cfg, err := config.Load()
			if err == nil {
				prof := cfg.Profiles[c.ActiveProfile]
				prof.AccessToken = tok
				prof.Expiration = exp.Format(client.TimeFormat)
				cfg.Profiles[c.ActiveProfile] = prof
				err = cfg.Save()
			}
			if err != nil {
				return fmt.Errorf("failed to store token in profile: %w", err)
			}
		} else {
			err = client.StoreToken(c.Host, c.SuUser, tok, "", "", exp)
			if err != nil {
				return fmt.Errorf("failed to store token: %w", err)
			}
		}

		fmt.Println("Login successful and token stored.")
		return nil
	},
}

var sessionLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "End a persistent session",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		activeProfile := cfgProfile
		host := cfgHost
		if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil {
			if activeProfile == "" {
				activeProfile = cfg.Default
			}
			if activeProfile != "" {
				if p, ok := cfg.Profiles[activeProfile]; ok && !RootCmd.PersistentFlags().Lookup("host").Changed && p.Host != "" {
					host = p.Host
				}
			}
		}

		origTokenFile := cfgTokenFile
		if !cfgTokenFile {
			if _, tokErr := client.GetTokenEntry(host, cfgSuUser); tokErr == nil {
				cfgTokenFile = true
			} else if cfgSuUser != "" {
				if _, tokErr := client.GetTokenEntry(host, ""); tokErr == nil {
					cfgTokenFile = true
				}
			}
		}
		defer func() { cfgTokenFile = origTokenFile }()

		c, err := initClient(ctx, false)
		if err == nil && c != nil {
			_ = c.Logout()
			if c.ActiveProfile != "" {
				activeProfile = c.ActiveProfile
			}
			if c.Host != "" {
				host = c.Host
			}
		}

		if activeProfile != "" {
			client.ClearProfileToken(activeProfile)
		}
		_ = client.DeleteToken(host, cfgSuUser)
		if cfgHost != "" && cfgHost != host {
			_ = client.DeleteToken(cfgHost, cfgSuUser)
		}

		if err != nil {
			return err
		}

		fmt.Println("Logged out.")
		return nil
	},
}

var sessionGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get data about current session",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}

		session, err := c.SDK.Session(nil)
		if err != nil {
			return fmt.Errorf("failed to get session: %w", err)
		}

		bytes, _ := json.MarshalIndent(session, "", "  ")
		fmt.Println(string(bytes))
		return nil
	},
}

var sessionUpdateCmd = &cobra.Command{
	Use:   "update [WORKSPACE_ID]",
	Short: "change the workspace_id of the current session (e.g. 'dev' or 'production')",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := initClient(ctx, false)
		if err != nil {
			return err
		}

		workspaceID := args[0]
		body := v4.WriteApiSession{WorkspaceId: &workspaceID}
		session, err := c.SDK.UpdateSession(body, nil)
		if err != nil {
			return fmt.Errorf("failed to update session: %w", err)
		}

		bytes, _ := json.MarshalIndent(session, "", "  ")
		fmt.Println(string(bytes))
		return nil
	},
}

func init() {
	RootCmd.AddCommand(SessionCmd)
	SessionCmd.AddCommand(sessionLoginCmd)
	SessionCmd.AddCommand(sessionLogoutCmd)
	SessionCmd.AddCommand(sessionGetCmd)
	SessionCmd.AddCommand(sessionUpdateCmd)

	sessionLoginCmd.Flags().BoolVar(&sessionLoginOAuth, "oauth", false, "Use OAuth PKCE flow for login")
	sessionLoginCmd.Flags().BoolVar(&sessionLoginText, "text", false, "Output token to screen instead of file")
}
