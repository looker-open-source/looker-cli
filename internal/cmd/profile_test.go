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
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/looker-open-source/looker-cli/internal/client"
	"github.com/looker-open-source/looker-cli/internal/config"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

func setupTempConfig(t *testing.T) string {
	t.Helper()
	// Create temp dir in current directory (internal/cmd) to keep it in workspace
	tmpDir, err := os.MkdirTemp(".", "gzr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	absTmpDir, err := filepath.Abs(tmpDir)
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}
	configPath := filepath.Join(absTmpDir, "config.yaml")
	config.ConfigPathOverride = configPath
	return absTmpDir
}

func teardownTempConfig(t *testing.T, tmpDir string) {
	t.Helper()
	_ = os.RemoveAll(tmpDir)
	config.ConfigPathOverride = ""
}

func executeCommand(args ...string) (string, error) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs(args)
	err := RootCmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

func TestProfileCommands(t *testing.T) {
	tmpDir := setupTempConfig(t)
	defer teardownTempConfig(t, tmpDir)

	// 1. List profiles (empty)
	out, err := executeCommand("profile", "ls")
	if err != nil {
		t.Fatalf("profile ls failed: %v", err)
	}
	if !strings.Contains(out, "No profiles found.") {
		t.Errorf("expected 'No profiles found.', got %q", out)
	}

	// 2. Add profile
	out, err = executeCommand("profile", "add", "test-prof", "--host", "test.looker.com", "--port", "19999", "--client-id", "id1", "--client-secret", "sec1")
	if err != nil {
		t.Fatalf("profile add failed: %v", err)
	}
	if !strings.Contains(out, "Profile \"test-prof\" added.") {
		t.Errorf("expected success message, got %q", out)
	}

	// 3. List profiles again
	out, err = executeCommand("profile", "ls")
	if err != nil {
		t.Fatalf("profile ls failed: %v", err)
	}
	if !strings.Contains(out, "* test-prof (test.looker.com:19999)") {
		t.Errorf("expected '* test-prof (test.looker.com:19999)', got %q", out)
	}

	// 4. Add another profile with --ssl=false
	_, err = executeCommand("profile", "add", "test-prof2", "--host", "test2.looker.com", "--port", "20000", "--ssl=false")
	if err != nil {
		t.Fatalf("profile add failed: %v", err)
	}

	cfgCheck, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfgCheck.Profiles["test-prof2"].SSL == nil || *cfgCheck.Profiles["test-prof2"].SSL != false {
		t.Errorf("expected test-prof2 SSL to be false, got %v", cfgCheck.Profiles["test-prof2"].SSL)
	}

	// 5. List profiles (check default marker)
	out, err = executeCommand("profile", "ls")
	if err != nil {
		t.Fatalf("profile ls failed: %v", err)
	}
	if !strings.Contains(out, "* test-prof ") || !strings.Contains(out, "  test-prof2 ") {
		t.Errorf("expected test-prof to be default, got %q", out)
	}

	// 6. Use profile
	out, err = executeCommand("profile", "use", "test-prof2")
	if err != nil {
		t.Fatalf("profile use failed: %v", err)
	}
	if !strings.Contains(out, "Using profile \"test-prof2\" as default.") {
		t.Errorf("expected success message, got %q", out)
	}

	// 7. List profiles (check default marker changed)
	out, err = executeCommand("profile", "ls")
	if err != nil {
		t.Fatalf("profile ls failed: %v", err)
	}
	if !strings.Contains(out, "  test-prof ") || !strings.Contains(out, "* test-prof2 ") {
		t.Errorf("expected test-prof2 to be default, got %q", out)
	}

	// 8. Remove profile
	out, err = executeCommand("profile", "rm", "test-prof")
	if err != nil {
		t.Fatalf("profile rm failed: %v", err)
	}
	if !strings.Contains(out, "Profile \"test-prof\" deleted.") {
		t.Errorf("expected success message, got %q", out)
	}

	// 9. List profiles (check removed)
	out, err = executeCommand("profile", "ls")
	if err != nil {
		t.Fatalf("profile ls failed: %v", err)
	}
	if strings.Contains(out, "test-prof ") {
		t.Errorf("expected test-prof to be removed, got %q", out)
	}
	if !strings.Contains(out, "* test-prof2 ") {
		t.Errorf("expected test-prof2 to still exist and be default, got %q", out)
	}
}

