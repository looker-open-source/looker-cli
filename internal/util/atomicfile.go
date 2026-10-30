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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	inProcLocksMu sync.Mutex
	inProcLocks   = make(map[string]*sync.RWMutex)
)

func canonicalLockPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func getInProcLock(cleanPath string) *sync.RWMutex {
	inProcLocksMu.Lock()
	defer inProcLocksMu.Unlock()
	mu, ok := inProcLocks[cleanPath]
	if !ok {
		mu = &sync.RWMutex{}
		inProcLocks[cleanPath] = mu
	}
	return mu
}

// WithFileLock executes fn while holding both an in-process exclusive lock and
// an OS-level exclusive advisory lock on <path>.lock.
func WithFileLock(path string, fn func() error) error {
	absPath := canonicalLockPath(path)
	mu := getInProcLock(absPath)
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	lockPath := absPath + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}
	if err := osLockFile(lf, true); err != nil {
		_ = lf.Close()
		return fmt.Errorf("failed to lock file %s: %w", lockPath, err)
	}
	defer func() {
		_ = osUnlockFile(lf)
		_ = lf.Close()
	}()

	return fn()
}

// WithFileRLock executes fn while holding an in-process read lock and, when
// path exists and a lock file can be opened, an OS-level shared lock on
// <path>.lock. When path does not exist or resides on a read-only filesystem
// without an existing lock file, it avoids creating directories or lock files.
func WithFileRLock(path string, fn func() error) error {
	absPath := canonicalLockPath(path)
	mu := getInProcLock(absPath)
	mu.RLock()
	defer mu.RUnlock()

	if _, err := os.Stat(absPath); err != nil {
		return fn()
	}

	lockPath := absPath + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		lf, err = os.Open(lockPath)
	}
	if err == nil {
		if lockErr := osLockFile(lf, false); lockErr != nil {
			_ = lf.Close()
			return fmt.Errorf("failed to read-lock file %s: %w", lockPath, lockErr)
		}
		defer func() {
			_ = osUnlockFile(lf)
			_ = lf.Close()
		}()
	}

	return fn()
}

// WriteFileAtomic0600 writes data to a 0600 temporary file in path's parent
// directory, fsyncs and closes it, and atomically renames it over path.
func WriteFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(0o600); err != nil {
			return fmt.Errorf("failed to set permissions on temp file: %w", err)
		}
	}

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", path, err)
	}
	cleanup = false
	return nil
}
