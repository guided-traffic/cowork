package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MCP server is a client of the API and nothing else (docs/adr/0040 D1,
// D3): its binary holds no path to the database — no store, no driver, no
// API handler — and no test package.
func TestTheBinaryIsAClientOnly(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	require.NoError(t, err)
	for _, pkg := range strings.Fields(string(out)) {
		for _, forbidden := range []string{
			"github.com/guided-traffic/cowork/backend/test/",
			"github.com/guided-traffic/cowork/backend/internal/store",
			"github.com/jackc/pgx",
			"github.com/minio/minio-go",
		} {
			assert.False(t, strings.HasPrefix(pkg, forbidden), "cmd/cowork-mcp depends on %s", pkg)
		}
		assert.NotEqual(t, "github.com/guided-traffic/cowork/backend/internal/api", pkg, "cmd/cowork-mcp depends on the API's handlers")
	}
}
