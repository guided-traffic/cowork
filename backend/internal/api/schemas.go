package api

import (
	"context"
	"encoding/json"
	"fmt"

	apispec "github.com/guided-traffic/cowork/backend/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// GetCoworkYamlSchema answers the JSON Schema of a repository's .cowork.yaml
// (docs/adr/0066 D4), for an editor to validate the file against.
func (s *Server) GetCoworkYamlSchema(context.Context, apigen.GetCoworkYamlSchemaRequestObject) (apigen.GetCoworkYamlSchemaResponseObject, error) {
	var schema apigen.GetCoworkYamlSchema200JSONResponse
	if err := json.Unmarshal(apispec.CoworkYAMLSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode the .cowork.yaml schema: %w", err)
	}
	return schema, nil
}
