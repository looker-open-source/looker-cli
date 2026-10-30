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

//go:build !windows

package util

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

func osLockFile(f *os.File, exclusive bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	fd := int(f.Fd())
	for {
		err := syscall.Flock(fd, how)
		if !errors.Is(err, syscall.EINTR) {
			runtime.KeepAlive(f)
			return err
		}
	}
}

func osUnlockFile(f *os.File) error {
	fd := int(f.Fd())
	for {
		err := syscall.Flock(fd, syscall.LOCK_UN)
		if !errors.Is(err, syscall.EINTR) {
			runtime.KeepAlive(f)
			return err
		}
	}
}
