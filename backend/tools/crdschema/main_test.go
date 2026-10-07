package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const definitions = `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: not-a-definition
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1alpha1
      served: false
      storage: false
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            apiVersion:
              type: string
            spec:
              type: object
              properties:
                size:
                  x-kubernetes-int-or-string: true
                  anyOf:
                    - type: integer
                    - type: string
                ports:
                  type: array
                  items:
                    type: object
                    properties:
                      port:
                        type: integer
                labels:
                  type: object
                  additionalProperties:
                    type: string
                extra:
                  type: object
                  x-kubernetes-preserve-unknown-fields: true
                  properties:
                    known:
                      type: string
              anyOf:
                - properties:
                    size: {}
                  required: [size]
`

// docs/adr/0058 D2: one schema per served version, at the path the
// examples' schema location names; every nested object that lists its
// properties is closed, so a misspelt field fails the lint, while the root,
// a map of strings, an object that keeps unknown fields and a branch of anyOf
// stay as the definition says.
func TestConvertWritesClosedSchemasPerServedVersion(t *testing.T) {
	schemas, err := convert([]byte(definitions))
	require.NoError(t, err)
	require.Len(t, schemas, 1, "the version that is not served has no schema")
	raw, ok := schemas["example.com/widget_v1.json"]
	require.True(t, ok, "the kind in lower case, the group as the directory")

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	assert.NotContains(t, schema, "additionalProperties", "the root stays open for apiVersion, kind and metadata")
	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)
	assert.Equal(t, false, spec["additionalProperties"], "a misspelt field of spec fails")
	properties := spec["properties"].(map[string]any)
	port := properties["ports"].(map[string]any)["items"].(map[string]any)
	assert.Equal(t, false, port["additionalProperties"], "the items of a list are closed as well")
	labels := properties["labels"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "string"}, labels["additionalProperties"], "a map keeps its schema of values")
	assert.NotContains(t, properties["extra"].(map[string]any), "additionalProperties", "an object that keeps unknown fields stays open")
	branch := spec["anyOf"].([]any)[0].(map[string]any)
	assert.NotContains(t, branch, "additionalProperties", "a branch lists a part of the properties and stays open")
}

func TestConvertRefusesAStreamWithoutADefinition(t *testing.T) {
	_, err := convert([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CustomResourceDefinition")

	_, err = convert([]byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: example.com
  names: {kind: Widget}
  versions:
    - name: v1
      served: true
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serves no schema")
}

func TestConvertFileWritesUnderTheGroup(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "widgets.yaml")
	require.NoError(t, os.WriteFile(in, []byte(definitions), 0o600))
	out := filepath.Join(dir, "schemas")
	written, err := convertFile(in, out)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(out, "example.com", "widget_v1.json")}, written)
	_, err = os.Stat(written[0])
	require.NoError(t, err)
}
