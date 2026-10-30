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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/looker-open-source/looker-cli/internal/config"
)

const (
	testSuUser      = "bob"
	testAdminToken  = "admin_tok"
	testSuToken     = "su_tok_from_login_user"
	testOAuthToken  = "oauth_admin_tok"
	testOAuthRefTok = "oauth_admin_refresh"
)

// suMockServer is a fake Looker API that issues OAuth tokens and login_user
// tokens, and records which bearer token each login_user call was made with.
type suMockServer struct {
	*httptest.Server
	mu            sync.Mutex
	loginUserAuth []string
}

func newSuMockServer(t *testing.T) *suMockServer {
	t.Helper()
	m := &suMockServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/token":
			_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":%q}`, testOAuthToken, testOAuthRefTok)
		case "/api/4.0/login/" + testSuUser:
			m.mu.Lock()
			m.loginUserAuth = append(m.loginUserAuth, r.Header.Get("Authorization"))
			m.mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, testSuToken)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *suMockServer) loginUserCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.loginUserAuth...)
}

func (m *suMockServer) hostPort(t *testing.T) (string, string) {
	t.Helper()
	u, err := url.Parse(m.URL)
	if err != nil {
		t.Fatalf("failed to parse mock server url: %v", err)
	}
	return u.Hostname(), u.Port()
}

// isolateAuthState points HOME and the config file at a fresh temp dir so
// tests never touch the developer's real ~/.looker_auth or config.yaml.
func isolateAuthState(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("LOOKERSDK_CLIENT_ID", "")
	t.Setenv("LOOKERSDK_CLIENT_SECRET", "")
	orig := config.ConfigPathOverride
	config.ConfigPathOverride = filepath.Join(tmpDir, "config.yaml")
	t.Cleanup(func() { config.ConfigPathOverride = orig })
}

func assertImpersonationEntry(t *testing.T, host string) {
	t.Helper()
	entry, err := GetTokenEntry(host, testSuUser)
	if err != nil {
		t.Fatalf("expected su token entry for %s: %v", testSuUser, err)
	}
	if entry.Token != testSuToken || !entry.Impersonated {
		t.Errorf("expected impersonation entry with token %q, got %+v", testSuToken, entry)
	}
	if entry.RefreshToken != "" {
		t.Errorf("impersonation entry must not carry a refresh token, got %q", entry.RefreshToken)
	}
}

func TestStoreImpersonationToken_RequiresUser(t *testing.T) {
	isolateAuthState(t)
	if err := StoreImpersonationToken("test.looker.com", "", "tok", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("expected error when storing an impersonation token without a target user")
	}
}

// Regression test: an older build (or `session login --su`) stored the caller's
// own token under the su user's key. NewClient compared that entry with the
// active token, found a match, and skipped login_user, so commands ran as the
// caller instead of the su user.
func TestNewClient_SuNotSkippedWhenCallerTokenStoredUnderSuKey(t *testing.T) {
	isolateAuthState(t)
	m := newSuMockServer(t)
	host, port := m.hostPort(t)

	if err := StoreToken(host, testSuUser, testAdminToken, "", "", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	wrapper, err := NewClient(context.Background(), host, port, "", "", testAdminToken, testSuUser, false, true, false, false, "")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	calls := m.loginUserCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 login_user call, got %d", len(calls))
	}
	if calls[0] != "Bearer "+testAdminToken {
		t.Errorf("expected login_user to be authorized by the caller token, got %q", calls[0])
	}
	if got := wrapper.Session.Config.Headers["Authorization"]; got != "Bearer "+testSuToken {
		t.Errorf("expected session to use su token, got %q", got)
	}
	assertImpersonationEntry(t, host)
}

