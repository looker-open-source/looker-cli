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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/looker-open-source/looker-cli/internal/config"
)

func resetGlobalClientFlags(t *testing.T) {
	t.Helper()
	restore := func() {
		cfgHost = "localhost"
		cfgPort = "19999"
		cfgClientID = ""
		cfgClientSecret = ""
		cfgToken = ""
		cfgSuUser = ""
		cfgSSL = true
		cfgVerifySSL = true
		cfgTokenFile = false
		cfgDebug = false
		cfgTimeout = 60
		cfgHTTPProxy = ""
		cfgProfile = ""
		cfgHeaders = nil
		for _, name := range []string{"host", "port", "client-id", "client-secret", "token", "su", "ssl", "verify-ssl", "token-file", "timeout", "http-proxy", "profile", "header"} {
			if f := RootCmd.PersistentFlags().Lookup(name); f != nil {
				f.Changed = false
			}
		}
	}
	restore()
	t.Cleanup(restore)
}

func TestInitClient_TimeoutAndHTTPProxyWiring(t *testing.T) {
	resetGlobalClientFlags(t)
	MockSDK = nil

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	origOverride := config.ConfigPathOverride
	config.ConfigPathOverride = filepath.Join(tmpDir, "config.yaml")
	t.Cleanup(func() { config.ConfigPathOverride = origOverride })

	t.Setenv("LOOKERSDK_CLIENT_ID", "dummy_id")
	t.Setenv("LOOKERSDK_CLIENT_SECRET", "dummy_secret")

	t.Run("default CLI timeout is 60s", func(t *testing.T) {
		resetGlobalClientFlags(t)
		t.Setenv("LOOKERSDK_TIMEOUT", "")

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient failed: %v", err)
		}
		if wrapper.Session.Config.Timeout != 60 {
			t.Errorf("Session.Config.Timeout = %d, want 60", wrapper.Session.Config.Timeout)
		}
		if wrapper.Session.Client.Timeout != 60*time.Second {
			t.Errorf("Session.Client.Timeout = %v, want 60s", wrapper.Session.Client.Timeout)
		}
	})

	t.Run("LOOKERSDK_TIMEOUT honored when flag omitted, overridden when flag set", func(t *testing.T) {
		resetGlobalClientFlags(t)
		t.Setenv("LOOKERSDK_TIMEOUT", "45")

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient with LOOKERSDK_TIMEOUT failed: %v", err)
		}
		if wrapper.Session.Config.Timeout != 45 {
			t.Errorf("Session.Config.Timeout = %d, want 45", wrapper.Session.Config.Timeout)
		}

		if err := RootCmd.PersistentFlags().Set("timeout", "25"); err != nil {
			t.Fatalf("Set timeout flag failed: %v", err)
		}
		wrapperOverride, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient with --timeout=25 failed: %v", err)
		}
		if wrapperOverride.Session.Config.Timeout != 25 {
			t.Errorf("Session.Config.Timeout = %d, want 25", wrapperOverride.Session.Config.Timeout)
		}
	})

	t.Run("non-positive timeout or invalid LOOKERSDK_TIMEOUT rejected", func(t *testing.T) {
		for _, badVal := range []string{"0", "-5"} {
			resetGlobalClientFlags(t)
			if err := RootCmd.PersistentFlags().Set("timeout", badVal); err != nil {
				t.Fatalf("Set timeout=%s failed: %v", badVal, err)
			}
			if _, err := initClient(context.Background(), false); err == nil {
				t.Errorf("expected error for --timeout=%s", badVal)
			}
		}

		for _, badEnv := range []string{"0", "-10", "notanumber"} {
			resetGlobalClientFlags(t)
			t.Setenv("LOOKERSDK_TIMEOUT", badEnv)
			if _, err := initClient(context.Background(), false); err == nil {
				t.Errorf("expected error for LOOKERSDK_TIMEOUT=%q", badEnv)
			}
		}
	})

	t.Run("--http-proxy wires proxy and rejects invalid URLs", func(t *testing.T) {
		resetGlobalClientFlags(t)
		if err := RootCmd.PersistentFlags().Set("http-proxy", "not-a-valid-proxy"); err != nil {
			t.Fatalf("Set http-proxy failed: %v", err)
		}
		if _, err := initClient(context.Background(), false); err == nil || !strings.Contains(err.Error(), "--http-proxy") {
			t.Fatalf("expected invalid --http-proxy error, got %v", err)
		}

		var proxyHits atomic.Int32
		proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1"}`))
		}))
		defer proxySrv.Close()

		resetGlobalClientFlags(t)
		_ = RootCmd.PersistentFlags().Set("host", "looker.internal.example")
		_ = RootCmd.PersistentFlags().Set("ssl", "false")
		_ = RootCmd.PersistentFlags().Set("token", "tok123")
		_ = RootCmd.PersistentFlags().Set("http-proxy", proxySrv.URL)

		wrapper, err := initClient(context.Background(), false)
		if err != nil {
			t.Fatalf("initClient with valid --http-proxy failed: %v", err)
		}
		if _, err := wrapper.SDK.Me("", nil); err != nil {
			t.Fatalf("SDK.Me through --http-proxy failed: %v", err)
		}
		if proxyHits.Load() != 1 {
			t.Fatalf("expected 1 hit on configured --http-proxy server, got %d", proxyHits.Load())
		}
	})
}

func TestRootCmd_DeadGlobalForceRemoved(t *testing.T) {
	resetGlobalClientFlags(t)

	if f := RootCmd.PersistentFlags().Lookup("force"); f != nil {
		t.Fatalf("expected dead persistent --force flag to be removed from RootCmd")
	}

	// Commands without a local --force flag must reject --force rather than silently ignoring it.
	RootCmd.SetArgs([]string{"user", "me", "--force"})
	err := RootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --force") {
		t.Fatalf("expected unknown flag: --force on 'user me --force', got %v", err)
	}

	// Commands that support --force locally must still expose their local --force flag.
	for _, cmdPath := range [][]string{
		{"look", "mv"},
		{"look", "import"},
		{"dashboard", "mv"},
		{"dashboard", "import"},
		{"folder", "rm"},
		{"model", "import"},
		{"attribute", "create"},
		{"attribute", "import"},
	} {
		sub, _, findErr := RootCmd.Find(cmdPath)
		if findErr != nil {
			t.Fatalf("Find(%v) failed: %v", cmdPath, findErr)
		}
		if sub.Flags().Lookup("force") == nil {
			t.Errorf("expected local --force flag on command %v", cmdPath)
		}
	}
}
