package api

import (
	"context"
	"errors"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// errNotBuilt answers the import and export routes until they are built.
var errNotBuilt = errors.New("the import and the export are not built yet")

// CreateImport reads an upload into a dry run (docs/adr/0051 D1, D2).
func (s *Server) CreateImport(context.Context, apigen.CreateImportRequestObject) (apigen.CreateImportResponseObject, error) {
	return nil, errNotBuilt
}

// GetImport answers an import job with its report (docs/adr/0051 D1).
func (s *Server) GetImport(context.Context, apigen.GetImportRequestObject) (apigen.GetImportResponseObject, error) {
	return nil, errNotBuilt
}

// ExecuteImport executes a dry run with its corrections (docs/adr/0051 D3).
func (s *Server) ExecuteImport(context.Context, apigen.ExecuteImportRequestObject) (apigen.ExecuteImportResponseObject, error) {
	return nil, errNotBuilt
}

// ExportProject answers the project's archive (docs/adr/0051 D4).
func (s *Server) ExportProject(context.Context, apigen.ExportProjectRequestObject) (apigen.ExportProjectResponseObject, error) {
	return nil, errNotBuilt
}

// ExportTenant answers the tenant's archive (docs/adr/0051 D4).
func (s *Server) ExportTenant(context.Context, apigen.ExportTenantRequestObject) (apigen.ExportTenantResponseObject, error) {
	return nil, errNotBuilt
}
