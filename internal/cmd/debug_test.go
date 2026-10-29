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

package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		mustHave    []string
		mustNotHave []string
	}{
		{
			name:        "login secret in query",
			in:          "https://looker.example.com:19999/api/4.0/login?client_id=abc&client_secret=s3cr3t",
			mustHave:    []string{"client_id=abc", "client_secret=%5BREDACTED%5D", "/api/4.0/login"},
			mustNotHave: []string{"s3cr3t"},
		},
		{
			name:        "token and password params",
			in:          "https://h/api/4.0/x?access_token=t0k&password=pw&fields=id",
			mustHave:    []string{"fields=id"},
			mustNotHave: []string{"t0k", "pw&", "=pw"},
		},
		{
			name:        "userinfo password",
			in:          "https://user:hunter2@h/api/4.0/user",
			mustHave:    []string{"user:"},
			mustNotHave: []string{"hunter2"},
		},
		{
			name:     "nothing sensitive is unchanged",
			in:       "https://h/api/4.0/user?fields=id",
			mustHave: []string{"https://h/api/4.0/user?fields=id"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactURL(tt.in)
			for _, s := range tt.mustHave {
				if !strings.Contains(got, s) {
					t.Errorf("redactURL(%q) = %q, want it to contain %q", tt.in, got, s)
				}
			}
			for _, s := range tt.mustNotHave {
				if strings.Contains(got, s) {
					t.Errorf("redactURL(%q) = %q, must not contain %q", tt.in, got, s)
				}
			}
		})
	}
}

func TestLogDebugRequest_RedactsSensitiveHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer super_secret_token")
	h.Set("Cookie", "looker.browser=abc")
	h.Set("X-Api-Key", "custom_key_value")
	h.Set("X-Goog-Iap-Jwt-Assertion-Token", "iap_jwt")
	h.Set("User-Agent", "looker-cli test")
	h.Set("Content-Type", "application/json")

	var buf bytes.Buffer
	logDebugRequest(&buf, "POST", "https://h/api/4.0/login?client_secret=cs", h, []byte(`{"a":1}`))
	out := buf.String()

	for _, secret := range []string{"super_secret_token", "looker.browser=abc", "custom_key_value", "iap_jwt", "client_secret=cs"} {
		if strings.Contains(out, secret) {
			t.Errorf("debug output leaked %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{"Header Authorization: [[REDACTED]]", "Header User-Agent: [looker-cli test]", "Header Content-Type: [application/json]", `Request Body: {"a":1}`, "--> POST "} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output missing %q:\n%s", want, out)
		}
	}
	// Header order must be deterministic.
	if strings.Index(out, "Header Authorization") > strings.Index(out, "Header User-Agent") {
		t.Errorf("expected headers sorted by name:\n%s", out)
	}
}

func resetRootFlags(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		f := RootCmd.PersistentFlags().Lookup(name)
		if f == nil {
			t.Fatalf("unknown flag %q", name)
		}
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatalf("failed to reset flag %q: %v", name, err)
		}
		f.Changed = false
	}
}

// Regression test: `--debug` on generated api commands printed the bearer
// token (and other credentials) to stdout.
func TestApiCommandDebug_DoesNotLeakCredentials(t *testing.T) {
	tmpDir := setupTempConfig(t)
	defer teardownTempConfig(t, tmpDir)
	t.Setenv("HOME", tmpDir)
	t.Setenv("LOOKERSDK_BASE_URL", "")
	t.Setenv("LOOKERSDK_CLIENT_ID", "")
	t.Setenv("LOOKERSDK_CLIENT_SECRET", "")

	const token = "very_secret_bearer_token"
	const headerSecret = "custom_header_secret"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/4.0/user" {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","display_name":"Test User"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse server url: %v", err)
	}

	MockSDK = nil
	var stderr bytes.Buffer
	RootCmd.SetErr(&stderr)
	defer func() {
		RootCmd.SetErr(nil)
		resetRootFlags(t, "host", "port", "ssl", "token", "debug")
		cfgHeaders = nil
		RootCmd.PersistentFlags().Lookup("header").Changed = false
	}()

	stdout, err := executeCommand("api", "user", "me",
		"--host", u.Hostname(), "--port", u.Port(), "--ssl=false",
		"--token", token, "--debug",
		"-H", "X-Api-Key: "+headerSecret)
	if err != nil {
		t.Fatalf("api user me failed: %v\nstderr: %s", err, stderr.String())
	}

	if gotAuth != "Bearer "+token {
		t.Fatalf("expected the request to carry the bearer token, got %q", gotAuth)
	}
	for name, out := range map[string]string{"stdout": stdout, "stderr": stderr.String()} {
		if strings.Contains(out, token) {
			t.Errorf("%s leaked the bearer token:\n%s", name, out)
		}
		if strings.Contains(out, headerSecret) {
			t.Errorf("%s leaked the custom secret header:\n%s", name, out)
		}
	}
	if strings.Contains(stdout, "-->") || strings.Contains(stdout, "<--") {
		t.Errorf("debug output must go to stderr, not stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr.String(), "Header Authorization: [[REDACTED]]") {
		t.Errorf("expected redacted Authorization header in stderr debug output:\n%s", stderr.String())
	}
	if !strings.Contains(stdout, `"display_name": "Test User"`) {
		t.Errorf("expected API response on stdout, got:\n%s", stdout)
	}
}
