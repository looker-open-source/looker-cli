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

package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type SwaggerSpec struct {
	BasePath    string                           `json:"basePath"`
	Tags        []Tag                            `json:"tags"`
	Paths       map[string]map[string]*Operation `json:"paths"`
	Definitions map[string]interface{}           `json:"definitions"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Operation struct {
	Tags        []string     `json:"tags"`
	OperationID string       `json:"operationId"`
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Parameters  []*Parameter `json:"parameters"`
	Deprecated  bool         `json:"deprecated"`
}

type Parameter struct {
	Name        string   `json:"name"`
	In          string   `json:"in"` // "path", "query", "body", "header"
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Type        string   `json:"type"` // "string", "integer", "boolean", "array"
	Format      string   `json:"format"`
	Schema      *Schema  `json:"schema"`
}

type Schema struct {
	Ref string `json:"$ref"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run tool/generate_api.go <looker_host> [port]")
		fmt.Println("Example: go run tool/generate_api.go sandbox.looker-devrel.com")
		os.Exit(1)
	}

	host := os.Args[1]
	port := "443"
	if len(os.Args) > 2 {
		port = os.Args[2]
	}

	targetURL := host
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + host
		if !strings.Contains(host, ":") {
			targetURL = fmt.Sprintf("https://%s:%s", host, port)
		}
	}
	if !strings.HasSuffix(targetURL, "/api/4.0/swagger.json") {
		targetURL = strings.TrimSuffix(targetURL, "/")
		targetURL = targetURL + "/api/4.0/swagger.json"
	}

	fmt.Printf("Fetching swagger.json from %s ...\n", targetURL)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(targetURL)
	if err != nil {
		fmt.Printf("Failed to fetch swagger.json: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Server returned status %s\n", resp.Status)
		os.Exit(1)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read response body: %v\n", err)
		os.Exit(1)
	}

	outputFile := "internal/cmd/api_generated.go"

	var spec SwaggerSpec
	if err := json.Unmarshal(b, &spec); err != nil {
		fmt.Printf("Failed to parse swagger JSON: %v\n", err)
		os.Exit(1)
	}

	// Group operations by tag
	tagOperations := make(map[string][]*OpMetadata)
	tagInfo := make(map[string]string)

	for _, t := range spec.Tags {
		tagInfo[t.Name] = t.Description
	}

	for path, pathItem := range spec.Paths {
		for method, op := range pathItem {
			if op.OperationID == "" {
				continue
			}
			tag := "Default"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}

			// Parse parameters
			var pathParams []string
			var requiredQueryParams []string
			var bodyParam string
			queryFlags := make(map[string]*Parameter)

			// Find path params in order of path appearance
			re := regexp.MustCompile(`\{([^}]+)\}`)
			matches := re.FindAllStringSubmatch(path, -1)
			for _, m := range matches {
				pathParams = append(pathParams, m[1])
			}

			var bodyParamPtr *Parameter
			for _, p := range op.Parameters {
				if p.In == "path" {
					// Already captured in order by regex, but verify it exists
					continue
				} else if p.In == "body" {
					bodyParam = p.Name
					bodyParamPtr = p
				} else if p.In == "query" {
					if p.Required {
						requiredQueryParams = append(requiredQueryParams, p.Name)
					} else {
						queryFlags[p.Name] = p
					}
				}
			}

			var bodySchemaJSON string
			var bodyTemplateJSON string
			if bodyParamPtr != nil && bodyParamPtr.Schema != nil && bodyParamPtr.Schema.Ref != "" {
				ref := bodyParamPtr.Schema.Ref
				defName := strings.TrimPrefix(ref, "#/definitions/")
				if def, ok := spec.Definitions[defName]; ok {
					if defMap, ok := def.(map[string]interface{}); ok {
						stripped := stripReadOnly(deepCopyMap(defMap))
						defBytes, _ := json.MarshalIndent(stripped, "", "  ")
						bodySchemaJSON = string(defBytes)

						// Generate template
						template := generateTemplate(stripped, spec.Definitions, map[string]bool{defName: true})
						templateBytes, _ := json.MarshalIndent(template, "", "  ")
						bodyTemplateJSON = string(templateBytes)
					} else {
						defBytes, _ := json.MarshalIndent(def, "", "  ")
						bodySchemaJSON = string(defBytes)
						bodyTemplateJSON = "{}"
					}
				}
			}

			metadata := &OpMetadata{
				Path:                path,
				Method:              strings.ToUpper(method),
				OperationID:         op.OperationID,
				Summary:             op.Summary,
				Description:         op.Description,
				PathParams:          pathParams,
				RequiredQueryParams: requiredQueryParams,
				BodyParam:           bodyParam,
				QueryFlags:          queryFlags,
				BodySchemaJSON:      bodySchemaJSON,
				BodyTemplateJSON:    bodyTemplateJSON,
				Deprecated:          op.Deprecated,
			}

			tagOperations[tag] = append(tagOperations[tag], metadata)
		}
	}

	// Generate Code
	var out strings.Builder
	out.WriteString(`// Copyright 2026 Google LLC
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

// Code generated by generate_api.go. DO NOT EDIT.
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var ApiCmd = &cobra.Command{
	Use:   "api",
	Short: "Make raw Looker API calls based on Swagger spec",
	Long:  "Make raw Looker API calls based on Swagger spec. Use 'looker-cli api help' to list categories.",
}

func init() {
	RootCmd.AddCommand(ApiCmd)
}
`)

	// Sort tags for deterministic code generation
	var tags []string
	for t := range tagOperations {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	for _, tag := range tags {
		ops := tagOperations[tag]
		sort.Slice(ops, func(i, j int) bool {
			return ops[i].OperationID < ops[j].OperationID
		})

		tagCmdName := fmt.Sprintf("api%sCmd", cleanTagName(tag))
		tagUse := strings.ToLower(strings.ReplaceAll(tag, " ", "_"))
		tagDesc := tagInfo[tag]
		if tagDesc == "" {
			tagDesc = tag + " commands"
		}

		fmt.Fprintf(&out, "\n// %s category\n", tag)
		fmt.Fprintf(&out, "var %s = &cobra.Command{\n", tagCmdName)
		fmt.Fprintf(&out, "\tUse:   %s,\n", strconv.Quote(tagUse))
		fmt.Fprintf(&out, "\tShort: %s,\n", strconv.Quote(tagDesc))
		out.WriteString("}\n\n")

		fmt.Fprintf(&out, "func init() {\n\tApiCmd.AddCommand(%s)\n}\n", tagCmdName)

		for _, op := range ops {
			opCmdName := fmt.Sprintf("api%s%sCmd", cleanTagName(tag), cleanOpName(op.OperationID))
			
			// Construct Use string
			var useParts []string
			useParts = append(useParts, op.OperationID)
			for _, p := range op.PathParams {
				useParts = append(useParts, fmt.Sprintf("[%s]", strings.ToUpper(p)))
			}
			for _, p := range op.RequiredQueryParams {
				useParts = append(useParts, fmt.Sprintf("[%s]", strings.ToUpper(p)))
			}
			if op.BodyParam != "" {
				useParts = append(useParts, fmt.Sprintf("[%s_JSON_FILE_OR_-]", strings.ToUpper(op.BodyParam)))
			}
			useStr := strings.Join(useParts, " ")

			numArgs := len(op.PathParams) + len(op.RequiredQueryParams)
			if op.BodyParam != "" {
				numArgs++
			}

			fmt.Fprintf(&out, "\nvar %s = &cobra.Command{\n", opCmdName)
			fmt.Fprintf(&out, "\tUse:   %s,\n", strconv.Quote(useStr))
			fmt.Fprintf(&out, "\tShort: %s,\n", strconv.Quote(op.Summary))
			fmt.Fprintf(&out, "\tLong:  %s,\n", strconv.Quote(op.Description))
			
			if op.BodyParam != "" {
				out.WriteString("\tArgs: func(cmd *cobra.Command, args []string) error {\n")
				out.WriteString("\t\tif val, _ := cmd.Flags().GetBool(\"describe-body\"); val {\n")
				out.WriteString("\t\t\treturn nil\n")
				out.WriteString("\t\t}\n")
				out.WriteString("\t\tif val, _ := cmd.Flags().GetBool(\"template\"); val {\n")
				out.WriteString("\t\t\treturn nil\n")
				out.WriteString("\t\t}\n")
				fmt.Fprintf(&out, "\t\treturn cobra.ExactArgs(%d)(cmd, args)\n", numArgs)
				out.WriteString("\t},\n")
			} else {
				fmt.Fprintf(&out, "\tArgs:  cobra.ExactArgs(%d),\n", numArgs)
			}

			out.WriteString("\tRunE: func(cmd *cobra.Command, args []string) error {\n")

			if op.BodyParam != "" {
				out.WriteString("\t\tif val, _ := cmd.Flags().GetBool(\"describe-body\"); val {\n")
				fmt.Fprintf(&out, "\t\t\tfmt.Println(%sBodySchema)\n", opCmdName)
				out.WriteString("\t\t\treturn nil\n")
				out.WriteString("\t\t}\n")
				out.WriteString("\t\tif val, _ := cmd.Flags().GetBool(\"template\"); val {\n")
				fmt.Fprintf(&out, "\t\t\tfmt.Println(%sBodyTemplate)\n", opCmdName)
				out.WriteString("\t\t\treturn nil\n")
				out.WriteString("\t\t}\n")
			}

			// queryParams map definition
			out.WriteString("\t\tqueryParams := make(map[string]string)\n")
			
			// Sort query flags for deterministic generation
			var qFlags []string
			for f := range op.QueryFlags {
				qFlags = append(qFlags, f)
			}
			sort.Strings(qFlags)

			for _, fName := range qFlags {
				p := op.QueryFlags[fName]
				if p.Type == "boolean" {
					fmt.Fprintf(&out, "\t\tif cmd.Flags().Changed(\"%s\") {\n", fName)
					fmt.Fprintf(&out, "\t\t\tval, _ := cmd.Flags().GetBool(\"%s\")\n", fName)
					fmt.Fprintf(&out, "\t\t\tqueryParams[\"%s\"] = strconv.FormatBool(val)\n", fName)
					out.WriteString("\t\t}\n")
				} else {
					fmt.Fprintf(&out, "\t\tif cmd.Flags().Changed(\"%s\") {\n", fName)
					fmt.Fprintf(&out, "\t\t\tval, _ := cmd.Flags().GetString(\"%s\")\n", fName)
					fmt.Fprintf(&out, "\t\t\tqueryParams[\"%s\"] = val\n", fName)
					out.WriteString("\t\t}\n")
				}
			}

			// Call executeApiCallGeneric
			pathParamsList := "[]string{"
			for i, p := range op.PathParams {
				if i > 0 {
					pathParamsList += ", "
				}
				pathParamsList += fmt.Sprintf("\"%s\"", p)
			}
			pathParamsList += "}"

			reqQueryParamsList := "[]string{"
			for i, p := range op.RequiredQueryParams {
				if i > 0 {
					reqQueryParamsList += ", "
				}
				reqQueryParamsList += fmt.Sprintf("\"%s\"", p)
			}
			reqQueryParamsList += "}"

			bodyParamStr := ""
			if op.BodyParam != "" {
				bodyParamStr = op.BodyParam
			}

			fmt.Fprintf(&out, "\t\treturn executeApiCallGeneric(cmd, \"%s\", \"%s\", %s, %s, \"%s\", queryParams, args)\n",
				op.Method, op.Path, pathParamsList, reqQueryParamsList, bodyParamStr)
			out.WriteString("\t},\n")
			out.WriteString("}\n\n")

			if op.BodyParam != "" {
				fmt.Fprintf(&out, "const %sBodySchema = %s\n", opCmdName, strconv.Quote(op.BodySchemaJSON))
				fmt.Fprintf(&out, "const %sBodyTemplate = %s\n\n", opCmdName, strconv.Quote(op.BodyTemplateJSON))
			}

			// init registration and flags
			fmt.Fprintf(&out, "func init() {\n\t%s.AddCommand(%s)\n", tagCmdName, opCmdName)
			if op.BodyParam != "" {
				fmt.Fprintf(&out, "\t%s.Flags().Bool(\"describe-body\", false, \"Describe the JSON schema expected in the body\")\n", opCmdName)
				fmt.Fprintf(&out, "\t%s.Flags().Bool(\"template\", false, \"Output a simplified JSON template for the request body\")\n", opCmdName)
			}
			for _, fName := range qFlags {
				p := op.QueryFlags[fName]
				desc := strconv.Quote(p.Description)
				if p.Type == "boolean" {
					fmt.Fprintf(&out, "\t%s.Flags().Bool(\"%s\", false, %s)\n", opCmdName, fName, desc)
				} else {
					fmt.Fprintf(&out, "\t%s.Flags().String(\"%s\", \"\", %s)\n", opCmdName, fName, desc)
				}
			}
			out.WriteString("}\n")
		}
	}

	// Append executeApiCallGeneric helper
	out.WriteString(`
func executeApiCallGeneric(cmd *cobra.Command, method, pathTemplate string, pathParams, requiredQueryParams []string, bodyParam string, queryParams map[string]string, args []string) error {
	ctx := cmd.Context()
	c, err := initClient(ctx, false)
	if err != nil {
		return err
	}

	actualPath := pathTemplate
	argIdx := 0

	// 1. Replace path parameters
	for _, paramName := range pathParams {
		if argIdx >= len(args) {
			return fmt.Errorf("missing required path parameter: %s", paramName)
		}
		val := args[argIdx]
		argIdx++
		actualPath = strings.ReplaceAll(actualPath, "{"+paramName+"}", url.PathEscape(val))
	}

	// 2. Parse required query parameters
	qParams := make(map[string]string)
	for _, paramName := range requiredQueryParams {
		if argIdx >= len(args) {
			return fmt.Errorf("missing required query parameter: %s", paramName)
		}
		val := args[argIdx]
		argIdx++
		qParams[paramName] = val
	}

	// 3. Parse body parameter
	var bodyReader io.Reader
	var bodyBytes []byte
	if bodyParam != "" {
		if argIdx >= len(args) {
			return fmt.Errorf("missing required body parameter (JSON file path or '-' for stdin)")
		}
		bodyFile := args[argIdx]
		argIdx++

		if bodyFile == "-" {
			bodyBytes, err = io.ReadAll(os.Stdin)
		} else {
			bodyBytes, err = os.ReadFile(bodyFile)
		}
		if err != nil {
			return fmt.Errorf("failed to read body from %s: %w", bodyFile, err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// 4. Construct URL
	u, err := url.Parse(c.Session.Config.BaseUrl)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = "/api/4.0" + actualPath

	q := u.Query()
	// Set required query params
	for k, v := range qParams {
		q.Set(k, v)
	}
	// Set optional query params from flags
	for k, v := range queryParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	// 5. Create HTTP request
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("looker-cli %s", Version))
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if cfgDebug {
		fmt.Printf("--> %s %s\n", method, u.String())
		for k, v := range req.Header {
			fmt.Printf("Header %s: %s\n", k, v)
		}
		if bodyParam != "" {
			fmt.Printf("Request Body: %s\n", string(bodyBytes))
		}
	}

	// 6. Execute request
	resp, err := c.Session.Client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 7. Read response
	outBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if cfgDebug {
		fmt.Printf("<-- %s\n", resp.Status)
		fmt.Printf("Response Body: %s\n", string(outBytes))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API returned error %s: %s", resp.Status, string(outBytes))
	}

	// Attempt to pretty print if it is JSON
	var raw interface{}
	if err := json.Unmarshal(outBytes, &raw); err == nil {
		prettyBytes, err := json.MarshalIndent(raw, "", "  ")
		if err == nil {
			fmt.Println(string(prettyBytes))
			return nil
		}
	}

	// Fallback for non-JSON (like CSV, SQL, Images)
	_, _ = os.Stdout.Write(outBytes)
	return nil
}
`)

	// Write output
	dir := filepath.Dir(outputFile)
	_ = os.MkdirAll(dir, 0755)
	err = os.WriteFile(outputFile, []byte(out.String()), 0644)
	if err != nil {
		fmt.Printf("Failed to write output file %s: %v\n", outputFile, err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated %s\n", outputFile)
}

type OpMetadata struct {
	Path                string
	Method              string
	OperationID         string
	Summary             string
	Description         string
	PathParams          []string
	RequiredQueryParams []string
	BodyParam           string
	QueryFlags          map[string]*Parameter
	BodySchemaJSON      string
	BodyTemplateJSON    string
	Deprecated          bool
}

func cleanTagName(tag string) string {
	// Convert "UserAttribute" to "UserAttribute", "ApiAuth" to "ApiAuth"
	// Remove spaces and non-alphanumeric
	reg := regexp.MustCompile(`[^a-zA-Z0-9]`)
	return reg.ReplaceAllString(tag, "")
}

func cleanOpName(op string) string {
	// Convert "create_query_task" to "CreateQueryTask" (CamelCase)
	parts := strings.Split(op, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func stripReadOnly(schema interface{}) interface{} {
	m, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}

	// Check if this object itself is readOnly
	if ro, ok := m["readOnly"].(bool); ok && ro {
		return nil
	}

	// Process properties recursively
	if props, ok := m["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for k, v := range props {
			stripped := stripReadOnly(v)
			if stripped != nil {
				newProps[k] = stripped
			}
		}
		m["properties"] = newProps
	}

	// Process nested schemas in arrays
	if items, ok := m["items"].(map[string]interface{}); ok {
		m["items"] = stripReadOnly(items)
	}

	return m
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{})
	for k, v := range m {
		switch vm := v.(type) {
		case map[string]interface{}:
			cp[k] = deepCopyMap(vm)
		case []interface{}:
			cp[k] = deepCopySlice(vm)
		default:
			cp[k] = v
		}
	}
	return cp
}

func deepCopySlice(s []interface{}) []interface{} {
	cp := make([]interface{}, len(s))
	for i, v := range s {
		switch vm := v.(type) {
		case map[string]interface{}:
			cp[i] = deepCopyMap(vm)
		case []interface{}:
			cp[i] = deepCopySlice(vm)
		default:
			cp[i] = v
		}
	}
	return cp
}

func generateTemplate(schema interface{}, definitions map[string]interface{}, visited map[string]bool) interface{} {
	m, ok := schema.(map[string]interface{})
	if !ok {
		return nil
	}

	if ro, ok := m["readOnly"].(bool); ok && ro {
		return nil
	}

	if ref, ok := m["$ref"].(string); ok {
		defName := strings.TrimPrefix(ref, "#/definitions/")
		if visited[defName] {
			return nil
		}
		if def, ok := definitions[defName]; ok {
			newVisited := make(map[string]bool)
			for k, v := range visited {
				newVisited[k] = v
			}
			newVisited[defName] = true
			return generateTemplate(def, definitions, newVisited)
		}
		return nil
	}

	tType, _ := m["type"].(string)
	switch tType {
	case "object":
		properties, ok := m["properties"].(map[string]interface{})
		if !ok {
			return map[string]interface{}{}
		}
		res := make(map[string]interface{})
		for k, v := range properties {
			if propMap, ok := v.(map[string]interface{}); ok {
				if ro, ok := propMap["readOnly"].(bool); ok && ro {
					continue
				}
			}
			val := generateTemplate(v, definitions, visited)
			if val != nil {
				res[k] = val
			}
		}
		return res
	case "array":
		items, ok := m["items"].(map[string]interface{})
		if !ok {
			return []interface{}{}
		}
		itemTemplate := generateTemplate(items, definitions, visited)
		if itemTemplate != nil {
			return []interface{}{itemTemplate}
		}
		return []interface{}{}
	case "string":
		if defVal, ok := m["default"]; ok {
			return defVal
		}
		if enumVals, ok := m["enum"].([]interface{}); ok && len(enumVals) > 0 {
			return enumVals[0]
		}
		if xLookerVals, ok := m["x-looker-values"].([]interface{}); ok && len(xLookerVals) > 0 {
			return xLookerVals[0]
		}
		return ""
	case "integer", "number":
		if defVal, ok := m["default"]; ok {
			return defVal
		}
		return 0
	case "boolean":
		if defVal, ok := m["default"]; ok {
			return defVal
		}
		return false
	default:
		if _, ok := m["properties"].(map[string]interface{}); ok {
			m["type"] = "object"
			return generateTemplate(m, definitions, visited)
		}
		return nil
	}
}
