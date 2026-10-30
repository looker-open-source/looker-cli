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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/looker-open-source/looker-cli/internal/config"
)

func TestOAuthCallbackHandler_StateAndNoise(t *testing.T) {
	const expectedState = "expected_state_token_123456"
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	handler := newOAuthCallbackHandler(expectedState, codeChan, errChan)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	assertChannelsEmpty := func(step string) {
		t.Helper()
		select {
		case c := <-codeChan:
			t.Fatalf("%s: unexpected code %q on codeChan", step, c)
		case err := <-errChan:
			t.Fatalf("%s: unexpected error %v on errChan", step, err)
		default:
		}
	}

	// 1. Browser background requests to non-root paths must return 404 and not poison errChan.
	for _, noisyPath := range []string{"/favicon.ico", "/.well-known/appspecific/com.chrome.devtools.json"} {
		resp, err := http.Get(srv.URL + noisyPath)
		if err != nil {
			t.Fatalf("GET %s failed: %v", noisyPath, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", noisyPath, resp.StatusCode)
		}
		assertChannelsEmpty(noisyPath)
	}

	// 2. Non-GET requests must return 405 and leave channels untouched.
	resp, err := http.Post(srv.URL+"/?code=evil&state="+expectedState, "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST / failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status = %d, want 405", resp.StatusCode)
	}
	assertChannelsEmpty("POST /")

	// 3. Bare GET / without OAuth parameters returns 400 without aborting login.
	resp, err = http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET / status = %d, want 400", resp.StatusCode)
	}
	assertChannelsEmpty("bare GET /")

	// 4. Forged code or error callbacks with missing/mismatched state must be rejected without touching channels.
	for _, forgedQuery := range []string{
		"/?code=attacker_code",
		"/?code=attacker_code&state=wrong_state",
		"/?error=access_denied",
		"/?error=access_denied&state=wrong_state",
	} {
		resp, err := http.Get(srv.URL + forgedQuery)
		if err != nil {
			t.Fatalf("GET %s failed: %v", forgedQuery, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400", forgedQuery, resp.StatusCode)
		}
		assertChannelsEmpty(forgedQuery)
	}

	// 5. Genuine OAuth error callback with valid state reports error to errChan.
	resp, err = http.Get(srv.URL + "/?error=access_denied&state=" + expectedState)
	if err != nil {
		t.Fatalf("GET valid error callback failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET valid error callback status = %d, want 400", resp.StatusCode)
	}
	select {
	case gotErr := <-errChan:
		if !strings.Contains(gotErr.Error(), "access_denied") {
			t.Fatalf("expected access_denied in error, got %v", gotErr)
		}
	default:
		t.Fatalf("expected error on errChan for valid error callback")
	}

	// 6. Genuine authorization code callback with valid state succeeds, and duplicate hits do not block.
	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/?code=legit_code_789&state=" + expectedState)
		if err != nil {
			t.Fatalf("GET valid code callback #%d failed: %v", i, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET valid code callback #%d status = %d, want 200", i, resp.StatusCode)
		}
	}
	select {
	case gotCode := <-codeChan:
		if gotCode != "legit_code_789" {
			t.Fatalf("got code %q, want legit_code_789", gotCode)
		}
	default:
		t.Fatalf("expected code on codeChan")
	}
}

func TestParsePastedAuthInputWithState(t *testing.T) {
	const expectedState = "state_abc_123"

	if code, err := parsePastedAuthInputWithState("http://127.0.0.1:7777/?code=mycode&state="+expectedState, expectedState); err != nil || code != "mycode" {
		t.Fatalf("expected matching state URL to succeed, got code=%q err=%v", code, err)
	}

	if _, err := parsePastedAuthInputWithState("http://127.0.0.1:7777/?code=mycode", expectedState); err == nil {
		t.Fatalf("expected missing state in pasted URL to fail")
	}

	if _, err := parsePastedAuthInputWithState("http://127.0.0.1:7777/?code=mycode&state=wrong", expectedState); err == nil {
		t.Fatalf("expected mismatched state in pasted URL to fail")
	}

	if code, err := parsePastedAuthInputWithState("bare_auth_code_xyz", expectedState); err != nil || code != "bare_auth_code_xyz" {
		t.Fatalf("expected bare authorization code fallback to succeed, got code=%q err=%v", code, err)
	}
}

