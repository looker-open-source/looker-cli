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

package util

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type testCounterDoc struct {
	Count int `json:"count"`
}

func TestWriteFileAtomic0600_CreatesAndTightensPermissions(t *testing.T) {
	base := t.TempDir()
	nestedDir := filepath.Join(base, "sub", "dir")
	target := filepath.Join(nestedDir, "secret.json")

	payload1 := []byte("{\"secret\":\"one\"}\n")
	if err := WriteFileAtomic0600(target, payload1); err != nil {
		t.Fatalf("WriteFileAtomic0600 create failed: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(payload1) {
		t.Errorf("got %q, want %q", string(got), string(payload1))
	}

	if runtime.GOOS != "windows" {
		dirInfo, err := os.Stat(nestedDir)
		if err != nil {
			t.Fatalf("Stat dir failed: %v", err)
		}
		if dirInfo.Mode().Perm() != 0o700 {
			t.Errorf("expected directory perm 0700, got %04o", dirInfo.Mode().Perm())
		}

		fileInfo, err := os.Stat(target)
		if err != nil {
			t.Fatalf("Stat file failed: %v", err)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Errorf("expected file perm 0600, got %04o", fileInfo.Mode().Perm())
		}

		// Loosen permissions to 0644 and ensure atomic overwrite restores 0600.
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatalf("Chmod 0644 failed: %v", err)
		}
	}

	payload2 := []byte("{\"secret\":\"two\"}\n")
	if err := WriteFileAtomic0600(target, payload2); err != nil {
		t.Fatalf("WriteFileAtomic0600 overwrite failed: %v", err)
	}

	if runtime.GOOS != "windows" {
		fileInfo, err := os.Stat(target)
		if err != nil {
			t.Fatalf("Stat file after overwrite failed: %v", err)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Errorf("expected overwritten file perm 0600, got %04o", fileInfo.Mode().Perm())
		}
	}

	entries, err := os.ReadDir(nestedDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("unexpected leftover temp file: %s", e.Name())
		}
	}
}

func incrementCounterLocked(path string, pause time.Duration) error {
	return WithFileLock(path, func() error {
		var doc testCounterDoc
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &doc); err != nil {
				return err
			}
		}
		if pause > 0 {
			time.Sleep(pause)
		}
		doc.Count++
		encoded, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		return WriteFileAtomic0600(path, encoded)
	})
}

func TestWithFileLock_GoroutinesAndReaders(t *testing.T) {
	target := filepath.Join(t.TempDir(), "counter.json")
	const writers = 20
	const readers = 8

	stopReaders := make(chan struct{})
	var readerWG sync.WaitGroup
	errCh := make(chan error, writers+readers)

	for i := 0; i < readers; i++ {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				err := WithFileRLock(target, func() error {
					raw, err := os.ReadFile(target)
					if errors.Is(err, os.ErrNotExist) {
						return nil
					}
					if err != nil {
						return err
					}
					var doc testCounterDoc
					return json.Unmarshal(raw, &doc)
				})
				if err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	var writerWG sync.WaitGroup
	for i := 0; i < writers; i++ {
		writerWG.Add(1)
		go func() {
			defer writerWG.Done()
			if err := incrementCounterLocked(target, time.Millisecond); err != nil {
				errCh <- err
			}
		}()
	}

	writerWG.Wait()
	close(stopReaders)
	readerWG.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent lock/atomic error: %v", err)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var doc testCounterDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal final counter failed: %v", err)
	}
	if doc.Count != writers {
		t.Errorf("expected count %d, got %d", writers, doc.Count)
	}
}

func TestHelperProcessFileLock(t *testing.T) {
	if os.Getenv("LOOKER_CLI_LOCK_HELPER") != "1" {
		return
	}
	target := os.Getenv("LOOKER_CLI_LOCK_TARGET")
	for i := 0; i < 10; i++ {
		if err := incrementCounterLocked(target, time.Millisecond); err != nil {
			t.Fatalf("child increment failed: %v", err)
		}
	}
}

func TestWithFileLock_MultiProcess(t *testing.T) {
	target := filepath.Join(t.TempDir(), "multiprocess_counter.json")
	const procs = 6
	const perProc = 10

	var wg sync.WaitGroup
	errCh := make(chan error, procs)

	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessFileLock$")
			cmd.Env = append(os.Environ(),
				"LOOKER_CLI_LOCK_HELPER=1",
				"LOOKER_CLI_LOCK_TARGET="+target,
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				errCh <- errors.New(string(out) + ": " + err.Error())
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("subprocess failed: %v", err)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var doc testCounterDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal final counter failed: %v", err)
	}
	if doc.Count != procs*perProc {
		t.Errorf("expected count %d across %d processes, got %d", procs*perProc, procs, doc.Count)
	}
}
