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
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// Profile represents a Looker CLI profile.
type Profile struct {
	Host              string `yaml:"host"`
	Port              string `yaml:"port"`
	ClientID          string `yaml:"client_id,omitempty"`
	ClientSecret      string `yaml:"client_secret,omitempty"`
	AccessToken       string `yaml:"access_token,omitempty"`
	RefreshToken      string `yaml:"refresh_token,omitempty"`
	Expiration        string `yaml:"expiration,omitempty"`
	RefreshExpiration string `yaml:"refresh_expiration,omitempty"`
	SSL               *bool  `yaml:"ssl,omitempty"`
	VerifySSL         *bool  `yaml:"verify_ssl,omitempty"`
}

// Config represents the structure of the config file.
type Config struct {
	Default  string             `yaml:"default"`
	Profiles map[string]Profile `yaml:"profiles"`
}

var ConfigPathOverride string

// ConfigPath returns the path to the config file.
func ConfigPath() (string, error) {
	if ConfigPathOverride != "" {
		return ConfigPathOverride, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "looker-cli", "config.yaml"), nil
}

// Load loads the config from the config file.
// If the file does not exist, it returns an empty Config.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{
				Profiles: make(map[string]Profile),
			}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	return &cfg, nil
}

// Save saves the config to the config file.
func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
