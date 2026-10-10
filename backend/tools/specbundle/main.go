// Command specbundle bundles the API document, split per path family
// (docs/adr/0046 D1), into the one JSON document the server embeds and serves
// and oapi-codegen reads: every external reference is internalised under the
// last segment of its JSON pointer, the deprecated twins of the team paths
// are written beside them, and the result is validated.
//
// It reads api/openapi.yaml and writes api/openapi.gen.json, relative to
// backend/, where `make generate` runs it.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	rootFile    = "api/openapi.yaml"
	bundledFile = "api/openapi.gen.json"
)

// A tenant is called a team on every surface (docs/adr/0005 D1), and the path
// family under the name before stays served for one release (docs/adr/0023
// D1, docs/adr/0046 D7): every path under newFamily gets a twin under
// oldFamily that answers as it does. The source names only the new family;
// the twins are written here, so that nobody keeps two copies of a path.
const (
	newFamily = "/api/v1/teams"
	oldFamily = "/api/v1/tenants"
	newParam  = "{team}"
	oldParam  = "{tenant}"
	// twinTag marks every twin; neither generated client carries an
	// operation of it (api/oapi-codegen.yaml, frontend/ng-openapi-gen.json).
	twinTag = "tenants"
	// twinParam is the deprecated path parameter of the twins
	// (components/parameters.yaml).
	twinParam   = "#/components/parameters/TenantSlug"
	newParamRef = "#/components/parameters/TeamSlug"
	// twinSuffix ends the operationId of a twin whose operation kept its
	// name in the rename.
	twinSuffix = "Deprecated"
)

// renamedOperations maps the operationId of an operation the rename renamed
// to the one the release before served it under, which its twin keeps: a
// client of that release — cowork-mcp checks at its start that every
// operation it calls is in the served document (docs/adr/0040 D5) — finds it.
var renamedOperations = map[string]string{
	"listTeams":       "listTenants",
	"createTeam":      "createTenant",
	"getTeam":         "getTenant",
	"updateTeam":      "updateTenant",
	"exportTeam":      "exportTenant",
	"searchTeam":      "searchTenant",
	"listTeamTickets": "listTenantTickets",
	"listTeamTime":    "listTenantTime",
	"listTeamTokens":  "listTenantTokens",
	"revokeTeamToken": "revokeTenantToken",
}

// methods are the keys of a path item that hold an operation.
var methods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true,
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "specbundle: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(rootFile)
	if err != nil {
		return fmt.Errorf("load %s: %w", rootFile, err)
	}
	doc.InternalizeRefs(ctx, func(_ *openapi3.T, ref openapi3.ComponentRef) string {
		s := ref.RefString()
		return s[strings.LastIndex(s, "/")+1:]
	})
	if err := doc.Validate(ctx); err != nil {
		return fmt.Errorf("validate the bundled document: %w", err)
	}
	internal, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(internal, &tree); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := addTwins(tree); err != nil {
		return err
	}
	withTwins, err := json.Marshal(tree)
	if err != nil {
		return fmt.Errorf("encode the twins: %w", err)
	}
	// Loaded once more, so that the twins are validated with the rest and the
	// file keeps the order kin-openapi writes.
	bundled, err := openapi3.NewLoader().LoadFromData(withTwins)
	if err != nil {
		return fmt.Errorf("load the document with its twins: %w", err)
	}
	if err := bundled.Validate(ctx); err != nil {
		return fmt.Errorf("validate the document with its twins: %w", err)
	}
	b, err := json.MarshalIndent(bundled, "", "  ")
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := os.WriteFile(bundledFile, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", bundledFile, err)
	}
	return nil
}

// addTwins writes, for every path of the new family, its deprecated twin
// under the old one: the same operations, each marked deprecated and tagged
// twinTag, its operationId the one of the release before or suffixed, and the
// path parameter the deprecated one.
func addTwins(tree map[string]any) error {
	paths, ok := tree["paths"].(map[string]any)
	if !ok {
		return fmt.Errorf("the document has no paths")
	}
	names := make([]string, 0, len(paths))
	for path := range paths {
		if path == newFamily || strings.HasPrefix(path, newFamily+"/") {
			names = append(names, path)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no path under %s", newFamily)
	}
	sort.Strings(names)
	for _, path := range names {
		twin := oldFamily + strings.Replace(strings.TrimPrefix(path, newFamily), newParam, oldParam, 1)
		if _, taken := paths[twin]; taken {
			return fmt.Errorf("%s is in the source; its twin is written here", twin)
		}
		item, err := twinItem(paths[path], path)
		if err != nil {
			return err
		}
		paths[twin] = item
	}
	return nil
}

// twinItem is a deep copy of a path item with its operations made twins.
func twinItem(item any, path string) (map[string]any, error) {
	b, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("copy %s: %w", path, err)
	}
	var twin map[string]any
	if err := json.Unmarshal(b, &twin); err != nil {
		return nil, fmt.Errorf("copy %s: %w", path, err)
	}
	twinParams(twin)
	for key, value := range twin {
		if !methods[key] {
			continue
		}
		op, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s %s is no operation", key, path)
		}
		id, _ := op["operationId"].(string)
		if id == "" {
			return nil, fmt.Errorf("%s %s has no operationId", key, path)
		}
		if old, renamed := renamedOperations[id]; renamed {
			op["operationId"] = old
		} else {
			op["operationId"] = id + twinSuffix
		}
		op["deprecated"] = true
		op["tags"] = []any{twinTag}
		note := fmt.Sprintf("Deprecated: `%s`, served under the name before for one release and removed in a "+
			"later one, answering as it does (docs/adr/0005 D1, docs/adr/0046 D7).", path)
		if summary, _ := op["summary"].(string); summary != "" {
			op["summary"] = "Deprecated: " + summary
		}
		if description, _ := op["description"].(string); description != "" {
			op["description"] = note + "\n\n" + description
		} else {
			op["description"] = note
		}
		twinParams(op)
	}
	return twin, nil
}

// twinParams replaces the team's path parameter of a path item or an
// operation by the deprecated one.
func twinParams(node map[string]any) {
	params, _ := node["parameters"].([]any)
	for i, p := range params {
		param, _ := p.(map[string]any)
		if ref, _ := param["$ref"].(string); ref == newParamRef {
			params[i] = map[string]any{"$ref": twinParam}
		}
	}
}
