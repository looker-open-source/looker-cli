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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"github.com/looker-open-source/looker-cli/internal/config"
	"golang.org/x/oauth2"
)

type ClientWrapper struct {
	SDK           *v4.LookerSDK
	Session       *rtl.AuthSession
	Host          string
	SuUser        string
	ActiveProfile string
}

var UserAgent = "looker-cli/unknown"

// NewClient initializes LookerSDK based on provided flags and auth mechanisms.
func NewClient(ctx context.Context, host, port, clientID, clientSecret, token, suUser string, ssl, verifySSL, oauth, tokenFile bool, activeProfile string) (*ClientWrapper, error) {
	scheme := "https"
	if !ssl {
		scheme = "http"
	}

	apiHost := host
	if port != "" {
		apiHost = host + ":" + port
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, apiHost)

	settings := rtl.ApiSettings{
		BaseUrl:    baseURL,
		ApiVersion: "4.0",
		VerifySsl:  verifySSL,
		Timeout:    120,
		AgentTag:   UserAgent,
		Headers:    make(map[string]string),
	}
	settings.Headers["User-Agent"] = UserAgent

	var activeToken string
	var expiredErr error
	tokenFromStorage := false
	activeClientID := clientID
	activeClientSecret := clientSecret

	var prof config.Profile
	if activeProfile != "" {
		cfg, err := config.Load()
		if err == nil {
			prof = cfg.Profiles[activeProfile]
		}
	}

	if token != "" {
		activeToken = token
	} else if activeProfile != "" && (prof.AccessToken != "" || prof.RefreshToken != "") && !oauth {
		expired := prof.AccessToken == ""
		if prof.Expiration != "" {
			exp, err := time.Parse(TimeFormat, prof.Expiration)
			if err != nil {
				expired = true
			} else if time.Now().After(exp.Add(-5 * time.Minute)) {
				expired = true
			}
		}

		refreshExpired := false
		if prof.RefreshToken != "" && IsRefreshTokenExpired(prof.RefreshExpiration, prof.Expiration) {
			refreshExpired = true
			prof.RefreshToken = ""
			prof.RefreshExpiration = ""
			if cfg, err := config.Load(); err == nil {
				cfg.Profiles[activeProfile] = prof
				_ = cfg.Save()
			}
		}

		if expired && prof.RefreshToken != "" {
			cID := activeClientID
			if cID == "" {
				cID = determineOAuthClientID(ctx, host, port, ssl, verifySSL)
			}
			tok, refTok, newExp, err := RefreshOAuthToken(ctx, host, port, cID, prof.RefreshToken, ssl)
			if err == nil {
				if refTok != prof.RefreshToken || prof.RefreshExpiration == "" {
					prof.RefreshExpiration = time.Now().Add(RefreshTokenDuration).Format(TimeFormat)
				}
				prof.AccessToken = tok
				prof.RefreshToken = refTok
				prof.Expiration = newExp.Format(TimeFormat)
				if cfg, err := config.Load(); err == nil {
					cfg.Profiles[activeProfile] = prof
					_ = cfg.Save()
				}
				activeToken = tok
				tokenFromStorage = true
			} else {
				ClearProfileToken(activeProfile)
				expiredErr = fmt.Errorf("token expired and refresh failed: %w", err)
			}
		} else if expired {
			ClearProfileToken(activeProfile)
			if refreshExpired {
				expiredErr = fmt.Errorf("token and refresh token expired")
			} else {
				expiredErr = fmt.Errorf("token expired and cannot be refreshed (no refresh token found)")
			}
		} else {
			activeToken = prof.AccessToken
			tokenFromStorage = true
		}
	}

	if activeToken == "" && tokenFile && !oauth {
		tokenUser := suUser
		entry, err := GetTokenEntry(host, tokenUser)
		if err == nil && suUser != "" {
			exp, parseErr := time.Parse(TimeFormat, entry.Expiration)
			if parseErr != nil || time.Now().After(exp.Add(-5*time.Minute)) {
				_ = DeleteToken(host, suUser)
				err = fmt.Errorf("su token expired")
			}
		}
		if err != nil && suUser != "" {
			tokenUser = ""
			entry, err = GetTokenEntry(host, "")
		}
		if err != nil {
			return nil, fmt.Errorf("auth required: no valid token found in file for host %s (%v)", host, err)
		}

		exp, err := time.Parse(TimeFormat, entry.Expiration)
		expired := entry.Token == ""
		if err != nil {
			expired = true
		} else if time.Now().After(exp.Add(-5 * time.Minute)) {
			expired = true
		}

		refreshExpired := false
		if entry.RefreshToken != "" && IsRefreshTokenExpired(entry.RefreshExpiration, entry.Expiration) {
			refreshExpired = true
			entry.RefreshToken = ""
			entry.RefreshExpiration = ""
		}

		if expired && entry.RefreshToken != "" {
			cID := entry.ClientID
			if cID == "" {
				cID = determineOAuthClientID(ctx, host, port, ssl, verifySSL)
			}
			tok, refTok, newExp, err := RefreshOAuthToken(ctx, host, port, cID, entry.RefreshToken, ssl)
			if err == nil {
				_ = StoreToken(host, tokenUser, tok, refTok, cID, newExp)
				activeToken = tok
				tokenFromStorage = true
			} else {
				_ = DeleteToken(host, tokenUser)
				expiredErr = fmt.Errorf("token expired and refresh failed: %w", err)
			}
		} else if expired {
			_ = DeleteToken(host, tokenUser)
			if refreshExpired {
				expiredErr = fmt.Errorf("token and refresh token expired")
			} else {
				expiredErr = fmt.Errorf("token expired and cannot be refreshed (no refresh token found)")
			}
		} else {
			if refreshExpired {
				_ = StoreToken(host, tokenUser, entry.Token, "", entry.ClientID, exp)
			}
			activeToken = entry.Token
			tokenFromStorage = true
		}
	}

	if activeClientID == "" || activeClientSecret == "" {
		cID, cSec, err := GetNetrcCredentials(host)
		if err == nil && cID != "" && cSec != "" {
			activeClientID = cID
			activeClientSecret = cSec
		} else {
			envSettings, _ := rtl.NewSettingsFromEnv()
			if envSettings.ClientId != "" && envSettings.ClientSecret != "" {
				activeClientID = envSettings.ClientId
				activeClientSecret = envSettings.ClientSecret
			}
		}
	}

	if activeToken != "" {
		if !strings.HasPrefix(activeToken, "Bearer ") && !strings.HasPrefix(activeToken, "token ") {
			activeToken = "Bearer " + activeToken
		}
		settings.Headers["Authorization"] = activeToken
		if activeClientID != "" && activeClientSecret != "" {
			settings.ClientId = activeClientID
			settings.ClientSecret = activeClientSecret
		}
	} else if oauth {
		cID := activeClientID
		if cID == "" {
			cID = determineOAuthClientID(ctx, host, port, ssl, verifySSL)
		}
		tok, refTok, exp, err := PerformOAuthLogin(ctx, host, port, cID, ssl)
		if err != nil {
			return nil, fmt.Errorf("oauth login failed: %w", err)
		}
		activeToken = tok
		tokenFromStorage = true
		if activeProfile != "" {
			if cfg, err := config.Load(); err == nil {
				p := cfg.Profiles[activeProfile]
				p.AccessToken = tok
				p.RefreshToken = refTok
				p.Expiration = exp.Format(TimeFormat)
				if refTok != "" {
					p.RefreshExpiration = time.Now().Add(RefreshTokenDuration).Format(TimeFormat)
				} else {
					p.RefreshExpiration = ""
				}
				cfg.Profiles[activeProfile] = p
				_ = cfg.Save()
			}
		} else {
			_ = StoreToken(host, suUser, tok, refTok, cID, exp)
		}
		if !strings.HasPrefix(activeToken, "Bearer ") && !strings.HasPrefix(activeToken, "token ") {
			activeToken = "Bearer " + activeToken
		}
		settings.Headers["Authorization"] = activeToken
	} else {
		if activeClientID != "" && activeClientSecret != "" {
			settings.ClientId = activeClientID
			settings.ClientSecret = activeClientSecret
		} else if expiredErr != nil {
			return nil, expiredErr
		} else {
			return nil, fmt.Errorf("auth required: must provide token, oauth, netrc, or client_id/secret")
		}
	}

	session := rtl.NewAuthSession(settings)

	if activeToken != "" {
		rawToken := activeToken
		if strings.HasPrefix(rawToken, "Bearer ") {
			rawToken = strings.TrimPrefix(rawToken, "Bearer ")
		} else if strings.HasPrefix(rawToken, "token ") {
			rawToken = strings.TrimPrefix(rawToken, "token ")
		}

		if oauth2Trans, ok := session.Client.Transport.(*oauth2.Transport); ok {
			var fallbackSource oauth2.TokenSource
			if settings.ClientId != "" && settings.ClientSecret != "" {
				fallbackSource = oauth2Trans.Source
			}
			oauth2Trans.Source = oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: rawToken,
			})
			if tokenFromStorage {
				oauth2Trans.Base = &tokenCleanupTransport{
					base:           oauth2Trans.Base,
					oauth2Trans:    oauth2Trans,
					session:        session,
					fallbackSource: fallbackSource,
					onUnauthorized: func() {
						if activeProfile != "" {
							ClearProfileToken(activeProfile)
						}
						_ = DeleteToken(host, suUser)
						if suUser != "" {
							_ = DeleteToken(host, "")
						}
					},
				}
			}
		}
	}

	sdk := v4.NewLookerSDK(session)

	wrapper := &ClientWrapper{
		SDK:           sdk,
		Session:       session,
		Host:          host,
		SuUser:        suUser,
		ActiveProfile: activeProfile,
	}

	if suUser != "" && (settings.ClientId != "" || settings.Headers["Authorization"] != "") {
		tok, err := GetToken(host, suUser)
		if err != nil || tok == "" || settings.Headers["Authorization"] != "Bearer "+tok {
			suTokResp, err := sdk.LoginUser(suUser, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to su to user %s: %w", suUser, err)
			}
			if suTokResp.AccessToken == nil || suTokResp.ExpiresIn == nil {
				return nil, fmt.Errorf("invalid su token response")
			}

			suTok := "Bearer " + *suTokResp.AccessToken
			session.Config.Headers["Authorization"] = suTok
			if oauth2Trans, ok := session.Client.Transport.(*oauth2.Transport); ok {
				oauth2Trans.Source = oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: *suTokResp.AccessToken,
				})
			}
			exp := time.Now().Add(time.Duration(*suTokResp.ExpiresIn) * time.Second)
			_ = StoreToken(host, suUser, *suTokResp.AccessToken, "", "", exp)
		}
	}

	return wrapper, nil
}

