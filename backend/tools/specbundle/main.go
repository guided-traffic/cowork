// Command specbundle bundles the API document, split per path family
// (docs/adr/0046 D1), into the one JSON document the server embeds and serves
// and oapi-codegen reads: every external reference is internalised under the
// last segment of its JSON pointer, and the result is validated.
//
// It reads api/openapi.yaml and writes api/openapi.gen.json, relative to
// backend/, where `make generate` runs it.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	rootFile    = "api/openapi.yaml"
	bundledFile = "api/openapi.gen.json"
)

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
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := os.WriteFile(bundledFile, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", bundledFile, err)
	}
	return nil
}
