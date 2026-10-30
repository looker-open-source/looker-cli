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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/looker-open-source/sdk-codegen/go/rtl"
	v4 "github.com/looker-open-source/sdk-codegen/go/sdk/v4"
)

func TestBuildProjectAPIURL_SingleEscapesPathSegmentAndPreservesBasePath(t *testing.T) {
	q := url.Values{}
	q.Set("file_path", "views/users.view.lkml")

	got, err := buildProjectAPIURL("https://looker.example.com:19999/subpath/", "org/my project", "/file/content", q)
	if err != nil {
		t.Fatalf("buildProjectAPIURL failed: %v", err)
	}
	want := "https://looker.example.com:19999/subpath/api/4.0/projects/org%2Fmy%20project/file/content?file_path=views%2Fusers.view.lkml"
	if got != want {
		t.Fatalf("buildProjectAPIURL = %q, want %q", got, want)
	}
}

func TestProjectFileAndDirectoryCommands_WireURLAndCustomHeaders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpCfg := setupTempConfig(t)
	defer teardownTempConfig(t, tmpCfg)

	type seenReq struct {
		method      string
		escapedPath string
		rawQuery    string
		authHeader  string
		customHdr   string
	}
	var recorded []seenReq

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded = append(recorded, seenReq{
			method:      r.Method,
			escapedPath: r.URL.EscapedPath(),
			rawQuery:    r.URL.RawQuery,
			authHeader:  r.Header.Get("Authorization"),
			customHdr:   r.Header.Get("X-Custom-Test"),
		})
		if strings.HasSuffix(r.URL.Path, "/directories") && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`["views","models"]`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/file/content") && r.Method == http.MethodGet {
			_, _ = w.Write([]byte("view: users {}"))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	srvURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	t.Cleanup(func() {
		cfgHost = "localhost"
		cfgPort = "19999"
		cfgSSL = true
		cfgToken = ""
		cfgHeaders = nil
		for _, name := range []string{"host", "port", "ssl", "token", "header"} {
			if f := RootCmd.PersistentFlags().Lookup(name); f != nil {
				f.Changed = false
			}
		}
		projectDirLsPlain, projectDirLsCSV = false, false
	})

	lkmlFile := filepath.Join(t.TempDir(), "users.view.lkml")
	if err := os.WriteFile(lkmlFile, []byte("view: users {}"), 0o600); err != nil {
		t.Fatalf("write lkml file: %v", err)
	}

	commonFlags := []string{
		"--host", srvURL.Hostname(),
		"--port", srvURL.Port(),
		"--ssl=false",
		"--token", "tok123",
		"-H", "X-Custom-Test: present",
	}

	const projID = "org/my project"
	cmds := [][]string{
		{"project", "file", "cat", projID, "views/users.view.lkml"},
		{"project", "file", "create", projID, "views/users.view.lkml", lkmlFile},
		{"project", "file", "update", projID, "views/users.view.lkml", lkmlFile},
		{"project", "file", "rm", projID, "views/users.view.lkml"},
		{"project", "directory", "ls", projID, "--plain"},
		{"project", "directory", "create", projID, "views/nested"},
		{"project", "directory", "rm", projID, "views/nested"},
	}

	for _, c := range cmds {
		args := append(append([]string{}, c...), commonFlags...)
		if _, err := executeCommand(args...); err != nil {
			t.Fatalf("command %v failed: %v", c, err)
		}
	}

	if len(recorded) != len(cmds) {
		t.Fatalf("recorded %d requests, want %d", len(recorded), len(cmds))
	}

	for i, rec := range recorded {
		if strings.Contains(rec.escapedPath, "%25") {
			t.Errorf("request %d (%s) double-escaped path: %q", i, rec.method, rec.escapedPath)
		}
		if !strings.HasPrefix(rec.escapedPath, "/api/4.0/projects/org%2Fmy%20project/") {
			t.Errorf("request %d (%s) unexpected escaped path: %q", i, rec.method, rec.escapedPath)
		}
		if rec.customHdr != "present" {
			t.Errorf("request %d (%s %s) missing custom header X-Custom-Test, got %q", i, rec.method, rec.escapedPath, rec.customHdr)
		}
		if rec.authHeader == "" {
			t.Errorf("request %d (%s %s) missing Authorization header", i, rec.method, rec.escapedPath)
		}
	}
}

type resourceMockDoer struct {
	t        *testing.T
	attrs    []v4.UserAttribute
	listErr  error
	calls    []string
	lastBody []byte
}

