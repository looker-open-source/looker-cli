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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/looker-open-source/looker-cli/internal/util"
)

const (
	tokenFileName        = ".looker_auth"
	TimeFormat           = "2006-01-02 15:04:05 -0700"
	RefreshTokenDuration = 30 * 24 * time.Hour
)

type TokenEntry struct {
	Token             string `json:"token"`
	Expiration        string `json:"expiration"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	RefreshExpiration string `json:"refresh_expiration,omitempty"`
	ClientID          string `json:"client_id,omitempty"`
	// Impersonated is true only for tokens issued by login_user for an --su
	// target. Entries stored under an su user key without this flag are never
	// trusted as impersonation tokens.
	Impersonated bool `json:"impersonated,omitempty"`
}

type HostTokens map[string]TokenEntry

type TokenData map[string]HostTokens

func getTokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}
	return filepath.Join(home, tokenFileName), nil
}

func checkSecureTokenFilePerm(goos string, mode os.FileMode, path string) error {
	// Windows synthesizes POSIX mode bits from the read-only attribute (0666 or
	// 0444), so POSIX group/other permission checks do not apply there.
	if goos == "windows" {
		return nil
	}
	if mode.Perm()&0o077 != 0 {
		return fmt.Errorf("token file %s has insecure permissions %04o, expected 0600 or 0400", path, mode.Perm())
	}
	return nil
}

func readTokenDataUnlocked(path string) (TokenData, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(TokenData), nil
		}
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat token file: %w", err)
	}

	if err := checkSecureTokenFilePerm(runtime.GOOS, stat.Mode(), path); err != nil {
		return nil, err
	}

	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var data TokenData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to decode token file: %w", err)
	}
	if data == nil {
		data = make(TokenData)
	}

	return data, nil
}

func writeTokenDataUnlocked(path string, data TokenData) error {
	if data == nil {
		data = make(TokenData)
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode token data: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := util.WriteFileAtomic0600(path, encoded); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}
	return nil
}

func updateTokenData(skipIfMissing bool, fn func(TokenData) (bool, error)) error {
	path, err := getTokenFilePath()
	if err != nil {
		return err
	}
	if skipIfMissing {
		if _, sErr := os.Stat(path); errors.Is(sErr, os.ErrNotExist) {
			if _, lErr := os.Stat(path + ".lock"); errors.Is(lErr, os.ErrNotExist) {
				return nil
			}
		}
	}
	return util.WithFileLock(path, func() error {
		data, err := readTokenDataUnlocked(path)
		if err != nil {
			return err
		}
		changed, err := fn(data)
		if err != nil || !changed {
			return err
		}
		return writeTokenDataUnlocked(path, data)
	})
}

func ReadTokenData() (TokenData, error) {
	path, err := getTokenFilePath()
	if err != nil {
		return nil, err
	}
	var data TokenData
	err = util.WithFileRLock(path, func() error {
		var rErr error
		data, rErr = readTokenDataUnlocked(path)
		return rErr
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func WriteTokenData(data TokenData) error {
	path, err := getTokenFilePath()
	if err != nil {
		return err
	}
	return util.WithFileLock(path, func() error {
		return writeTokenDataUnlocked(path, data)
	})
}

func GetTokenEntry(host, suUser string) (TokenEntry, error) {
	host = util.CleanHost(host)
	data, err := ReadTokenData()
	if err != nil {
		return TokenEntry{}, err
	}

	hostTokens, ok := data[host]
	if !ok {
		return TokenEntry{}, fmt.Errorf("no tokens found for host %s", host)
	}

	key := "default"
	if suUser != "" {
		key = suUser
	}

	entry, ok := hostTokens[key]
	if !ok {
		return TokenEntry{}, fmt.Errorf("no token found for host %s, user %s", host, key)
	}

	return entry, nil
}

func GetToken(host, suUser string) (string, error) {
	host = util.CleanHost(host)
	entry, err := GetTokenEntry(host, suUser)
	if err != nil {
		return "", err
	}

	exp, err := time.Parse(TimeFormat, entry.Expiration)
	if err != nil {
		_ = DeleteToken(host, suUser)
		return "", fmt.Errorf("failed to parse expiration time %s: %w", entry.Expiration, err)
	}

	if time.Now().After(exp.Add(-5 * time.Minute)) {
		_ = DeleteToken(host, suUser)
		return "", fmt.Errorf("token expired or expiring soon (at %s)", entry.Expiration)
	}

	return entry.Token, nil
}

func DeleteToken(host, suUser string) error {
	host = util.CleanHost(host)
	key := "default"
	if suUser != "" {
		key = suUser
	}

	return updateTokenData(true, func(data TokenData) (bool, error) {
		hostTokens, ok := data[host]
		if !ok {
			return false, nil
		}
		if _, exists := hostTokens[key]; !exists {
			return false, nil
		}
		delete(hostTokens, key)
		if len(hostTokens) == 0 {
			delete(data, host)
		}
		return true, nil
	})
}

func IsRefreshTokenExpired(refreshExpiration, accessExpiration string) bool {
	if refreshExpiration != "" {
		refExp, err := time.Parse(TimeFormat, refreshExpiration)
		if err != nil {
			return true
		}
		return time.Now().After(refExp.Add(-5 * time.Minute))
	}
	if accessExpiration != "" {
		exp, err := time.Parse(TimeFormat, accessExpiration)
		if err != nil {
			return true
		}
		return time.Now().After(exp.Add(RefreshTokenDuration - 5*time.Minute))
	}
	return false
}

func StoreToken(host, suUser, token, refreshToken, clientID string, expiration time.Time) error {
	return storeToken(host, suUser, token, refreshToken, clientID, expiration, false)
}

// StoreImpersonationToken stores a token obtained via login_user for suUser.
// Only tokens stored through this function are reused for --su sessions.
func StoreImpersonationToken(host, suUser, token string, expiration time.Time) error {
	if suUser == "" {
		return fmt.Errorf("impersonation token requires a target user")
	}
	return storeToken(host, suUser, token, "", "", expiration, true)
}

func storeToken(host, suUser, token, refreshToken, clientID string, expiration time.Time, impersonated bool) error {
	host = util.CleanHost(host)
	key := "default"
	if suUser != "" {
		key = suUser
	}

	return updateTokenData(false, func(data TokenData) (bool, error) {
		hostTokens, ok := data[host]
		if !ok {
			hostTokens = make(HostTokens)
			data[host] = hostTokens
		}

		refreshExpStr := ""
		if refreshToken != "" {
			if existing, exists := hostTokens[key]; exists && existing.RefreshToken == refreshToken && existing.RefreshExpiration != "" {
				refreshExpStr = existing.RefreshExpiration
			} else {
				baseTime := time.Now()
				if expiration.Before(baseTime) {
					baseTime = expiration
				}
				refreshExpStr = baseTime.Add(RefreshTokenDuration).Format(TimeFormat)
			}
		}

		hostTokens[key] = TokenEntry{
			Token:             token,
			Expiration:        expiration.Format(TimeFormat),
			RefreshToken:      refreshToken,
			RefreshExpiration: refreshExpStr,
			ClientID:          clientID,
			Impersonated:      impersonated,
		}
		return true, nil
	})
}