func TestInitClient_Profile(t *testing.T) {
	tmpDir := setupTempConfig(t)
	defer teardownTempConfig(t, tmpDir)

	// Create a profile
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	cfg.Profiles["prof1"] = config.Profile{
		Host:         "profile-host.com",
		Port:         "1234",
		ClientID:     "prof-id",
		ClientSecret: "prof-sec",
	}
	cfg.Default = "prof1"
	err = cfg.Save()
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Reset flags to default/unchanged
	_ = RootCmd.PersistentFlags().Set("host", "localhost")
	_ = RootCmd.PersistentFlags().Set("port", "19999")
	_ = RootCmd.PersistentFlags().Set("client-id", "")
	_ = RootCmd.PersistentFlags().Set("client-secret", "")
	_ = RootCmd.PersistentFlags().Set("ssl", "true")
	_ = RootCmd.PersistentFlags().Set("verify-ssl", "true")
	RootCmd.PersistentFlags().Lookup("host").Changed = false
	RootCmd.PersistentFlags().Lookup("port").Changed = false
	RootCmd.PersistentFlags().Lookup("client-id").Changed = false
	RootCmd.PersistentFlags().Lookup("client-secret").Changed = false
	RootCmd.PersistentFlags().Lookup("ssl").Changed = false
	RootCmd.PersistentFlags().Lookup("verify-ssl").Changed = false
	cfgProfile = ""

	t.Run("Default profile is used when no flags are set", func(t *testing.T) {
		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Host != "profile-host.com" {
			t.Errorf("expected host 'profile-host.com' from profile, got '%s'", wrapper.Host)
		}
		if wrapper.Session.Config.BaseUrl != "https://profile-host.com:1234" {
			t.Errorf("expected base URL 'https://profile-host.com:1234', got '%s'", wrapper.Session.Config.BaseUrl)
		}
		if wrapper.Session.Config.ClientId != "prof-id" {
			t.Errorf("expected client ID 'prof-id', got '%s'", wrapper.Session.Config.ClientId)
		}
	})

	t.Run("Explicit profile flag overrides default profile", func(t *testing.T) {
		// Add another profile
		cfg, _ := config.Load()
		cfg.Profiles["prof2"] = config.Profile{
			Host:         "profile2-host.com",
			Port:         "5678",
			ClientID:     "prof2-id",
			ClientSecret: "prof2-sec",
		}
		_ = cfg.Save()

		// Set profile flag
		_ = RootCmd.PersistentFlags().Set("profile", "prof2")

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Host != "profile2-host.com" {
			t.Errorf("expected host 'profile2-host.com', got '%s'", wrapper.Host)
		}
		// Reset profile flag for next tests
		_ = RootCmd.PersistentFlags().Set("profile", "")
		cfgProfile = ""
	})

	t.Run("Explicit flags override profile values", func(t *testing.T) {
		_ = RootCmd.PersistentFlags().Set("host", "override-host.com")
		RootCmd.PersistentFlags().Lookup("host").Changed = true
		defer func() {
			_ = RootCmd.PersistentFlags().Set("host", "localhost")
			RootCmd.PersistentFlags().Lookup("host").Changed = false
		}()

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Host != "override-host.com" {
			t.Errorf("expected host 'override-host.com', got '%s'", wrapper.Host)
		}
		// Port should still come from profile
		if wrapper.Session.Config.BaseUrl != "https://override-host.com:1234" {
			t.Errorf("expected base URL 'https://override-host.com:1234', got '%s'", wrapper.Session.Config.BaseUrl)
		}
	})

	t.Run("Profile verify-ssl is used when flag is omitted", func(t *testing.T) {
		cfg, _ := config.Load()
		falseVal := false
		cfg.Profiles["prof-no-ssl"] = config.Profile{
			Host:         "profile-host.com",
			Port:         "1234",
			ClientID:     "prof-id",
			ClientSecret: "prof-sec",
			VerifySSL:    &falseVal,
		}
		_ = cfg.Save()
		_ = RootCmd.PersistentFlags().Set("profile", "prof-no-ssl")
		defer func() {
			_ = RootCmd.PersistentFlags().Set("profile", "")
			cfgProfile = ""
		}()

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Session.Config.VerifySsl {
			t.Errorf("expected verifySSL to be false from profile, got true")
		}
	})

	t.Run("Explicit verify-ssl flag overrides profile value", func(t *testing.T) {
		cfg, _ := config.Load()
		falseVal := false
		cfg.Profiles["prof-no-ssl"] = config.Profile{
			Host:         "profile-host.com",
			Port:         "1234",
			ClientID:     "prof-id",
			ClientSecret: "prof-sec",
			VerifySSL:    &falseVal,
		}
		_ = cfg.Save()
		_ = RootCmd.PersistentFlags().Set("profile", "prof-no-ssl")
		_ = RootCmd.PersistentFlags().Set("verify-ssl", "true")
		RootCmd.PersistentFlags().Lookup("verify-ssl").Changed = true
		defer func() {
			_ = RootCmd.PersistentFlags().Set("profile", "")
			cfgProfile = ""
			_ = RootCmd.PersistentFlags().Set("verify-ssl", "true")
			RootCmd.PersistentFlags().Lookup("verify-ssl").Changed = false
		}()

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if !wrapper.Session.Config.VerifySsl {
			t.Errorf("expected verifySSL to be true from flag, got false")
		}
	})

	t.Run("Profile ssl is used when flag is omitted", func(t *testing.T) {
		cfg, _ := config.Load()
		falseVal := false
		cfg.Profiles["prof-no-https"] = config.Profile{
			Host:         "profile-host.com",
			Port:         "1234",
			ClientID:     "prof-id",
			ClientSecret: "prof-sec",
			SSL:          &falseVal,
		}
		_ = cfg.Save()
		_ = RootCmd.PersistentFlags().Set("profile", "prof-no-https")
		_ = RootCmd.PersistentFlags().Set("ssl", "true")
		RootCmd.PersistentFlags().Lookup("ssl").Changed = false
		defer func() {
			_ = RootCmd.PersistentFlags().Set("profile", "")
			cfgProfile = ""
			_ = RootCmd.PersistentFlags().Set("ssl", "true")
			if flag := RootCmd.PersistentFlags().Lookup("ssl"); flag != nil {
				flag.Changed = false
			}
		}()

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Session.Config.BaseUrl != "http://profile-host.com:1234" {
			t.Errorf("expected base URL 'http://profile-host.com:1234', got '%s'", wrapper.Session.Config.BaseUrl)
		}
	})

	t.Run("Explicit ssl flag overrides profile value", func(t *testing.T) {
		cfg, _ := config.Load()
		falseVal := false
		cfg.Profiles["prof-no-https"] = config.Profile{
			Host:         "profile-host.com",
			Port:         "1234",
			ClientID:     "prof-id",
			ClientSecret: "prof-sec",
			SSL:          &falseVal,
		}
		_ = cfg.Save()
		_ = RootCmd.PersistentFlags().Set("profile", "prof-no-https")
		_ = RootCmd.PersistentFlags().Set("ssl", "true")
		RootCmd.PersistentFlags().Lookup("ssl").Changed = true
		defer func() {
			_ = RootCmd.PersistentFlags().Set("profile", "")
			cfgProfile = ""
			_ = RootCmd.PersistentFlags().Set("ssl", "true")
			RootCmd.PersistentFlags().Lookup("ssl").Changed = false
			_ = RootCmd.PersistentFlags().Set("ssl", "true")
			if flag := RootCmd.PersistentFlags().Lookup("ssl"); flag != nil {
				flag.Changed = false
			}
		}()

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}

		if wrapper.Session.Config.BaseUrl != "https://profile-host.com:1234" {
			t.Errorf("expected base URL 'https://profile-host.com:1234', got '%s'", wrapper.Session.Config.BaseUrl)
		}
	})
}