func (m *resourceMockDoer) Do(result interface{}, method, ver, path string, reqPars map[string]interface{}, body interface{}, options *rtl.ApiSettings) error {
	call := method + " " + path
	m.calls = append(m.calls, call)
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		m.lastBody = b
	}

	respondJSON := func(v interface{}) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, result)
	}

	switch {
	case method == "GET" && path == "/user_attributes":
		if m.listErr != nil {
			return m.listErr
		}
		return respondJSON(m.attrs)

	case method == "POST" && path == "/user_attributes":
		var wua v4.WriteUserAttribute
		_ = json.Unmarshal(m.lastBody, &wua)
		return respondJSON(v4.UserAttribute{
			Id:    ptr("new_attr_1"),
			Name:  wua.Name,
			Label: wua.Label,
		})

	case method == "PATCH" && strings.HasPrefix(path, "/user_attributes/"):
		id := strings.TrimPrefix(path, "/user_attributes/")
		var wua v4.WriteUserAttribute
		_ = json.Unmarshal(m.lastBody, &wua)
		return respondJSON(v4.UserAttribute{
			Id:    &id,
			Name:  wua.Name,
			Label: wua.Label,
		})

	case method == "PATCH" && path == "/groups/10/attribute_values/55":
		// Looker omits Value (returns null) when value_is_hidden is true.
		return respondJSON(v4.UserAttributeGroupValue{
			Id:            nil,
			Value:         nil,
			ValueIsHidden: ptrBool(true),
		})

	case method == "GET" && path == "/users/42":
		if f, ok := reqPars["fields"].(*string); ok && f != nil && *f == "first_name,last_name" {
			return respondJSON(v4.User{
				FirstName: ptr("Ada"),
				LastName:  ptr("Lovelace"),
			})
		}
		return respondJSON(v4.User{
			Id:        ptr("42"),
			FirstName: ptr("Ada"),
			LastName:  ptr("Lovelace"),
		})

	case method == "GET" && path == "/roles/7/groups":
		return respondJSON([]v4.Group{{Id: ptr("100")}})

	case method == "PUT" && path == "/roles/7/groups":
		return respondJSON([]v4.Group{})

	case method == "GET" && path == "/roles/7/users":
		return respondJSON([]v4.User{{Id: ptr("200")}})

	case method == "PUT" && path == "/roles/7/users":
		return respondJSON([]v4.User{})
	}

	m.t.Errorf("resourceMockDoer: unhandled call %s", call)
	return fmt.Errorf("unhandled call %s", call)
}

func useResourceMock(t *testing.T, m *resourceMockDoer) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	MockSDK = v4.NewLookerSDK(m)
	resetFlags := func() {
		attributeCreateForce, attributeCreatePlain = false, false
		attributeImportForce, attributeImportPlain = false, false
		userCatDir, userCatFields, userCatTrim = "", "", false
	}
	resetFlags()
	t.Cleanup(func() {
		MockSDK = nil
		resetFlags()
	})
}

func TestAttributeSetGroupValue_HiddenValueDoesNotPanic(t *testing.T) {
	m := &resourceMockDoer{
		t:     t,
		attrs: []v4.UserAttribute{{Id: ptr("55"), Name: "secret_attr", Label: "Secret"}},
	}
	useResourceMock(t, m)

	out, err := executeCommand("attribute", "set", "group", "value", "10", "secret_attr", "top-secret")
	if err != nil {
		t.Fatalf("attribute set group value failed: %v", err)
	}
	if !strings.Contains(out, "Group attribute 55 set to <hidden>") {
		t.Errorf("expected hidden value placeholder, got %q", out)
	}
}

