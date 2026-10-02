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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/browser"
)

var oauthStdin io.Reader = os.Stdin


const (
	redirectPort = "7777"
	redirectURI  = "http://127.0.0.1:" + redirectPort
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func generatePKCE() (verifier, challenge string, err error) {
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(verifierBytes)

	hasher := sha256.New()
	hasher.Write([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))

	return verifier, challenge, nil
}

// PerformOAuthLogin initiates PKCE flow, opens browser, waits for callback, and exchanges code for token.
func PerformOAuthLogin(ctx context.Context, host, port, clientID string, ssl bool, headers ...Header) (string, string, time.Time, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	scheme := "https"
	if !ssl {
		scheme = "http"
	}

	// UI host for /auth
	uiHost := host
	uiURL := fmt.Sprintf("%s://%s/auth", scheme, uiHost)

	authURL, err := url.Parse(uiURL)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("invalid auth URL: %w", err)
	}

	q := authURL.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "cors_api") // Looker API scope
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    "127.0.0.1:" + redirectPort,
		Handler: mux,
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errDesc := r.URL.Query().Get("error_description")
			http.Error(w, "Authorization failed: "+errDesc, http.StatusBadRequest)
			errChan <- fmt.Errorf("oauth error: %s", errDesc)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, "<html><body><h1>Authorization Successful!</h1><p>You can close this window and return to the CLI.</p></body></html>")
		codeChan <- code
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Warning: local callback server failed to start: %v. Automatic redirect will not work, but you can still use the manual fallback.\n", err)
		}
	}()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	if isSSHSession() {
		fmt.Println("=======================================================================")
		fmt.Println("SSH session detected! You can:")
		fmt.Println("A. (Recommended) Copy the authorization URL below into your local browser.")
		fmt.Println("   After authorizing, copy the redirected URL (even if it fails to load)")
		fmt.Println("   and paste it back here to complete login.")
		fmt.Println("B. Forward port 7777 back to your local machine:")
		fmt.Println("   ssh -L 7777:127.0.0.1:7777 user@remote-host")
		fmt.Println("=======================================================================")
	}

	fmt.Printf("Opening browser to URL: %s\n", authURL.String())
	browserOpened := true
	if err := browser.OpenURL(authURL.String()); err != nil {
		fmt.Printf("Failed to open browser automatically. Please visit the URL above.\n")
		browserOpened = false
	}

	stdinCodeChan := make(chan string, 1)
	stdinErrChan := make(chan error, 1)

	go func() {
		// Only print fallback instructions if browser failed to open, or after 1.5s if no response yet
		if !browserOpened {
			fmt.Println("\n--- OAuth Stdin Fallback ---")
			fmt.Println("Please open the URL above in a browser.")
			fmt.Printf("After authorizing, you will be redirected to %s/?code=...\n", redirectURI)
			fmt.Println("Copy the full redirected URL and paste it below.")
			fmt.Print("Paste redirected URL here: ")
		} else {
			// Wait a bit before printing instructions to not clutter automatic redirect flow
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
				// If we got here, maybe browser is slow or didn't actually open, or redirect is taking time
				fmt.Println("\n--- OAuth Stdin Fallback ---")
				fmt.Println("If the browser did not open or redirect automatically, please:")
				fmt.Println("1. Copy the URL above into your browser.")
				fmt.Printf("2. After authorizing, copy the redirected URL (starting with %s/?code=...)\n", redirectURI)
				fmt.Println("3. Paste the URL here.")
				fmt.Print("Paste redirected URL here: ")
			}
		}

		scanner := bufio.NewScanner(oauthStdin)
		for {
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					stdinErrChan <- fmt.Errorf("failed to read from stdin: %w", err)
				}
				return
			}
			input := scanner.Text()
			code, err := parsePastedAuthInput(input)
			if err != nil {
				fmt.Printf("Error: %v. Please try again: ", err)
				continue
			}
			stdinCodeChan <- code
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
			"redirect_uri":  redirectURI,
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
		applyHeaders(req.Header, headers)

		resp, err := http.DefaultClient.Do(req)
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
	scheme := "https"
	if !ssl {
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
	applyHeaders(req.Header, headers)

	resp, err := http.DefaultClient.Do(req)
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
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	// Try to parse as URL
	u, err := url.Parse(input)
	if err == nil && u.Scheme != "" && u.Host != "" {
		code := u.Query().Get("code")
		if code != "" {
			return code, nil
		}
		return "", fmt.Errorf("pasted URL did not contain a 'code' parameter")
	}

	// Fallback: if it doesn't look like a URL but could be the code
	if len(input) > 5 && !strings.Contains(input, "/") && !strings.Contains(input, " ") {
		return input, nil
	}

	return "", fmt.Errorf("could not parse input as a redirect URL or authorization code")
}


