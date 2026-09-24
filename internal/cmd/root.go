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
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/config"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/spf13/cobra"
)

var (
	cfgHost         string
	cfgPort         string
	cfgClientID     string
	cfgClientSecret string
	cfgToken        string
	cfgSuUser       string
	cfgSSL          bool
	cfgVerifySSL    bool
	cfgTokenFile    bool
	cfgDebug        bool
	cfgTimeout      int
	cfgHTTPProxy    string
	cfgForce        bool
	cfgProfile      string
)

var RootCmd = &cobra.Command{
	Use:   "looker-cli",
	Short: "Looker CLI - A Looker Content Utility",
	Long:  `Looker CLI can be used to navigate and manage Folders, Looks, and Dashboards via a simple command line tool.`,
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVar(&cfgHost, "host", "localhost", "Looker Host")
	RootCmd.PersistentFlags().StringVar(&cfgPort, "port", "19999", "Looker API Port")
	RootCmd.PersistentFlags().StringVar(&cfgClientID, "client-id", "", "API Client Id")
	RootCmd.PersistentFlags().StringVar(&cfgClientSecret, "client-secret", "", "API Client Secret")
	RootCmd.PersistentFlags().StringVar(&cfgToken, "token", "", "Access token to use for authentication")
	RootCmd.PersistentFlags().StringVar(&cfgSuUser, "su", "", "After connecting, change to user_id given")
	RootCmd.PersistentFlags().BoolVar(&cfgSSL, "ssl", true, "Use ssl to communicate with host")
	RootCmd.PersistentFlags().BoolVar(&cfgVerifySSL, "verify-ssl", true, "Verify the SSL certificate of the host")
	RootCmd.PersistentFlags().BoolVar(&cfgTokenFile, "token-file", false, "Use access token stored in file for authentication")
	RootCmd.PersistentFlags().BoolVar(&cfgDebug, "debug", false, "Run in debug mode")
	RootCmd.PersistentFlags().IntVar(&cfgTimeout, "timeout", 60, "Seconds to wait for a response from the server")
	RootCmd.PersistentFlags().StringVar(&cfgHTTPProxy, "http-proxy", "", "HTTP Proxy for connecting to Looker host")
	RootCmd.PersistentFlags().BoolVar(&cfgForce, "force", false, "Overwrite objects on server")
	RootCmd.PersistentFlags().StringVar(&cfgProfile, "profile", "", "Use a specific profile from config.yaml")

	client.UserAgent = fmt.Sprintf("looker-cli %s", Version)
}

var MockSDK *v4.LookerSDK

func initClient(ctx context.Context, oauth bool) (*client.ClientWrapper, error) {
	if MockSDK != nil {
		activeProfile := cfgProfile
		host := cfgHost
		if cfg, err := config.Load(); err == nil && cfg != nil {
			if activeProfile == "" {
				activeProfile = cfg.Default
			}
			if activeProfile != "" {
				if p, ok := cfg.Profiles[activeProfile]; ok && !RootCmd.PersistentFlags().Lookup("host").Changed && p.Host != "" {
					host = p.Host
				}
			}
		}
		return &client.ClientWrapper{SDK: MockSDK, Host: host, SuUser: cfgSuUser, ActiveProfile: activeProfile}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		if cfgProfile != "" {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
	}

	var prof config.Profile
	activeProfile := cfgProfile
	if activeProfile == "" && cfg != nil {
		activeProfile = cfg.Default
	}

	if activeProfile != "" && cfg != nil {
		if p, ok := cfg.Profiles[activeProfile]; ok {
			prof = p
		} else if cfgProfile != "" {
			return nil, fmt.Errorf("profile %q not found", cfgProfile)
		}
	}

	host := cfgHost
	if !RootCmd.PersistentFlags().Lookup("host").Changed {
		if prof.Host != "" {
			host = prof.Host
		} else if envURL := os.Getenv("LOOKERSDK_BASE_URL"); envURL != "" {
			if u, err := url.Parse(envURL); err == nil && u.Hostname() != "" {
				host = u.Hostname()
			}
		}
	}

	port := cfgPort
	if !RootCmd.PersistentFlags().Lookup("port").Changed {
		if prof.Port != "" {
			port = prof.Port
		} else if envURL := os.Getenv("LOOKERSDK_BASE_URL"); envURL != "" {
			if u, err := url.Parse(envURL); err == nil && u.Port() != "" {
				port = u.Port()
			}
		}
	}

	clientID := cfgClientID
	if !RootCmd.PersistentFlags().Lookup("client-id").Changed && prof.ClientID != "" {
		clientID = prof.ClientID
	}

	clientSecret := cfgClientSecret
	if !RootCmd.PersistentFlags().Lookup("client-secret").Changed && prof.ClientSecret != "" {
		clientSecret = prof.ClientSecret
	}

	token := ""
	if RootCmd.PersistentFlags().Lookup("token").Changed {
		token = cfgToken
	}

	ssl := cfgSSL
	if flag := RootCmd.PersistentFlags().Lookup("ssl"); flag != nil && !flag.Changed {
		if prof.SSL != nil {
			ssl = *prof.SSL
		}
	}

	verifySSL := cfgVerifySSL
	if !RootCmd.PersistentFlags().Lookup("verify-ssl").Changed {
		if prof.VerifySSL != nil {
			verifySSL = *prof.VerifySSL
		} else if envVerify := os.Getenv("LOOKERSDK_VERIFY_SSL"); envVerify == "false" {
			verifySSL = false
		}
	}

	return client.NewClient(
		ctx,
		host,
		port,
		clientID,
		clientSecret,
		token,
		cfgSuUser,
		ssl,
		verifySSL,
		oauth,
		cfgTokenFile,
		activeProfile,
	)
}
