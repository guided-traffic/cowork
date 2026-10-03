package tools

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// IncompatibleError is an installation whose API this client does not know
// (docs/adr/0040 D5): another major version, or operations it lacks.
type IncompatibleError struct {
	Installation, Theirs, Ours string
	Missing                    []string
}

func (e *IncompatibleError) Error() string {
	if len(e.Missing) > 0 {
		return fmt.Sprintf("the installation at %s runs cowork %s, whose API lacks %s, which this cowork-mcp %s calls: "+
			"use a cowork-mcp of the installation's release", e.Installation, e.Theirs, strings.Join(e.Missing, ", "), e.Ours)
	}
	return fmt.Sprintf("the installation at %s runs cowork %s, another major version than this cowork-mcp %s: "+
		"use a cowork-mcp of the installation's release", e.Installation, e.Theirs, e.Ours)
}

var semver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// CheckVersion compares the major version of the installation with the
// client's (docs/adr/0040 D5); a build that names no version — a development
// build — is not compared.
func CheckVersion(ctx context.Context, s *Session, ours string) (theirs string, err error) {
	res, err := s.API.GetVersionWithResponse(ctx)
	if err := check(res, err, http.StatusOK); err != nil {
		return "", err
	}
	theirs = res.JSON200.Version
	a, b := semver.FindStringSubmatch(ours), semver.FindStringSubmatch(theirs)
	if a != nil && b != nil && a[1] != b[1] {
		return theirs, &IncompatibleError{Installation: s.Installation, Theirs: theirs, Ours: ours}
	}
	return theirs, nil
}

// CheckCompatibility is the start-up check of the MCP server
// (docs/adr/0040 D5): the major version, and every operation the catalogue
// calls present in the API document the installation serves.
func CheckCompatibility(ctx context.Context, s *Session, catalogue []Tool, ours string) error {
	theirs, err := CheckVersion(ctx, s, ours)
	if err != nil {
		return err
	}
	res, err := s.API.GetOpenAPIWithResponse(ctx)
	if err := check(res, err, http.StatusOK); err != nil {
		return err
	}
	if res.JSON200 == nil {
		return errNoBody
	}
	served := documentOperations(*res.JSON200)
	var missing []string
	for _, op := range Operations(catalogue) {
		if !slices.Contains(served, op) {
			missing = append(missing, op)
		}
	}
	if len(missing) > 0 {
		return &IncompatibleError{Installation: s.Installation, Theirs: theirs, Ours: ours, Missing: missing}
	}
	return nil
}

// documentOperations are the operationIds of an OpenAPI document.
func documentOperations(doc map[string]any) []string {
	var ops []string
	paths, _ := doc["paths"].(map[string]any)
	for _, item := range paths {
		methods, _ := item.(map[string]any)
		for _, op := range methods {
			if o, ok := op.(map[string]any); ok {
				if id, ok := o["operationId"].(string); ok {
					ops = append(ops, id)
				}
			}
		}
	}
	return ops
}