type tokenCleanupTransport struct {
	base           http.RoundTripper
	oauth2Trans    *oauth2.Transport
	session        *rtl.AuthSession
	fallbackSource oauth2.TokenSource
	onUnauthorized func()
	retried        bool
}

func (t *tokenCleanupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil && resp != nil && resp.StatusCode == http.StatusUnauthorized {
		if t.onUnauthorized != nil {
			t.onUnauthorized()
		}
		if t.session != nil && t.session.Config.Headers != nil {
			delete(t.session.Config.Headers, "Authorization")
		}
		if t.fallbackSource != nil && !t.retried {
			t.retried = true
			if t.oauth2Trans != nil {
				t.oauth2Trans.Source = t.fallbackSource
			}
			if req.Body == nil || req.GetBody != nil {
				if tok, tokErr := t.fallbackSource.Token(); tokErr == nil {
					_ = resp.Body.Close()
					retryReq := req.Clone(req.Context())
					if req.Body != nil && req.GetBody != nil {
						if body, bodyErr := req.GetBody(); bodyErr == nil {
							retryReq.Body = body
						}
					}
					retryReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
					return t.base.RoundTrip(retryReq)
				}
			}
		}
	}
	return resp, err
}

// ClearProfileToken deletes any stored auth token from the specified profile in config.yaml.
func ClearProfileToken(activeProfile string) {
	if activeProfile == "" {
		return
	}
	if cfg, err := config.Load(); err == nil && cfg != nil {
		if prof, ok := cfg.Profiles[activeProfile]; ok {
			if prof.AccessToken != "" || prof.RefreshToken != "" || prof.Expiration != "" || prof.RefreshExpiration != "" {
				prof.AccessToken = ""
				prof.RefreshToken = ""
				prof.Expiration = ""
				prof.RefreshExpiration = ""
				cfg.Profiles[activeProfile] = prof
				_ = cfg.Save()
			}
		}
	}
}