func TestAttributeCreateAndImport_MatchesByNameOnlyAndChecksListError(t *testing.T) {
	t.Run("create --force does not overwrite another attribute with matching label", func(t *testing.T) {
		m := &resourceMockDoer{
			t: t,
			attrs: []v4.UserAttribute{
				{Id: ptr("99"), Name: "legacy_dept_attr", Label: "department"},
			},
		}
		useResourceMock(t, m)

		// Default label when omitted is name ("department"), which matches legacy_dept_attr.Label.
		out, err := executeCommand("attribute", "create", "department", "--force")
		if err != nil {
			t.Fatalf("attribute create failed: %v", err)
		}
		for _, c := range m.calls {
			if strings.HasPrefix(c, "PATCH /user_attributes/") {
				t.Fatalf("attribute create --force overwrote unrelated attribute via %s", c)
			}
		}
		if !strings.Contains(out, "Attribute new_attr_1 (department) created/modified.") {
			t.Errorf("unexpected output: %q", out)
		}
	})

	t.Run("import --force with omitted label does not match attribute with empty label", func(t *testing.T) {
		m := &resourceMockDoer{
			t: t,
			attrs: []v4.UserAttribute{
				{Id: ptr("99"), Name: "other_attr", Label: ""},
			},
		}
		useResourceMock(t, m)

		attrFile := filepath.Join(t.TempDir(), "attr.json")
		if err := os.WriteFile(attrFile, []byte(`{"name":"imported_attr","type":"string"}`), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := executeCommand("attribute", "import", attrFile, "--force"); err != nil {
			t.Fatalf("attribute import failed: %v", err)
		}
		for _, c := range m.calls {
			if strings.HasPrefix(c, "PATCH /user_attributes/") {
				t.Fatalf("attribute import --force overwrote unrelated attribute via %s", c)
			}
		}
	})

	t.Run("import rejects file missing name", func(t *testing.T) {
		m := &resourceMockDoer{t: t}
		useResourceMock(t, m)

		attrFile := filepath.Join(t.TempDir(), "bad_attr.json")
		if err := os.WriteFile(attrFile, []byte(`{"label":"Only Label"}`), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := executeCommand("attribute", "import", attrFile, "--force")
		if err == nil || !strings.Contains(err.Error(), "missing name") {
			t.Fatalf("expected missing name error, got %v", err)
		}
		if len(m.calls) != 0 {
			t.Errorf("expected no API calls on invalid input, got %v", m.calls)
		}
	})

	t.Run("create and import surface AllUserAttributes failure", func(t *testing.T) {
		m := &resourceMockDoer{
			t:       t,
			listErr: fmt.Errorf("response error. status=500 Internal Server Error"),
		}
		useResourceMock(t, m)

		if _, err := executeCommand("attribute", "create", "dept", "--force"); err == nil {
			t.Fatal("expected attribute create to fail when listing user attributes fails")
		}

		attrFile := filepath.Join(t.TempDir(), "attr.json")
		if err := os.WriteFile(attrFile, []byte(`{"name":"dept","label":"Dept","type":"string"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := executeCommand("attribute", "import", attrFile, "--force"); err == nil {
			t.Fatal("expected attribute import to fail when listing user attributes fails")
		}
	})
}

func TestUserCatDir_UsesStringUserID(t *testing.T) {
	m := &resourceMockDoer{t: t}
	useResourceMock(t, m)

	outDir := t.TempDir()
	if _, err := executeCommand("user", "cat", "42", "--dir", outDir); err != nil {
		t.Fatalf("user cat --dir failed: %v", err)
	}

	wantFile := filepath.Join(outDir, "User_42_Ada_Lovelace.json")
	if _, err := os.Stat(wantFile); err != nil {
		entries, _ := os.ReadDir(outDir)
		t.Fatalf("expected %s to exist, found %v (err: %v)", wantFile, entries, err)
	}

	// Also verify fallback to CLI userID when --fields omits "id".
	outDirNoID := t.TempDir()
	if _, err := executeCommand("user", "cat", "42", "--fields", "first_name,last_name", "--dir", outDirNoID); err != nil {
		t.Fatalf("user cat --fields --dir failed: %v", err)
	}
	wantFallback := filepath.Join(outDirNoID, "User_42_Ada_Lovelace.json")
	if _, err := os.Stat(wantFallback); err != nil {
		entries, _ := os.ReadDir(outDirNoID)
		t.Fatalf("expected %s to exist when id omitted from --fields, found %v (err: %v)", wantFallback, entries, err)
	}
}

func TestRoleGroupAndUserRm_RemovingLastMemberSendsEmptyJSONArray(t *testing.T) {
	t.Run("role group rm sends [] not null", func(t *testing.T) {
		m := &resourceMockDoer{t: t}
		useResourceMock(t, m)

		if _, err := executeCommand("role", "group", "rm", "7", "100"); err != nil {
			t.Fatalf("role group rm failed: %v", err)
		}
		if string(m.lastBody) != "[]" {
			t.Fatalf("SetRoleGroups body = %s, want []", string(m.lastBody))
		}
	})

	t.Run("role user rm sends [] not null", func(t *testing.T) {
		m := &resourceMockDoer{t: t}
		useResourceMock(t, m)

		if _, err := executeCommand("role", "user", "rm", "7", "200"); err != nil {
			t.Fatalf("role user rm failed: %v", err)
		}
		if string(m.lastBody) != "[]" {
			t.Fatalf("SetRoleUsers body = %s, want []", string(m.lastBody))
		}
	})
}
