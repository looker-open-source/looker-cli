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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/looker-open-source/looker-cli/internal/config"
)

func TestCheckSecureTokenFilePerm(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		mode    os.FileMode
		wantErr bool
	}{
		{name: "windows 0666 synthesized writable", goos: "windows", mode: 0o666, wantErr: false},
		{name: "windows 0444 synthesized readonly", goos: "windows", mode: 0o444, wantErr: false},
		{name: "linux 0600 owner rw", goos: "linux", mode: 0o600, wantErr: false},
		{name: "linux 0400 owner r", goos: "linux", mode: 0o400, wantErr: false},
		{name: "darwin 0600 owner rw", goos: "darwin", mode: 0o600, wantErr: false},
		{name: "darwin 0400 owner r", goos: "darwin", mode: 0o400, wantErr: false},
		{name: "linux 0644 world readable", goos: "linux", mode: 0o644, wantErr: true},
		{name: "linux 0640 group readable", goos: "linux", mode: 0o640, wantErr: true},
		{name: "linux 0604 other readable", goos: "linux", mode: 0o604, wantErr: true},
		{name: "darwin 0660 group rw", goos: "darwin", mode: 0o660, wantErr: true},
		{name: "linux 0666 world rw", goos: "linux", mode: 0o666, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSecureTokenFilePerm(tc.goos, tc.mode, "/tmp/.looker_auth")
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkSecureTokenFilePerm(%q, %04o) error = %v, wantErr = %v", tc.goos, tc.mode, err, tc.wantErr)
			}
		})
	}
}

func TestReadTokenData_Allows0400InReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode test")
	}
	isolateAuthState(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	exp := time.Now().Add(time.Hour)
	if err := StoreToken("ro.looker.com", "", "ro_tok", "", "", exp); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	tokenPath := filepath.Join(home, tokenFileName)
	// Remove the lock file to simulate a mounted read-only secret directory.
	_ = os.Remove(tokenPath + ".lock")
	if err := os.Chmod(tokenPath, 0o400); err != nil {
		t.Fatalf("Chmod 0400 failed: %v", err)
	}
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("Chmod 0500 home failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(home, 0o700)
		_ = os.Chmod(tokenPath, 0o600)
	})

	got, err := GetToken("ro.looker.com", "")
	if err != nil {
		t.Fatalf("GetToken on 0400 file in 0500 dir failed: %v", err)
	}
	if got != "ro_tok" {
		t.Errorf("got token %q, want 'ro_tok'", got)
	}
}

func TestDeleteToken_MissingFileDoesNotCreateLockFile(t *testing.T) {
	isolateAuthState(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	if err := DeleteToken("missing.looker.com", ""); err != nil {
		t.Fatalf("DeleteToken failed: %v", err)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tokenFileName) {
			t.Errorf("unexpected file created in HOME on no-op DeleteToken: %s", e.Name())
		}
	}
}

func TestHelperProcessStoreToken(t *testing.T) {
	if os.Getenv("LOOKER_CLI_TOKEN_HELPER") != "1" {
		return
	}
	host := os.Getenv("LOOKER_CLI_TOKEN_HOST")
	tok := os.Getenv("LOOKER_CLI_TOKEN_VALUE")
	if err := StoreToken(host, "", tok, "ref_"+tok, "cid", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("child StoreToken failed: %v", err)
	}
}

func TestStoreToken_ConcurrentGoroutinesAndProcesses(t *testing.T) {
	isolateAuthState(t)
	exp := time.Now().Add(time.Hour)

	const goroutines = 20
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			host := fmt.Sprintf("g-%02d.looker.com", i)
			tok := fmt.Sprintf("tok-g-%02d", i)
			if err := StoreToken(host, "", tok, "", "", exp); err != nil {
				errCh <- err
				return
			}
			if _, err := ReadTokenData(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent goroutine StoreToken/ReadTokenData error: %v", err)
	}

	const procs = 6
	var procWG sync.WaitGroup
	procErrCh := make(chan error, procs)
	for i := 0; i < procs; i++ {
		i := i
		procWG.Add(1)
		go func() {
			defer procWG.Done()
			host := fmt.Sprintf("p-%02d.looker.com", i)
			tok := fmt.Sprintf("tok-p-%02d", i)
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessStoreToken$")
			cmd.Env = append(os.Environ(),
				"LOOKER_CLI_TOKEN_HELPER=1",
				"LOOKER_CLI_TOKEN_HOST="+host,
				"LOOKER_CLI_TOKEN_VALUE="+tok,
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				procErrCh <- errors.New(string(out) + ": " + err.Error())
			}
		}()
	}
	procWG.Wait()
	close(procErrCh)
	for err := range procErrCh {
		t.Fatalf("subprocess StoreToken failed: %v", err)
	}

	data, err := ReadTokenData()
	if err != nil {
		t.Fatalf("ReadTokenData failed: %v", err)
	}
	for i := 0; i < goroutines; i++ {
		host := fmt.Sprintf("g-%02d.looker.com", i)
		want := fmt.Sprintf("tok-g-%02d", i)
		if got := data[host]["default"].Token; got != want {
			t.Errorf("missing or wrong goroutine token for %s: got %q, want %q", host, got, want)
		}
	}
	for i := 0; i < procs; i++ {
		host := fmt.Sprintf("p-%02d.looker.com", i)
		want := fmt.Sprintf("tok-p-%02d", i)
		if got := data[host]["default"].Token; got != want {
			t.Errorf("missing or wrong subprocess token for %s: got %q, want %q", host, got, want)
		}
	}
}

func TestNewClient_SurfacesCorruptConfigAndOAuthPersistFailure(t *testing.T) {
	t.Run("corrupt config.yaml with activeProfile returns error", func(t *testing.T) {
		isolateAuthState(t)
		cfgPath, err := config.ConfigPath()
		if err != nil {
			t.Fatalf("ConfigPath failed: %v", err)
		}
		if err := os.WriteFile(cfgPath, []byte("profiles:\n  bad: [unterminated\n"), 0o600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		_, err = NewClient(context.Background(), "example.looker.com", "19999", "cid", "csec", "", "", true, true, false, false, "bad")
		if err == nil || !strings.Contains(err.Error(), "failed to load config") {
			t.Fatalf("expected failed to load config error, got %v", err)
		}
	})

	t.Run("oauth login surfaces token storage error", func(t *testing.T) {
		isolateAuthState(t)
		t.Setenv("PATH", "")

		// Point HOME at a regular file so ~/.looker_auth cannot be opened/locked.
		blockingFile := filepath.Join(t.TempDir(), "not_a_directory")
		if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		t.Setenv("HOME", blockingFile)

		m := newSuMockServer(t)
		host, port := m.hostPort(t)

		oldStdin := oauthStdin
		t.Cleanup(func() { oauthStdin = oldStdin })
		pr, pw := io.Pipe()
		oauthStdin = pr
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, _ = fmt.Fprintln(pw, "test_auth_code")
			_ = pw.Close()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := NewClient(ctx, host, port, "cid", "", "", "", false, true, true, false, "")
		if err == nil || !strings.Contains(err.Error(), "failed to store oauth token") {
			t.Fatalf("expected failed to store oauth token error, got %v", err)
		}
	})
}