func TestSessionLogout_DeletesTokens(t *testing.T) {
	tmpDir := setupTempConfig(t)
	defer teardownTempConfig(t, tmpDir)

	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()
	_ = os.Setenv("HOME", tmpDir)

	mockDoer := &statefulMockDoer{t: t}
	MockSDK = v4.NewLookerSDK(mockDoer)
	defer func() { MockSDK = nil }()

	exp := time.Now().Add(1 * time.Hour)
	cfg := &config.Config{
		Default: "prof1",
		Profiles: map[string]config.Profile{
			"prof1": {
				Host:         "profile-host.com",
				Port:         "1234",
				ClientID:     "prof-id",
				ClientSecret: "prof-sec",
				AccessToken:  "session_tok_123",
				RefreshToken: "refresh_tok_456",
				Expiration:   exp.Format(client.TimeFormat),
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if err := client.StoreToken("profile-host.com", "", "file_tok_123", "file_refresh_456", "prof-id", exp); err != nil {
		t.Fatalf("failed to store token in file: %v", err)
	}

	out, err := executeCommand("session", "logout")
	if err != nil {
		t.Fatalf("session logout failed: %v", err)
	}
	if !strings.Contains(out, "Logged out.") {
		t.Errorf("expected 'Logged out.', got %q", out)
	}

	loadedCfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	prof := loadedCfg.Profiles["prof1"]
	if prof.AccessToken != "" || prof.RefreshToken != "" || prof.Expiration != "" {
		t.Errorf("expected profile auth token fields to be cleared after logout, got %+v", prof)
	}
	if prof.ClientID != "prof-id" || prof.ClientSecret != "prof-sec" {
		t.Errorf("expected profile client credentials to be preserved, got %+v", prof)
	}

	if _, err := client.GetTokenEntry("profile-host.com", ""); err == nil {
		t.Errorf("expected token in token file to be deleted after logout")
	}
}

