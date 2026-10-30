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
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/looker-open-source/looker-cli/internal/config"
	"github.com/looker-open-source/looker-cli/internal/util"
	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const DefaultTimeoutSeconds = 120

type ClientWrapper struct {
	SDK           *v4.LookerSDK
	Session       *rtl.AuthSession
	Host          string
	SuUser        string
	ActiveProfile string
}

// ClientOptions configures Looker SDK client initialization.
type ClientOptions struct {
	Host          string
	Port          string
	ClientID      string
	ClientSecret  string
	Token         string
	SuUser        string
	SSL           bool
	VerifySSL     bool
	OAuth         bool
	TokenFile     bool
	ActiveProfile string
	Timeout       int // seconds; if <= 0, defaults to DefaultTimeoutSeconds
	HTTPProxy     string
	Headers       []string
}

var UserAgent = "looker-cli/unknown"

// Header represents a parsed HTTP header key-value pair.
type Header struct {
	Key   string
	Value string
}

// ParseHeaders parses a slice of "Key: Value" strings into Header structs.
func ParseHeaders(rawHeaders []string) ([]Header, error) {
	var parsed []Header
	for _, raw := range rawHeaders {
		k, v, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("invalid header %q: must be in 'Key: Value' format", raw)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("invalid header %q: header name cannot be empty", raw)
		}
		v = strings.TrimSpace(v)
		parsed = append(parsed, Header{Key: k, Value: v})
	}
	return parsed, nil
}

func applyHeaders(h http.Header, headers []Header) {
	for _, hdr := range headers {
		h.Del(hdr.Key)
	}
	for _, hdr := range headers {
		h.Add(hdr.Key, hdr.Value)
	}
}

// parseHTTPProxyURL parses and validates an HTTP/HTTPS/SOCKS5 proxy URL.
// It returns (nil, nil) when raw is empty or whitespace-only.
func parseHTTPProxyURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid --http-proxy URL %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid --http-proxy URL %q: must include scheme and host (e.g. http://proxy.example.com:8080)", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return u, nil
	default:
		return nil, fmt.Errorf("invalid --http-proxy URL %q: unsupported scheme %q (expected http, https, socks5, or socks5h)", raw, u.Scheme)
	}
}

// ValidateHTTPProxy validates a proxy URL string if non-empty.
func ValidateHTTPProxy(raw string) error {
	_, err := parseHTTPProxyURL(raw)
	return err
}

func buildHTTPTransport(verifySSL bool, httpProxy string) (*http.Transport, error) {
	proxyURL, err := parseHTTPProxyURL(httpProxy)
	if err != nil {
		return nil, err
	}

	var tr *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = dt.Clone()
	} else {
		tr = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		}
	}

	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: !verifySSL}
	} else {
		tr.TLSClientConfig = tr.TLSClientConfig.Clone()
		tr.TLSClientConfig.InsecureSkipVerify = !verifySSL
	}

	if proxyURL != nil {
		tr.Proxy = http.ProxyURL(proxyURL)
	}
	return tr, nil
}

func buildHTTPClient(verifySSL bool, timeout time.Duration, httpProxy string) (*http.Client, error) {
	tr, err := buildHTTPTransport(verifySSL, httpProxy)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}, nil
}

// NewClient initializes LookerSDK based on provided flags and auth mechanisms.
func NewClient(ctx context.Context, host, port, clientID, clientSecret, token, suUser string, ssl, verifySSL, oauth, tokenFile bool, activeProfile string, headers ...string) (*ClientWrapper, error) {
	return NewClientWithOptions(ctx, ClientOptions{
		Host:          host,
		Port:          port,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Token:         token,
		SuUser:        suUser,
		SSL:           ssl,
		VerifySSL:     verifySSL,
		OAuth:         oauth,
		TokenFile:     tokenFile,
		ActiveProfile: activeProfile,
		Timeout:       DefaultTimeoutSeconds,
		Headers:       headers,
	})
}

