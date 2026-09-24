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
	"fmt"
	"os"
	"path/filepath"
	"time"
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

func ReadTokenData() (TokenData, error) {
	path, err := getTokenFilePath()
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(path)
	if os.IsNotExist(err) {
		return make(TokenData), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to stat token file: %w", err)
	}

	// Check permissions (should be 600)
	if stat.Mode().Perm() != 0600 {
		return nil, fmt.Errorf("token file %s has insecure permissions %s, expected 0600", path, stat.Mode().Perm())
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var data TokenData
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode token file: %w", err)
	}

	return data, nil
}

func WriteTokenData(data TokenData) error {
	path, err := getTokenFilePath()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open token file for writing: %w", err)
	}
	_ = os.Chmod(path, 0600)
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode token data: %w", err)
	}

	return nil
}

func GetTokenEntry(host, suUser string) (TokenEntry, error) {
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
	data, err := ReadTokenData()
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}

	hostTokens, ok := data[host]
	if !ok {
		return nil
	}

	key := "default"
	if suUser != "" {
		key = suUser
	}

	if _, exists := hostTokens[key]; !exists {
		return nil
	}

	delete(hostTokens, key)
	if len(hostTokens) == 0 {
		delete(data, host)
	}

	return WriteTokenData(data)
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
	data, err := ReadTokenData()
	if err != nil {
		return err
	}
	if data == nil {
		data = make(TokenData)
	}

	hostTokens, ok := data[host]
	if !ok {
		hostTokens = make(HostTokens)
		data[host] = hostTokens
	}

	key := "default"
	if suUser != "" {
		key = suUser
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
	}

	return WriteTokenData(data)
}
