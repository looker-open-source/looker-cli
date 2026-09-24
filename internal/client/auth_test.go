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

package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/looker-open-source/looker-cli/internal/config"
)

func TestTokenStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "looker_cli_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()
	_ = os.Setenv("HOME", tmpDir)

	host := "test.looker.com"
	token := "test_token_123"
	exp := time.Now().Add(1 * time.Hour)

	// 1. Store token
	err = StoreToken(host, "", token, "", "", exp)
	if err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// 2. Verify file exists and permissions
	path := filepath.Join(tmpDir, tokenFileName)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("token file not created: %v", err)
	}
	if stat.Mode().Perm() != 0600 {
		t.Errorf("expected perm 0600, got %v", stat.Mode().Perm())
	}

	// 3. Get token
	gotTok, err := GetToken(host, "")
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if gotTok != token {
		t.Errorf("expected token %s, got %s", token, gotTok)
	}

	// 4. Get token for non-existent host
	_, err = GetToken("unknown.com", "")
	if err == nil {
		t.Errorf("expected error for unknown host")
	}

	// 5. Store su token
	suUser := "1237"
	suToken := "su_token_456"
	err = StoreToken(host, suUser, suToken, "", "", exp)
	if err != nil {
		t.Fatalf("StoreToken su failed: %v", err)
	}

	gotSuTok, err := GetToken(host, suUser)
	if err != nil {
		t.Fatalf("GetToken su failed: %v", err)
	}
	if gotSuTok != suToken {
		t.Errorf("expected su token %s, got %s", suToken, gotSuTok)
	}
}

func TestTokenStorage_Expired(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "looker_cli_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()
	_ = os.Setenv("HOME", tmpDir)

	host := "test.looker.com"
	token := "test_token_123"
	// Expired 10 minutes ago
	exp := time.Now().Add(-10 * time.Minute)

	err = StoreToken(host, "", token, "", "", exp)
	if err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	_, err = GetToken(host, "")
	if err == nil {
		t.Errorf("expected error for expired token")
	}

	// Verify expired token was deleted from token file
	if _, err := GetTokenEntry(host, ""); err == nil {
		t.Errorf("expected expired token to be deleted from token file")
	}
}

func TestCompareVersionLessThanOrEqual26_8(t *testing.T) {
	tests := []struct {
		version  string
		expected bool
	}{
		{"25.20.0", true},
		{"26.8.0", true},
		{"26.8.9", true},
		{"26.9.0", false},
		{"26.20.1", false},
		{"27.1.0", false},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		res := compareVersionLessThanOrEqual26_8(tt.version)
		if res != tt.expected {
			t.Errorf("for version %q, expected %v, got %v", tt.version, tt.expected, res)
		}
	}
}