// ExplicitLogin performs explicit API login to get token details for 'session login'.
func (c *ClientWrapper) ExplicitLogin(clientID, clientSecret string) (string, time.Time, error) {
	resp, err := c.SDK.Login(v4.RequestLogin{ClientId: &clientID, ClientSecret: &clientSecret}, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.AccessToken == nil || resp.ExpiresIn == nil {
		return "", time.Time{}, fmt.Errorf("invalid login response")
	}
	exp := time.Now().Add(time.Duration(*resp.ExpiresIn) * time.Second)
	return *resp.AccessToken, exp, nil
}

// Logout explicitly logs out and deletes any stored auth token.
func (c *ClientWrapper) Logout() error {
	_, err := c.SDK.Logout(nil)
	if c.ActiveProfile != "" {
		ClearProfileToken(c.ActiveProfile)
	}
	if c.Host != "" {
		_ = DeleteToken(c.Host, c.SuUser)
	}
	return err
}

type swaggerInfo struct {
	Info struct {
		ReleaseVersion string `json:"x-looker-release-version"`
	} `json:"info"`
}

func determineOAuthClientID(ctx context.Context, host, port string, ssl, verifySSL bool) string {
	version, err := fetchLookerVersion(ctx, host, port, ssl, verifySSL)
	if err != nil {
		// Default fallback on failure
		return "com.looker.cli"
	}
	if compareVersionLessThanOrEqual26_8(version) {
		return "looker-cli"
	}
	return "com.looker.cli"
}

func fetchLookerVersion(ctx context.Context, host, port string, ssl, verifySSL bool) (string, error) {
	scheme := "https"
	if !ssl {
		scheme = "http"
	}
	u := fmt.Sprintf("%s://%s:%s/api/4.0/swagger.json", scheme, host, port)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySSL},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch swagger.json, status: %s", resp.Status)
	}

	var info swaggerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}

	return info.Info.ReleaseVersion, nil
}

func compareVersionLessThanOrEqual26_8(versionStr string) bool {
	// E.g. "26.8.9" or "25.20.1"
	parts := strings.Split(versionStr, ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if major < 26 {
		return true
	}
	if major == 26 && minor <= 8 {
		return true
	}
	return false
}
