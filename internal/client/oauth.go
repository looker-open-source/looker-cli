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
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/looker-open-source/looker-cli/internal/util"
	"github.com/pkg/browser"
)

var oauthStdin io.Reader = os.Stdin

const (
	redirectPort = "7777"
	redirectURI  = "http://127.0.0.1:" + redirectPort
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// OAuthRequestOptions configures HTTP client behavior for OAuth endpoints.
type OAuthRequestOptions struct {
	SSL          bool
	VerifySSL    bool
	Timeout      time.Duration
	HTTPProxy    string
	HTTPClient   *http.Client
	Headers      []Header
	callbackAddr string // optional listener override for tests; defaults to 127.0.0.1:7777
}

func resolveOAuthHTTPClient(opts OAuthRequestOptions) (*http.Client, error) {
	if opts.HTTPClient != nil {
		return opts.HTTPClient, nil
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = time.Duration(DefaultTimeoutSeconds) * time.Second
	}
	return buildHTTPClient(opts.VerifySSL, timeout, opts.HTTPProxy)
}

func randomBase64URL(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generatePKCE() (verifier, challenge string, err error) {
	verifier, err = randomBase64URL(32)
	if err != nil {
		return "", "", err
	}

	hasher := sha256.New()
	hasher.Write([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))

	return verifier, challenge, nil
}

func generateOAuthState() (string, error) {
	return randomBase64URL(32)
}

func newOAuthCallbackHandler(expectedState string, codeChan chan<- string, errChan chan<- error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		code := q.Get("code")
		oauthErr := q.Get("error")
		errDesc := q.Get("error_description")
		if code == "" && oauthErr == "" && errDesc == "" {
			http.Error(w, "Missing OAuth callback parameters", http.StatusBadRequest)
			return
		}

		gotState := q.Get("state")
		if expectedState == "" || subtle.ConstantTimeCompare([]byte(gotState), []byte(expectedState)) != 1 {
			http.Error(w, "Authorization failed: invalid or missing state parameter", http.StatusBadRequest)
			return
		}

		if code == "" {
			errMsg := errDesc
			if errMsg == "" {
				errMsg = oauthErr
			}
			if errMsg == "" {
				errMsg = "authorization failed"
			}
			http.Error(w, "Authorization failed: "+errMsg, http.StatusBadRequest)
			select {
			case errChan <- fmt.Errorf("oauth error: %s", errMsg):
			default:
			}
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<html><body><h1>Authorization Successful!</h1><p>You can close this window and return to the CLI.</p></body></html>")
		select {
		case codeChan <- code:
		default:
		}
	})
	return mux
}

// PerformOAuthLogin initiates PKCE flow, opens browser, waits for callback, and exchanges code for token.
func PerformOAuthLogin(ctx context.Context, host, port, clientID string, ssl bool, headers ...Header) (string, string, time.Time, error) {
	return PerformOAuthLoginWithOptions(ctx, host, port, clientID, OAuthRequestOptions{
		SSL:       ssl,
		VerifySSL: true,
		Timeout:   time.Duration(DefaultTimeoutSeconds) * time.Second,
		Headers:   headers,
	})
}

// PerformOAuthLoginWithOptions initiates PKCE flow using the provided HTTP client options.
func PerformOAuthLoginWithOptions(ctx context.Context, host, port, clientID string, opts OAuthRequestOptions) (string, string, time.Time, error) {
	host = util.CleanHost(host)

	httpClient, err := resolveOAuthHTTPClient(opts)
	if err != nil {
		return "", "", time.Time{}, err
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	state, err := generateOAuthState()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to generate OAuth state: %w", err)
	}

	scheme := "https"
	if !opts.SSL {
		scheme = "http"
	}

	stderrWriter := os.Stderr
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	listenAddr := "127.0.0.1:" + redirectPort
	if opts.callbackAddr != "" {
		listenAddr = opts.callbackAddr
	}
	cbRedirectURI := redirectURI

	ln, listenErr := net.Listen("tcp", listenAddr)
	if listenErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: local callback server failed to start: %v. Automatic redirect will not work, but you can still use the manual fallback.\n", listenErr)
	} else {
		if opts.callbackAddr != "" {
			cbRedirectURI = "http://" + ln.Addr().String()
		}
		srv := &http.Server{
			Handler:           newOAuthCallbackHandler(state, codeChan, errChan),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
				_, _ = fmt.Fprintf(stderrWriter, "Warning: local callback server stopped unexpectedly: %v\n", serveErr)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
	}

	// UI host for /auth
	uiURL := fmt.Sprintf("%s://%s/auth", scheme, host)
	authURL, err := url.Parse(uiURL)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("invalid auth URL: %w", err)
	}

	q := authURL.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", cbRedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "cors_api") // Looker API scope
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	authURL.RawQuery = q.Encode()

	if isSSHSession() {
		fmt.Fprintln(os.Stderr, "=======================================================================")
		fmt.Fprintln(os.Stderr, "SSH session detected! You can:")
		fmt.Fprintln(os.Stderr, "A. (Recommended) Copy the authorization URL below into your local browser.")
		fmt.Fprintln(os.Stderr, "   After authorizing, copy the redirected URL (even if it fails to load)")
		fmt.Fprintln(os.Stderr, "   and paste it back here to complete login.")
		fmt.Fprintln(os.Stderr, "B. Forward port 7777 back to your local machine:")
		fmt.Fprintln(os.Stderr, "   ssh -L 7777:127.0.0.1:7777 user@remote-host")
		fmt.Fprintln(os.Stderr, "=======================================================================")
	}

	fmt.Fprintf(os.Stderr, "Opening browser to URL: %s\n", authURL.String())
	browserOpened := true
	if err := browser.OpenURL(authURL.String()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open browser automatically. Please visit the URL above.\n")
		browserOpened = false
	}

	stdinCodeChan := make(chan string, 1)
	stdinErrChan := make(chan error, 1)
	stdinReader := oauthStdin

	loginCtx, cancelLogin := context.WithCancel(ctx)
	defer cancelLogin()

	go func() {
		// Only print fallback instructions if browser failed to open, or after 1.5s if no response yet
		if !browserOpened {
			_, _ = fmt.Fprintln(stderrWriter, "\n--- OAuth Stdin Fallback ---")
			_, _ = fmt.Fprintln(stderrWriter, "Please open the URL above in a browser.")
			_, _ = fmt.Fprintf(stderrWriter, "After authorizing, you will be redirected to %s/?code=...\n", cbRedirectURI)
			_, _ = fmt.Fprintln(stderrWriter, "Copy the full redirected URL and paste it below.")
			_, _ = fmt.Fprint(stderrWriter, "Paste redirected URL here: ")
		} else {
			// Wait a bit before printing instructions to not clutter automatic redirect flow
			select {
			case <-loginCtx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
				// If we got here, maybe browser is slow or didn't actually open, or redirect is taking time
				_, _ = fmt.Fprintln(stderrWriter, "\n--- OAuth Stdin Fallback ---")
				_, _ = fmt.Fprintln(stderrWriter, "If the browser did not open or redirect automatically, please:")
				_, _ = fmt.Fprintln(stderrWriter, "1. Copy the URL above into your browser.")
				_, _ = fmt.Fprintf(stderrWriter, "2. After authorizing, copy the redirected URL (starting with %s/?code=...)\n", cbRedirectURI)
				_, _ = fmt.Fprintln(stderrWriter, "3. Paste the URL here.")
				_, _ = fmt.Fprint(stderrWriter, "Paste redirected URL here: ")
			}
		}

		scanner := bufio.NewScanner(stdinReader)
		for {
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					select {
					case stdinErrChan <- fmt.Errorf("failed to read from stdin: %w", err):
					default:
					}
				}
				return
			}
			input := scanner.Text()
			code, err := parsePastedAuthInputWithState(input, state)
			if err != nil {
				_, _ = fmt.Fprintf(stderrWriter, "Error: %v. Please try again: ", err)
				continue
			}
			select {
			case stdinCodeChan <- code:
			default:
			}
			return
		}
	}()

	var code string
	select {
	case <-ctx.Done():
		return "", "", time.Time{}, ctx.Err()
	case err := <-errChan:
		return "", "", time.Time{}, err
	case err := <-stdinErrChan:
		return "", "", time.Time{}, err
	case code = <-codeChan:
		// Succeeded via local server redirect
	case code = <-stdinCodeChan:
		// Succeeded via copy-paste fallback
	}

	// Exchange code for token
	apiHost := host
	if port != "" {
		apiHost = host + ":" + port
	}
	tokenURL := fmt.Sprintf("%s://%s/api/token", scheme, apiHost)

	body := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"redirect_uri":  cbRedirectURI,
		"code":          code,
		"code_verifier": verifier,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("create token request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	applyHeaders(req.Header, opts.Headers)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyStr, _ := io.ReadAll(resp.Body)
		return "", "", time.Time{}, fmt.Errorf("token request returned status %d: %s", resp.StatusCode, string(bodyStr))
	}

	var tokResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to decode token response: %w", err)
	}

	expiration := time.Now().Add(time.Duration(tokResp.ExpiresIn) * time.Second)
	return tokResp.AccessToken, tokResp.RefreshToken, expiration, nil
}

