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

//go:build windows

package util

import (
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

const winLockAllBytes = ^uint32(0)

func osLockFile(f *os.File, exclusive bool) error {
	var flags uint32
	if exclusive {
		flags = windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	var ol windows.Overlapped
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, winLockAllBytes, winLockAllBytes, &ol)
	runtime.KeepAlive(f)
	return err
}

func osUnlockFile(f *os.File) error {
	var ol windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, winLockAllBytes, winLockAllBytes, &ol)
	runtime.KeepAlive(f)
	return err
}