// NewClientWithOptions initializes LookerSDK using ClientOptions.
func NewClientWithOptions(ctx context.Context, opts ClientOptions) (*ClientWrapper, error) {
	host := util.CleanHost(opts.Host)
	port := opts.Port
	clientID := opts.ClientID
	clientSecret := opts.ClientSecret
	token := opts.Token
	suUser := opts.SuUser
	ssl := opts.SSL
	verifySSL := opts.VerifySSL
	oauth := opts.OAuth
	tokenFile := opts.TokenFile
	activeProfile := opts.ActiveProfile

	parsedHeaders, err := ParseHeaders(opts.Headers)
	if err != nil {
		return nil, err
	}

	timeoutSec := opts.Timeout
	if timeoutSec <= 0 {
		timeoutSec = DefaultTimeoutSeconds
	}
	if timeoutSec > math.MaxInt32 {
		return nil, fmt.Errorf("timeout %d exceeds maximum supported seconds", timeoutSec)
	}
	timeoutDuration := time.Duration(timeoutSec) * time.Second

	rawTransport, err := buildHTTPTransport(verifySSL, opts.HTTPProxy)
	if err != nil {
		return nil, err
	}
	oauthHTTPClient := &http.Client{
		Transport: rawTransport,
		Timeout:   timeoutDuration,
	}
	probeTimeout := 10 * time.Second
	if timeoutDuration < probeTimeout {
		probeTimeout = timeoutDuration
	}
	probeHTTPClient := &http.Client{
		Transport: rawTransport,
		Timeout:   probeTimeout,
	}
	oauthOpts := OAuthRequestOptions{
		SSL:        ssl,
		VerifySSL:  verifySSL,
		Timeout:    timeoutDuration,
		HTTPProxy:  opts.HTTPProxy,
		HTTPClient: oauthHTTPClient,
		Headers:    parsedHeaders,
	}

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
		Timeout:    int32(timeoutSec),
		AgentTag:   UserAgent,
		Headers:    make(map[string]string),
	}
	settings.Headers["User-Agent"] = UserAgent

	var customAuthHeader string
	for _, h := range parsedHeaders {
		if strings.EqualFold(h.Key, "Authorization") {
			customAuthHeader = h.Value
		}
	}

	var activeToken string
	var expiredErr error
	tokenFromStorage := false
	// usingImpersonationToken is true only when activeToken is a login_user
	// token previously issued for suUser, which makes a fresh su unnecessary.
	usingImpersonationToken := false
	activeClientID := clientID
	activeClientSecret := clientSecret

	var prof config.Profile
	if activeProfile != "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("failed to load config for profile %q: %w", activeProfile, err)
		}
		prof = cfg.Profiles[activeProfile]
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
			_ = config.UpdateIfChanged(true, func(cfg *config.Config) (bool, error) {
				p, ok := cfg.Profiles[activeProfile]
				if !ok || (p.RefreshToken == "" && p.RefreshExpiration == "") {
					return false, nil
				}
				p.RefreshToken = ""
				p.RefreshExpiration = ""
				cfg.Profiles[activeProfile] = p
				return true, nil
			})
		}

		if expired && prof.RefreshToken != "" {
			cID := activeClientID
			if cID == "" {
				cID = determineOAuthClientID(ctx, probeHTTPClient, host, port, ssl, parsedHeaders...)
			}
			tok, refTok, newExp, err := RefreshOAuthTokenWithOptions(ctx, host, port, cID, prof.RefreshToken, oauthOpts)
			if err == nil {
				refExp := prof.RefreshExpiration
				if refTok != prof.RefreshToken || refExp == "" {
					refExp = time.Now().Add(RefreshTokenDuration).Format(TimeFormat)
				}
				prof.AccessToken = tok
				prof.RefreshToken = refTok
				prof.Expiration = newExp.Format(TimeFormat)
				prof.RefreshExpiration = refExp
				if storeErr := config.Update(func(cfg *config.Config) error {
					p := cfg.Profiles[activeProfile]
					p.AccessToken = tok
					p.RefreshToken = refTok
					p.Expiration = prof.Expiration
					p.RefreshExpiration = refExp
					cfg.Profiles[activeProfile] = p
					return nil
				}); storeErr != nil {
					return nil, fmt.Errorf("failed to store refreshed token in profile %q: %w", activeProfile, storeErr)
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
			if !entry.Impersonated {
				// Never trust a non-impersonation token stored under an su
				// user key (older versions stored the caller's own token
				// there), since using it would skip the su entirely.
				_ = DeleteToken(host, suUser)
				err = fmt.Errorf("stored token for user %s is not an impersonation token", suUser)
			} else if parseErr != nil || time.Now().After(exp.Add(-5*time.Minute)) {
				_ = DeleteToken(host, suUser)
				err = fmt.Errorf("su token expired")
			}
		}
		if err != nil && suUser != "" {
			tokenUser = ""
			entry, err = GetTokenEntry(host, "")
		}
		if err != nil {
			return nil, fmt.Errorf("auth required: no valid token found in file for host %s (%w)", host, err)
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
				cID = determineOAuthClientID(ctx, probeHTTPClient, host, port, ssl, parsedHeaders...)
			}
			tok, refTok, newExp, err := RefreshOAuthTokenWithOptions(ctx, host, port, cID, entry.RefreshToken, oauthOpts)
			if err == nil {
				if storeErr := StoreToken(host, tokenUser, tok, refTok, cID, newExp); storeErr != nil {
					return nil, fmt.Errorf("failed to store refreshed token: %w", storeErr)
				}
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
			usingImpersonationToken = tokenUser != "" && entry.Impersonated
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

	if activeToken == "" && customAuthHeader != "" && !oauth && (activeClientID == "" || activeClientSecret == "") {
		activeToken = customAuthHeader
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
			cID = determineOAuthClientID(ctx, probeHTTPClient, host, port, ssl, parsedHeaders...)
		}
		tok, refTok, exp, err := PerformOAuthLoginWithOptions(ctx, host, port, cID, oauthOpts)
		if err != nil {
			return nil, fmt.Errorf("oauth login failed: %w", err)
		}
		activeToken = tok
		tokenFromStorage = true
		if activeProfile != "" {
			if err := config.Update(func(cfg *config.Config) error {
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
				return nil
			}); err != nil {
				return nil, fmt.Errorf("failed to store oauth token in profile %q: %w", activeProfile, err)
			}
		} else {
			// The OAuth token belongs to the caller, never to the su target,
			// so it always goes in the default slot.
			if err := StoreToken(host, "", tok, refTok, cID, exp); err != nil {
				return nil, fmt.Errorf("failed to store oauth token: %w", err)
			}
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

	for _, h := range parsedHeaders {
		settings.Headers[h.Key] = h.Value
	}

	baseTransport := http.RoundTripper(rawTransport)
	var hdrTrans *headerTransport
	if len(parsedHeaders) > 0 {
		hdrTrans = &headerTransport{
			base:    baseTransport,
			headers: append([]Header(nil), parsedHeaders...),
		}
		baseTransport = hdrTrans
	}

	session := rtl.NewAuthSessionWithTransport(settings, baseTransport)
	session.Client.Timeout = timeoutDuration

	var tokenSrc *switchableTokenSource
	if oauth2Trans, ok := session.Client.Transport.(*oauth2.Transport); ok {
		if settings.ClientId != "" && settings.ClientSecret != "" {
			oauthCfg := clientcredentials.Config{
				ClientID:     settings.ClientId,
				ClientSecret: settings.ClientSecret,
				TokenURL:     fmt.Sprintf("%s/api/%s/login", settings.BaseUrl, settings.ApiVersion),
				AuthStyle:    oauth2.AuthStyleInParams,
			}
			tokenHTTPClient := &http.Client{
				Transport: oauth2Trans.Base,
				Timeout:   timeoutDuration,
			}
			tokenCtx := context.WithValue(context.Background(), oauth2.HTTPClient, tokenHTTPClient)
			oauth2Trans.Source = oauthCfg.TokenSource(tokenCtx)
		}

		if activeToken != "" {
			rawToken := activeToken
			tokenType := "Bearer"
			if strings.HasPrefix(rawToken, "Bearer ") {
				rawToken = strings.TrimPrefix(rawToken, "Bearer ")
			} else if strings.HasPrefix(rawToken, "token ") {
				rawToken = strings.TrimPrefix(rawToken, "token ")
				tokenType = "token"
			}

			var fallbackSource oauth2.TokenSource
			if settings.ClientId != "" && settings.ClientSecret != "" && suUser == "" {
				fallbackSource = oauth2Trans.Source
			}

			tokenSrc = &switchableTokenSource{
				current: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: rawToken,
					TokenType:   tokenType,
				}),
			}
			oauth2Trans.Source = tokenSrc

			if tokenFromStorage {
				oauth2Trans.Base = &tokenCleanupTransport{
					base:           oauth2Trans.Base,
					tokenSource:    tokenSrc,
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

	// Always su unless the active token is already a stored login_user token
	// for this exact user. Comparing against whatever happens to be stored
	// under the su key isn't enough: that let a caller's own token be
	// mistaken for an impersonation token.
	if suUser != "" && !usingImpersonationToken && (settings.ClientId != "" || settings.Headers["Authorization"] != "") {
		suTokResp, err := sdk.LoginUser(suUser, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to su to user %s: %w", suUser, err)
		}
		if suTokResp.AccessToken == nil || suTokResp.ExpiresIn == nil {
			return nil, fmt.Errorf("invalid su token response")
		}

		suTok := "Bearer " + *suTokResp.AccessToken
		session.Config.Headers["Authorization"] = suTok
		if hdrTrans != nil {
			hdrTrans.removeHeader("Authorization")
		}
		suSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: *suTokResp.AccessToken,
		})
		if tokenSrc != nil {
			tokenSrc.Set(suSource)
		} else if oauth2Trans, ok := session.Client.Transport.(*oauth2.Transport); ok {
			oauth2Trans.Source = suSource
		}
		exp := time.Now().Add(time.Duration(*suTokResp.ExpiresIn) * time.Second)
		_ = StoreImpersonationToken(host, suUser, *suTokResp.AccessToken, exp)
	}

	return wrapper, nil
}

type headerTransport struct {
	base    http.RoundTripper
	mu      sync.RWMutex
	headers []Header
}

func (t *headerTransport) removeHeader(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	filtered := make([]Header, 0, len(t.headers))
	for _, h := range t.headers {
		if !strings.EqualFold(h.Key, key) {
			filtered = append(filtered, h)
		}
	}
	t.headers = filtered
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	headers := t.headers
	t.mu.RUnlock()
	if len(headers) == 0 {
		return t.base.RoundTrip(req)
	}
	reqCopy := req.Clone(req.Context())
	applyHeaders(reqCopy.Header, headers)
	return t.base.RoundTrip(reqCopy)
}

type switchableTokenSource struct {
	mu      sync.RWMutex
	current oauth2.TokenSource
}

func (s *switchableTokenSource) Token() (*oauth2.Token, error) {
	s.mu.RLock()
	src := s.current
	s.mu.RUnlock()
	return src.Token()
}

func (s *switchableTokenSource) Set(src oauth2.TokenSource) {
	s.mu.Lock()
	s.current = src
	s.mu.Unlock()
}

type tokenCleanupTransport struct {
	base           http.RoundTripper
	tokenSource    *switchableTokenSource
	fallbackSource oauth2.TokenSource
	onUnauthorized func()
	cleanupOnce    sync.Once
	mu             sync.Mutex
	retried        bool
}

func (t *tokenCleanupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil && resp != nil && resp.StatusCode == http.StatusUnauthorized {
		if t.onUnauthorized != nil {
			t.cleanupOnce.Do(t.onUnauthorized)
		}
		if t.fallbackSource != nil {
			t.mu.Lock()
			shouldRetry := !t.retried
			if shouldRetry {
				t.retried = true
				if t.tokenSource != nil {
					t.tokenSource.Set(t.fallbackSource)
				}
			}
			t.mu.Unlock()

			if shouldRetry && (req.Body == nil || req.GetBody != nil) {
				if tok, tokErr := t.fallbackSource.Token(); tokErr == nil {
					_ = resp.Body.Close()
					retryReq := req.Clone(req.Context())
					if req.Body != nil && req.GetBody != nil {
						if body, bodyErr := req.GetBody(); bodyErr == nil {
							retryReq.Body = body
						}
					}
					tok.SetAuthHeader(retryReq)
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
	_ = config.UpdateIfChanged(true, func(cfg *config.Config) (bool, error) {
		prof, ok := cfg.Profiles[activeProfile]
		if !ok {
			return false, nil
		}
		if prof.AccessToken == "" && prof.RefreshToken == "" && prof.Expiration == "" && prof.RefreshExpiration == "" {
			return false, nil
		}
		prof.AccessToken = ""
		prof.RefreshToken = ""
		prof.Expiration = ""
		prof.RefreshExpiration = ""
		cfg.Profiles[activeProfile] = prof
		return true, nil
	})
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

func determineOAuthClientID(ctx context.Context, httpClient *http.Client, host, port string, ssl bool, headers ...Header) string {
	version, err := fetchLookerVersion(ctx, httpClient, host, port, ssl, headers...)
	if err != nil {
		// Default fallback on failure
		return "com.looker.cli"
	}
	if compareVersionLessThanOrEqual26_8(version) {
		return "looker-cli"
	}
	return "com.looker.cli"
}

func fetchLookerVersion(ctx context.Context, httpClient *http.Client, host, port string, ssl bool, headers ...Header) (string, error) {
	scheme := "https"
	if !ssl {
		scheme = "http"
	}
	apiHost := host
	if port != "" {
		apiHost = host + ":" + port
	}
	u := fmt.Sprintf("%s://%s/api/4.0/swagger.json", scheme, apiHost)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	applyHeaders(req.Header, headers)

	if httpClient == nil {
		var buildErr error
		httpClient, buildErr = buildHTTPClient(true, 10*time.Second, "")
		if buildErr != nil {
			return "", buildErr
		}
	}

	resp, err := httpClient.Do(req)
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