func TestPerformOAuthLoginWithOptions_CallbackTLSAndState(t *testing.T) {
	t.Setenv("PATH", "")

	var receivedVerifier string
	tlsLooker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/token" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedVerifier = payload["code_verifier"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tls_oauth_tok","token_type":"Bearer","expires_in":3600,"refresh_token":"tls_ref_tok"}`))
	}))
	defer tlsLooker.Close()

	u, err := url.Parse(tlsLooker.URL)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}
	host, port := u.Hostname(), u.Port()

	// Helper to run PerformOAuthLoginWithOptions while driving the local HTTP callback listener.
	runFlow := func(verifySSL bool) (authQuery url.Values, tok string, loginErr error) {
		oldStdin := oauthStdin
		rIn, wIn := io.Pipe()
		oauthStdin = rIn
		defer func() {
			_ = wIn.Close()
			oauthStdin = oldStdin
		}()

		oldStderr := os.Stderr
		rErr, wErr, pipeErr := os.Pipe()
		if pipeErr != nil {
			t.Fatalf("os.Pipe failed: %v", pipeErr)
		}
		os.Stderr = wErr

		doneErr := make(chan struct{})
		go func() {
			defer close(doneErr)
			reader := bufio.NewReader(rErr)
			driven := false
			for {
				line, readErr := reader.ReadString('\n')
				if line != "" && !driven && strings.HasPrefix(line, "Opening browser to URL: ") {
					driven = true
					rawURL := strings.TrimSpace(strings.TrimPrefix(line, "Opening browser to URL: "))
					if parsed, pErr := url.Parse(rawURL); pErr == nil {
						authQuery = parsed.Query()
						cbURI := authQuery.Get("redirect_uri")
						state := authQuery.Get("state")

						// Hit /favicon.ico and a forged state callback first; neither may abort login.
						if resp, gErr := http.Get(cbURI + "/favicon.ico"); gErr == nil {
							_ = resp.Body.Close()
						}
						if resp, gErr := http.Get(cbURI + "/?code=forged&state=bad"); gErr == nil {
							_ = resp.Body.Close()
						}
						// Now deliver the legitimate callback with matching state.
						if resp, gErr := http.Get(cbURI + "/?code=valid_cb_code&state=" + url.QueryEscape(state)); gErr == nil {
							_ = resp.Body.Close()
						}
					}
				}
				if readErr != nil {
					return
				}
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tok, _, _, loginErr = PerformOAuthLoginWithOptions(ctx, host, port, "com.looker.cli", OAuthRequestOptions{
			SSL:          true,
			VerifySSL:    verifySSL,
			Timeout:      5 * time.Second,
			callbackAddr: "127.0.0.1:0",
		})

		_ = wErr.Close()
		os.Stderr = oldStderr
		<-doneErr
		return authQuery, tok, loginErr
	}

	// 1. VerifySSL: true must reject the self-signed certificate during code exchange.
	if _, _, err := runFlow(true); err == nil {
		t.Fatalf("expected TLS verification failure when VerifySSL=true against self-signed server")
	}

	// 2. VerifySSL: false must succeed and verify PKCE + state end-to-end.
	authQuery, tok, err := runFlow(false)
	if err != nil {
		t.Fatalf("PerformOAuthLoginWithOptions(VerifySSL=false) failed: %v", err)
	}
	if tok != "tls_oauth_tok" {
		t.Fatalf("got access token %q, want tls_oauth_tok", tok)
	}
	if authQuery.Get("state") == "" {
		t.Fatalf("expected state parameter on authURL")
	}
	sum := sha256.Sum256([]byte(receivedVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if gotChallenge := authQuery.Get("code_challenge"); gotChallenge != wantChallenge {
		t.Fatalf("PKCE challenge mismatch: authURL had %q, verifier hashed to %q", gotChallenge, wantChallenge)
	}
}

func TestRefreshOAuthTokenWithOptions_TLSProxyAndTimeout(t *testing.T) {
	t.Run("VerifySSL honored on refresh", func(t *testing.T) {
		tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"refreshed_tls_tok","token_type":"Bearer","expires_in":3600}`))
		}))
		defer tlsSrv.Close()

		u, _ := url.Parse(tlsSrv.URL)
		ctx := context.Background()

		if _, _, _, err := RefreshOAuthTokenWithOptions(ctx, u.Hostname(), u.Port(), "cid", "rtok", OAuthRequestOptions{
			SSL:       true,
			VerifySSL: true,
			Timeout:   5 * time.Second,
		}); err == nil {
			t.Fatalf("expected TLS error with VerifySSL=true")
		}

		tok, refTok, _, err := RefreshOAuthTokenWithOptions(ctx, u.Hostname(), u.Port(), "cid", "rtok", OAuthRequestOptions{
			SSL:       true,
			VerifySSL: false,
			Timeout:   5 * time.Second,
		})
		if err != nil {
			t.Fatalf("RefreshOAuthTokenWithOptions(VerifySSL=false) failed: %v", err)
		}
		if tok != "refreshed_tls_tok" || refTok != "rtok" {
			t.Fatalf("unexpected tokens: tok=%q refTok=%q", tok, refTok)
		}
	})

	t.Run("HTTPProxy honored on refresh", func(t *testing.T) {
		var proxyHits atomic.Int32
		proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"via_proxy_tok","token_type":"Bearer","expires_in":3600,"refresh_token":"new_rtok"}`))
		}))
		defer proxySrv.Close()

		tok, refTok, _, err := RefreshOAuthTokenWithOptions(context.Background(), "looker.internal.example", "19999", "cid", "rtok", OAuthRequestOptions{
			SSL:       false,
			VerifySSL: false,
			Timeout:   5 * time.Second,
			HTTPProxy: proxySrv.URL,
		})
		if err != nil {
			t.Fatalf("RefreshOAuthTokenWithOptions via proxy failed: %v", err)
		}
		if proxyHits.Load() != 1 || tok != "via_proxy_tok" || refTok != "new_rtok" {
			t.Fatalf("expected 1 proxy hit and via_proxy_tok, got hits=%d tok=%q refTok=%q", proxyHits.Load(), tok, refTok)
		}
	})

	t.Run("Timeout honored on refresh", func(t *testing.T) {
		slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}))
		defer slowSrv.Close()

		u, _ := url.Parse(slowSrv.URL)
		start := time.Now()
		_, _, _, err := RefreshOAuthTokenWithOptions(context.Background(), u.Hostname(), u.Port(), "cid", "rtok", OAuthRequestOptions{
			SSL:     false,
			Timeout: 80 * time.Millisecond,
		})
		if err == nil {
			t.Fatalf("expected timeout error from slow server")
		}
		if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
			t.Fatalf("expected timeout around 80ms, took %v", elapsed)
		}
	})
}

func TestNewClientWithOptions_TimeoutProxyAndConcurrentFallback(t *testing.T) {
	t.Run("invalid HTTPProxy URLs rejected upfront", func(t *testing.T) {
		for _, badProxy := range []string{
			"not-a-url",
			"proxy.example.com:8080",
			"http://",
			"ftp://proxy.example.com:21",
			"://missing-scheme",
		} {
			_, err := NewClientWithOptions(context.Background(), ClientOptions{
				Host:      "example.looker.com",
				Port:      "19999",
				Token:     "tok",
				SSL:       true,
				VerifySSL: true,
				HTTPProxy: badProxy,
			})
			if err == nil {
				t.Errorf("expected error for invalid HTTPProxy %q, got nil", badProxy)
			}
		}
	})

	t.Run("HTTPProxy routes SDK requests and default transport preserves ProxyFromEnvironment", func(t *testing.T) {
		defaultTr, err := buildHTTPTransport(true, "")
		if err != nil {
			t.Fatalf("buildHTTPTransport failed: %v", err)
		}
		if defaultTr.Proxy == nil {
			t.Fatalf("expected buildHTTPTransport with empty httpProxy to preserve http.ProxyFromEnvironment")
		}

		var proxyHits atomic.Int32
		proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"42"}`))
		}))
		defer proxySrv.Close()

		wrapper, err := NewClientWithOptions(context.Background(), ClientOptions{
			Host:      "looker.internal.example",
			Port:      "19999",
			Token:     "tok",
			SSL:       false,
			HTTPProxy: proxySrv.URL,
			Timeout:   5,
		})
		if err != nil {
			t.Fatalf("NewClientWithOptions failed: %v", err)
		}
		if _, err := wrapper.SDK.Me("", nil); err != nil {
			t.Fatalf("SDK.Me via explicit HTTPProxy failed: %v", err)
		}
		if proxyHits.Load() != 1 {
			t.Fatalf("expected 1 proxy hit via HTTPProxy, got %d", proxyHits.Load())
		}
	})

	t.Run("Timeout wired to both Session.Config and Session.Client", func(t *testing.T) {
		slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		}))
		defer slowSrv.Close()

		u, _ := url.Parse(slowSrv.URL)
		wrapper, err := NewClientWithOptions(context.Background(), ClientOptions{
			Host:    u.Hostname(),
			Port:    u.Port(),
			Token:   "tok",
			SSL:     false,
			Timeout: 1,
		})
		if err != nil {
			t.Fatalf("NewClientWithOptions failed: %v", err)
		}
		if wrapper.Session.Config.Timeout != 1 {
			t.Fatalf("Session.Config.Timeout = %d, want 1", wrapper.Session.Config.Timeout)
		}
		if wrapper.Session.Client.Timeout != 1*time.Second {
			t.Fatalf("Session.Client.Timeout = %v, want 1s", wrapper.Session.Client.Timeout)
		}

		start := time.Now()
		req, _ := http.NewRequest(http.MethodGet, slowSrv.URL+"/api/4.0/user", bytes.NewReader(nil))
		if _, err := wrapper.Session.Client.Do(req); err == nil {
			t.Fatalf("expected Session.Client.Do to time out")
		}
		if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
			t.Fatalf("expected Session.Client.Do timeout near 1s, took %v", elapsed)
		}
	})

	t.Run("concurrent 401 token cleanup and fallback is race-free", func(t *testing.T) {
		isolateAuthState(t)
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		origOverride := config.ConfigPathOverride
		config.ConfigPathOverride = filepath.Join(tmpDir, "config.yaml")
		t.Cleanup(func() { config.ConfigPathOverride = origOverride })

		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/4.0/login" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"fresh_fallback_tok","token_type":"Bearer","expires_in":3600}`))
				return
			}
			if r.Header.Get("Authorization") == "Bearer fresh_fallback_tok" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"99"}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Requires authentication."}`))
		}))
		defer mockSrv.Close()

		u, _ := url.Parse(mockSrv.URL)
		exp := time.Now().Add(1 * time.Hour)
		cfg := &config.Config{
			Default: "p1",
			Profiles: map[string]config.Profile{
				"p1": {
					Host:         u.Hostname(),
					Port:         u.Port(),
					ClientID:     "cid",
					ClientSecret: "csec",
					AccessToken:  "stale_stored_tok",
					Expiration:   exp.Format(TimeFormat),
				},
			},
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("cfg.Save failed: %v", err)
		}

		wrapper, err := NewClientWithOptions(context.Background(), ClientOptions{
			Host:          u.Hostname(),
			Port:          u.Port(),
			ClientID:      "cid",
			ClientSecret:  "csec",
			SSL:           false,
			ActiveProfile: "p1",
			Timeout:       5,
		})
		if err != nil {
			t.Fatalf("NewClientWithOptions failed: %v", err)
		}

		var wg sync.WaitGroup
		var successCount atomic.Int32
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, callErr := wrapper.SDK.Me("", nil); callErr == nil {
					successCount.Add(1)
				}
			}()
		}
		wg.Wait()

		// After fallback switches the TokenSource, a subsequent call must definitely succeed.
		if _, err := wrapper.SDK.Me("", nil); err != nil {
			t.Fatalf("expected post-fallback SDK.Me to succeed, got: %v", err)
		}
		if successCount.Load() == 0 {
			t.Fatalf("expected at least the retrying goroutine to succeed on 401 fallback")
		}
	})
}
