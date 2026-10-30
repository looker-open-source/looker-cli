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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const redacted = "[REDACTED]"

// sensitiveNameFragments marks header and query parameter names whose values
// must never appear in debug output. Matching is case-insensitive substring.
var sensitiveNameFragments = []string{
	"authorization",
	"cookie",
	"token",
	"secret",
	"password",
	"api-key",
	"apikey",
}

func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, frag := range sensitiveNameFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// redactURL returns rawURL with sensitive query parameter values and any
// userinfo password replaced. If parsing fails, the query string is dropped.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		if i := strings.IndexByte(rawURL, '?'); i >= 0 {
			return rawURL[:i] + "?" + redacted
		}
		return rawURL
	}
	if u.User != nil {
		if _, hasPwd := u.User.Password(); hasPwd {
			u.User = url.UserPassword(u.User.Username(), redacted)
		}
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if isSensitiveName(k) {
				q[k] = []string{redacted}
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// logDebugRequest writes a redacted description of an outgoing API request.
// Callers should pass stderr so debug output never mixes with command output.
func logDebugRequest(w io.Writer, method, rawURL string, header http.Header, body []byte) {
	_, _ = fmt.Fprintf(w, "--> %s %s\n", method, redactURL(rawURL))
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := header[k]
		if isSensitiveName(k) {
			v = []string{redacted}
		}
		_, _ = fmt.Fprintf(w, "Header %s: %s\n", k, v)
	}
	if len(body) > 0 {
		_, _ = fmt.Fprintf(w, "Request Body: %s\n", string(body))
	}
}

// logDebugResponse writes the status and body of an API response.
func logDebugResponse(w io.Writer, status string, body []byte) {
	_, _ = fmt.Fprintf(w, "<-- %s\n", status)
	_, _ = fmt.Fprintf(w, "Response Body: %s\n", string(body))
}
