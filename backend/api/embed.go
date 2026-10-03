// Package apispec embeds the bundled API document, which `make generate`
// writes from the files beside this one (docs/adr/0046 D1, D5).
package apispec

import _ "embed"

// Document is the bundled OpenAPI document, as JSON.
//
//go:embed openapi.gen.json
var Document []byte