// RefreshOAuthToken performs explicit OAuth2 refresh token grant to get a new short-lived access token.
func RefreshOAuthToken(ctx context.Context, host, port, clientID, refreshToken string, ssl bool, headers ...Header) (string, string, time.Time, error) {
	return RefreshOAuthTokenWithOptions(ctx, host, port, clientID, refreshToken, OAuthRequestOptions{
		SSL:       ssl,
		VerifySSL: true,
		Timeout:   time.Duration(DefaultTimeoutSeconds) * time.Second,
		Headers:   headers,
	})
}

// RefreshOAuthTokenWithOptions performs explicit OAuth2 refresh token grant using the provided HTTP client options.
func RefreshOAuthTokenWithOptions(ctx context.Context, host, port, clientID, refreshToken string, opts OAuthRequestOptions) (string, string, time.Time, error) {
	host = util.CleanHost(host)

	httpClient, err := resolveOAuthHTTPClient(opts)
	if err != nil {
		return "", "", time.Time{}, err
	}

	scheme := "https"
	if !opts.SSL {
		scheme = "http"
	}

	apiHost := host
	if port != "" {
		apiHost = host + ":" + port
	}
	tokenURL := fmt.Sprintf("%s://%s/api/token", scheme, apiHost)

	body := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": refreshToken,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("create refresh token request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	applyHeaders(req.Header, opts.Headers)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("refresh token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyStr, _ := io.ReadAll(resp.Body)
		return "", "", time.Time{}, fmt.Errorf("refresh token request returned status %d: %s", resp.StatusCode, string(bodyStr))
	}

	var tokResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to decode token response: %w", err)
	}

	expiration := time.Now().Add(time.Duration(tokResp.ExpiresIn) * time.Second)

	// Fallback to old refresh token if server did not issue a new one
	newRefreshToken := tokResp.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	return tokResp.AccessToken, newRefreshToken, expiration, nil
}

func isSSHSession() bool {
	return os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""
}

func parsePastedAuthInput(input string) (string, error) {
	return parsePastedAuthInputWithState(input, "")
}

func parsePastedAuthInputWithState(input, expectedState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	// Try to parse as URL
	u, err := url.Parse(input)
	if err == nil && u.Scheme != "" && u.Host != "" {
		q := u.Query()
		code := q.Get("code")
		if code == "" {
			return "", fmt.Errorf("pasted URL did not contain a 'code' parameter")
		}
		if expectedState != "" {
			gotState := q.Get("state")
			if subtle.ConstantTimeCompare([]byte(gotState), []byte(expectedState)) != 1 {
				return "", fmt.Errorf("invalid or missing 'state' parameter in pasted redirect URL")
			}
		}
		return code, nil
	}

	// Fallback: if it doesn't look like a URL but could be the code
	if len(input) > 5 && !strings.Contains(input, "/") && !strings.Contains(input, " ") {
		return input, nil
	}

	return "", fmt.Errorf("could not parse input as a redirect URL or authorization code")
}