func TestNewClient_TokenFileSu_LegacyNonImpersonatedEntry(t *testing.T) {
	future := time.Now().Add(time.Hour)

	t.Run("without default token it fails closed and removes the entry", func(t *testing.T) {
		isolateAuthState(t)
		m := newSuMockServer(t)
		host, port := m.hostPort(t)

		// What `--oauth --su bob` used to write: the caller's OAuth token and
		// refresh token stored under bob's key.
		if err := StoreToken(host, testSuUser, testOAuthToken, testOAuthRefTok, "cid", future); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}

		if _, err := NewClient(context.Background(), host, port, "", "", "", testSuUser, false, true, false, true, ""); err == nil {
			t.Fatal("expected NewClient to fail rather than run as the caller")
		}
		if n := len(m.loginUserCalls()); n != 0 {
			t.Errorf("expected no login_user calls, got %d", n)
		}
		if _, err := GetTokenEntry(host, testSuUser); err == nil {
			t.Error("expected the non-impersonation entry under the su key to be deleted")
		}
	})

	t.Run("with default token it performs a real su", func(t *testing.T) {
		isolateAuthState(t)
		m := newSuMockServer(t)
		host, port := m.hostPort(t)

		if err := StoreToken(host, "", testAdminToken, "", "", future); err != nil {
			t.Fatalf("StoreToken default failed: %v", err)
		}
		if err := StoreToken(host, testSuUser, testAdminToken, "", "", future); err != nil {
			t.Fatalf("StoreToken su failed: %v", err)
		}

		wrapper, err := NewClient(context.Background(), host, port, "", "", "", testSuUser, false, true, false, true, "")
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		calls := m.loginUserCalls()
		if len(calls) != 1 || calls[0] != "Bearer "+testAdminToken {
			t.Fatalf("expected one login_user call authorized by the default token, got %v", calls)
		}
		if got := wrapper.Session.Config.Headers["Authorization"]; got != "Bearer "+testSuToken {
			t.Errorf("expected session to use su token, got %q", got)
		}
		assertImpersonationEntry(t, host)
	})
}

func TestNewClient_TokenFileSu_ReusesImpersonationToken(t *testing.T) {
	isolateAuthState(t)
	m := newSuMockServer(t)
	host, port := m.hostPort(t)

	const cached = "cached_su_tok"
	if err := StoreImpersonationToken(host, testSuUser, cached, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("StoreImpersonationToken failed: %v", err)
	}

	wrapper, err := NewClient(context.Background(), host, port, "", "", "", testSuUser, false, true, false, true, "")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if n := len(m.loginUserCalls()); n != 0 {
		t.Errorf("expected cached impersonation token to be reused without login_user, got %d calls", n)
	}
	if got := wrapper.Session.Config.Headers["Authorization"]; got != "Bearer "+cached {
		t.Errorf("expected session to use cached su token, got %q", got)
	}
}

// Regression test for the reported bug: `--oauth --su bob` must store the
// OAuth token as the caller's (default slot) and still su to bob.
func TestNewClient_OAuthWithSu_PerformsSu(t *testing.T) {
	isolateAuthState(t)
	// Make browser.OpenURL fail fast so the stdin fallback is used and no
	// real browser is launched.
	t.Setenv("PATH", "")
	m := newSuMockServer(t)
	host, port := m.hostPort(t)

	oldStdin := oauthStdin
	t.Cleanup(func() { oauthStdin = oldStdin })
	pr, pw := io.Pipe()
	oauthStdin = pr
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = fmt.Fprintln(pw, "test_auth_code")
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wrapper, err := NewClient(ctx, host, port, "cid", "", "", testSuUser, false, true, true, false, "")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	calls := m.loginUserCalls()
	if len(calls) != 1 || calls[0] != "Bearer "+testOAuthToken {
		t.Fatalf("expected one login_user call authorized by the OAuth token, got %v", calls)
	}
	if got := wrapper.Session.Config.Headers["Authorization"]; got != "Bearer "+testSuToken {
		t.Errorf("expected session to use su token, got %q", got)
	}

	def, err := GetTokenEntry(host, "")
	if err != nil {
		t.Fatalf("expected OAuth token in default slot: %v", err)
	}
	if def.Token != testOAuthToken || def.RefreshToken != testOAuthRefTok || def.Impersonated {
		t.Errorf("unexpected default entry: %+v", def)
	}
	assertImpersonationEntry(t, host)
}
