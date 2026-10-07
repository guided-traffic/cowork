// Command crdschema writes the JSON schemas kubeconform validates custom
// resources against, from the CustomResourceDefinitions of an operator's
// release: one file per version a definition serves, at
// <out>/<group>/<kind>_<version>.json with the kind in lower case — the
// layout of the schema location `make examples-lint` gives kubeconform
// (docs/adr/0058 D2). Every object of a schema that lists its properties and
// keeps no unknown field gets additionalProperties false, as kubectl
// validates, so a misspelt field fails; the root stays open for apiVersion,
// kind and metadata.
//
//	go run ./tools/crdschema -out <dir> <crd.yaml>...
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

func main() {
	out := flag.String("out", "", "the directory the schemas are written to")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: crdschema -out <dir> <crd.yaml>...")
		os.Exit(2)
	}
	for _, path := range flag.Args() {
		written, err := convertFile(path, *out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "crdschema: %s: %v\n", path, err)
			os.Exit(1)
		}
		for _, w := range written {
			fmt.Println(w)
		}
	}
}

// convertFile writes the schemas of every definition in one file and returns
// the paths it wrote.
func convertFile(path, out string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	schemas, err := convert(raw)
	if err != nil {
		return nil, err
	}
	written := make([]string, 0, len(schemas))
	for name, schema := range schemas {
		target := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return nil, fmt.Errorf("create the directory of %s: %w", name, err)
		}
		if err := os.WriteFile(target, schema, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		written = append(written, target)
	}
	return written, nil
}

// definition is the part of a CustomResourceDefinition a schema comes from.
type definition struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Group string `yaml:"group"`
		Names struct {
			Kind string `yaml:"kind"`
		} `yaml:"names"`
		Versions []struct {
			Name   string `yaml:"name"`
			Served bool   `yaml:"served"`
			Schema struct {
				OpenAPIV3Schema map[string]any `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

// convert reads every CustomResourceDefinition of a YAML stream and returns
// its schemas as JSON, by the relative path each belongs at. A stream that
// holds no definition, or a served version without a schema, is an error:
// kubeconform would otherwise find no schema and the lint would test nothing.
func convert(raw []byte) (map[string][]byte, error) {
	schemas := map[string][]byte{}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var d definition
		err := decoder.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse: %w", err)
		}
		if d.Kind != "CustomResourceDefinition" {
			continue
		}
		for _, v := range d.Spec.Versions {
			if !v.Served {
				continue
			}
			if v.Schema.OpenAPIV3Schema == nil {
				return nil, fmt.Errorf("%s %s %s serves no schema", d.Spec.Group, d.Spec.Names.Kind, v.Name)
			}
			closeObjects(v.Schema.OpenAPIV3Schema, true)
			schema, err := json.MarshalIndent(v.Schema.OpenAPIV3Schema, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("encode %s %s: %w", d.Spec.Names.Kind, v.Name, err)
			}
			name := fmt.Sprintf("%s/%s_%s.json", d.Spec.Group, strings.ToLower(d.Spec.Names.Kind), v.Name)
			schemas[name] = schema
		}
	}
	if len(schemas) == 0 {
		return nil, errors.New("no CustomResourceDefinition with a served version")
	}
	return schemas, nil
}

// closeObjects sets additionalProperties false on every object of a schema
// that lists its properties, says nothing of additional ones and does not
// keep unknown fields — beneath the root, which carries apiVersion, kind and
// metadata. It walks the schema's structure — the properties, the items, the
// schema of additional properties — and leaves the branches of allOf, anyOf,
// oneOf and not alone: a branch lists a part of the properties, and closed it
// would refuse an object the definition admits.
func closeObjects(schema map[string]any, root bool) {
	if properties, ok := schema["properties"].(map[string]any); ok {
		_, hasAdditional := schema["additionalProperties"]
		preserve, _ := schema["x-kubernetes-preserve-unknown-fields"].(bool)
		if !root && !hasAdditional && !preserve {
			schema["additionalProperties"] = false
		}
		for _, property := range properties {
			if p, ok := property.(map[string]any); ok {
				closeObjects(p, false)
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if child, ok := schema[key].(map[string]any); ok {
			closeObjects(child, false)
		}
	}
}