func TestParsePastedAuthInput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:        "valid redirect URL",
			input:       "http://127.0.0.1:7777/?code=authcode123",
			expected:    "authcode123",
			expectError: false,
		},
		{
			name:        "valid redirect URL HTTPS",
			input:       "https://localhost:7777/auth?code=securecode456&state=xyz",
			expected:    "securecode456",
			expectError: false,
		},
		{
			name:        "just code",
			input:       "directcode789",
			expected:    "directcode789",
			expectError: false,
		},
		{
			name:        "empty input",
			input:       "",
			expectError: true,
		},
		{
			name:        "url without code",
			input:       "http://127.0.0.1:7777/?state=xyz",
			expectError: true,
		},
		{
			name:        "invalid url query",
			input:       "http://127.0.0.1:7777/??invalid",
			expectError: true,
		},
		{
			name:        "too short code fallback",
			input:       "abc",
			expectError: true,
		},
		{
			name:        "code with spaces",
			input:       "some code",
			expectError: true,
		},
		{
			name:        "code with slash",
			input:       "some/code",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePastedAuthInput(tt.input)
			if (err != nil) != tt.expectError {
				t.Fatalf("parsePastedAuthInput(%q) error = %v, expectError = %v", tt.input, err, tt.expectError)
			}
			if !tt.expectError && got != tt.expected {
				t.Errorf("parsePastedAuthInput(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPerformOAuthLogin_StdinFallback(t *testing.T) {
	// 1. Start mock Looker API server
	mockLooker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"access_token": "mock_access_token_abc",
				"token_type": "Bearer",
				"expires_in": 3600,
				"refresh_token": "mock_refresh_token_def"
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockLooker.Close()

	u, err := url.Parse(mockLooker.URL)
	if err != nil {
		t.Fatalf("failed to parse mock server url: %v", err)
	}
	host := u.Hostname()
	port := u.Port()
	ssl := u.Scheme == "https"

	// 2. Mock stdin
	oldStdin := oauthStdin
	defer func() { oauthStdin = oldStdin }()

	pr, pw := io.Pipe()
	oauthStdin = pr

	// 3. Write input to stdin mock pipe asynchronously
	go func() {
		// Wait a bit to ensure scanner.Scan() is called
		time.Sleep(100 * time.Millisecond)
		// Write the pasted redirect URL
		_, _ = fmt.Fprintln(pw, "http://127.0.0.1:7777/?code=test_auth_code_123")
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accessToken, refreshToken, exp, err := PerformOAuthLogin(ctx, host, port, "test_client_id", ssl)
	if err != nil {
		t.Fatalf("PerformOAuthLogin failed: %v", err)
	}

	if accessToken != "mock_access_token_abc" {
		t.Errorf("expected access token 'mock_access_token_abc', got %q", accessToken)
	}
	if refreshToken != "mock_refresh_token_def" {
		t.Errorf("expected refresh token 'mock_refresh_token_def', got %q", refreshToken)
	}
	if exp.Before(time.Now()) {
		t.Errorf("expected expiration in the future, got %v", exp)
	}
}

func TestNewClient_DeletesExpiredTokens(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "looker_cli_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()
	_ = os.Setenv("HOME", tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	origOverride := config.ConfigPathOverride
	config.ConfigPathOverride = configPath
	defer func() { config.ConfigPathOverride = origOverride }()

	host := "test.looker.com"
	exp := time.Now().Add(-10 * time.Minute)

	t.Run("Expired profile token without client credentials is deleted and returns error", func(t *testing.T) {
		cfg := &config.Config{
			Default: "prof1",
			Profiles: map[string]config.Profile{
				"prof1": {
					Host:        host,
					Port:        "19999",
					AccessToken: "expired_tok",
					Expiration:  exp.Format(TimeFormat),
				},
			},
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		_, err := NewClient(context.Background(), host, "19999", "", "", "", "", true, true, false, false, "prof1")
		if err == nil {
			t.Fatalf("expected error for expired profile token without fallback credentials")
		}

		loadedCfg, err := config.Load()
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		if loadedCfg.Profiles["prof1"].AccessToken != "" || loadedCfg.Profiles["prof1"].Expiration != "" {
			t.Errorf("expected expired profile token to be deleted, got %+v", loadedCfg.Profiles["prof1"])
		}
	})

	t.Run("Expired profile token with client credentials is deleted and falls back to client credentials", func(t *testing.T) {
		cfg := &config.Config{
			Default: "prof1",
			Profiles: map[string]config.Profile{
				"prof1": {
					Host:         host,
					Port:         "19999",
					ClientID:     "my_id",
					ClientSecret: "my_secret",
					AccessToken:  "expired_tok",
					Expiration:   exp.Format(TimeFormat),
				},
			},
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		wrapper, err := NewClient(context.Background(), host, "19999", "my_id", "my_secret", "", "", true, true, false, false, "prof1")
		if err != nil {
			t.Fatalf("expected fallback to client credentials to succeed, got error: %v", err)
		}
		if wrapper.Session.Config.Headers["Authorization"] != "" {
			t.Errorf("expected expired token not to be set in Authorization header, got %q", wrapper.Session.Config.Headers["Authorization"])
		}

		loadedCfg, err := config.Load()
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		if loadedCfg.Profiles["prof1"].AccessToken != "" || loadedCfg.Profiles["prof1"].Expiration != "" {
			t.Errorf("expected expired profile token to be deleted, got %+v", loadedCfg.Profiles["prof1"])
		}
	})

	t.Run("Expired token in token file is deleted", func(t *testing.T) {
		if err := StoreToken(host, "", "expired_file_tok", "", "", exp); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}

		_, err := NewClient(context.Background(), host, "19999", "", "", "", "", true, true, false, true, "")
		if err == nil {
			t.Fatalf("expected error for expired token in token file")
		}

		if _, err := GetTokenEntry(host, ""); err == nil {
			t.Errorf("expected expired token entry to be deleted from token file")
		}
	})

	t.Run("Expired refresh token (>30 days) in profile is deleted without attempting refresh", func(t *testing.T) {
		expired31DaysAgo := time.Now().Add(-31 * 24 * time.Hour)
		cfg := &config.Config{
			Default: "prof1",
			Profiles: map[string]config.Profile{
				"prof1": {
					Host:              host,
					Port:              "19999",
					AccessToken:       "expired_tok",
					RefreshToken:      "expired_ref_tok",
					Expiration:        exp.Format(TimeFormat),
					RefreshExpiration: expired31DaysAgo.Add(RefreshTokenDuration).Format(TimeFormat),
				},
			},
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		_, err := NewClient(context.Background(), host, "19999", "", "", "", "", true, true, false, false, "prof1")
		if err == nil {
			t.Fatalf("expected error when both token and refresh token are expired")
		}

		loadedCfg, err := config.Load()
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		p := loadedCfg.Profiles["prof1"]
		if p.AccessToken != "" || p.RefreshToken != "" || p.Expiration != "" || p.RefreshExpiration != "" {
			t.Errorf("expected expired profile token and refresh token to be deleted, got %+v", p)
		}
	})

	t.Run("Expired refresh token (>30 days) in token file is deleted", func(t *testing.T) {
		expired31DaysAgo := time.Now().Add(-31 * 24 * time.Hour)
		if err := StoreToken(host, "", "expired_file_tok", "expired_file_ref_tok", "cid", expired31DaysAgo); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}

		_, err := NewClient(context.Background(), host, "19999", "", "", "", "", true, true, false, true, "")
		if err == nil {
			t.Fatalf("expected error when both token and refresh token in token file are expired")
		}

		if _, err := GetTokenEntry(host, ""); err == nil {
			t.Errorf("expected expired token and refresh token entry to be deleted from token file")
		}
	})
}

func TestNewClient_UnauthorizedDeletesStoredToken(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "looker_cli_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()
	_ = os.Setenv("HOME", tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	origOverride := config.ConfigPathOverride
	config.ConfigPathOverride = configPath
	defer func() { config.ConfigPathOverride = origOverride }()

	mockLooker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Requires authentication."}`))
	}))
	defer mockLooker.Close()

	u, err := url.Parse(mockLooker.URL)
	if err != nil {
		t.Fatalf("failed to parse mock server url: %v", err)
	}
	host := u.Hostname()
	port := u.Port()
	exp := time.Now().Add(1 * time.Hour)

	cfg := &config.Config{
		Default: "prof1",
		Profiles: map[string]config.Profile{
			"prof1": {
				Host:        host,
				Port:        port,
				AccessToken: "server_timed_out_tok",
				Expiration:  exp.Format(TimeFormat),
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	if err := StoreToken(host, "", "server_timed_out_tok", "", "", exp); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	wrapper, err := NewClient(context.Background(), host, port, "", "", "", "", false, false, false, true, "prof1")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = wrapper.SDK.Me("", nil)
	if err == nil {
		t.Fatalf("expected 401 error from SDK.Me")
	}

	loadedCfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if loadedCfg.Profiles["prof1"].AccessToken != "" || loadedCfg.Profiles["prof1"].Expiration != "" {
		t.Errorf("expected profile token to be deleted after 401, got %+v", loadedCfg.Profiles["prof1"])
	}
	if _, err := GetTokenEntry(host, ""); err == nil {
		t.Errorf("expected token file entry to be deleted after 401")
	}
}
