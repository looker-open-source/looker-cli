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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func isolateConfigPath(t *testing.T, path string) {
	t.Helper()
	orig := ConfigPathOverride
	ConfigPathOverride = path
	t.Cleanup(func() { ConfigPathOverride = orig })
}

func TestLoad_MissingFileDoesNotCreateDirsOrLock(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "nonexistent", "looker-cli")
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	isolateConfigPath(t, cfgPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg == nil || cfg.Profiles == nil || len(cfg.Profiles) != 0 {
		t.Fatalf("expected empty initialized Config, got %+v", cfg)
	}
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("expected Load on missing file not to create parent dir, stat err = %v", err)
	}
}

func TestSaveAndLoad_PermissionsAndCleanHost(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, ".config", "looker-cli")
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	isolateConfigPath(t, cfgPath)

	cfg := &Config{
		Default: "prod",
		Profiles: map[string]Profile{
			"prod": {
				Host:         "https://prod.looker.com/",
				Port:         "19999",
				ClientID:     "cid",
				ClientSecret: "csec",
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if runtime.GOOS != "windows" {
		dirInfo, err := os.Stat(cfgDir)
		if err != nil {
			t.Fatalf("Stat config dir failed: %v", err)
		}
		if dirInfo.Mode().Perm() != 0o700 {
			t.Errorf("expected config dir perm 0700, got %04o", dirInfo.Mode().Perm())
		}
		fileInfo, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatalf("Stat config file failed: %v", err)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Errorf("expected config file perm 0600, got %04o", fileInfo.Mode().Perm())
		}
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Profiles["prod"].Host != "prod.looker.com" {
		t.Errorf("expected cleaned host 'prod.looker.com', got %q", loaded.Profiles["prod"].Host)
	}
}

func TestLoadAndUpdate_CorruptYAMLReturnsError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	isolateConfigPath(t, cfgPath)

	corrupt := []byte("profiles:\n  broken: [unterminated\n")
	if err := os.WriteFile(cfgPath, corrupt, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected Load to fail on corrupt YAML")
	}

	err := Update(func(cfg *Config) error {
		cfg.Default = "other"
		return nil
	})
	if err == nil {
		t.Fatal("expected Update to fail on corrupt YAML without clobbering file")
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(after) != string(corrupt) {
		t.Errorf("corrupt config file was unexpectedly overwritten: %q", string(after))
	}
}

func TestUpdate_ConcurrentGoroutinesPreserveAllProfiles(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	isolateConfigPath(t, cfgPath)

	const n = 25
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("prof-%02d", i)
			err := Update(func(cfg *Config) error {
				cfg.Profiles[name] = Profile{
					Host: fmt.Sprintf("https://host-%02d.looker.com/", i),
					Port: "19999",
				}
				if cfg.Default == "" {
					cfg.Default = name
				}
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("Update failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Profiles) != n {
		t.Fatalf("expected %d profiles, got %d", n, len(loaded.Profiles))
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("prof-%02d", i)
		wantHost := fmt.Sprintf("host-%02d.looker.com", i)
		if got := loaded.Profiles[name].Host; got != wantHost {
			t.Errorf("profile %s host = %q, want %q", name, got, wantHost)
		}
	}
}
