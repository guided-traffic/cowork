// Command dashboard writes the Grafana dashboard the chart ships
// (docs/adr/0060 D3) from internal/metrics, where a test holds every panel to
// the instruments that exist. `make generate` runs it from backend/.
package main

import (
	"fmt"
	"os"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
)

const dashboardFile = "../deploy/helm/cowork/files/grafana-dashboard.json"

func main() {
	b, err := metrics.Dashboard()
	if err == nil {
		err = os.WriteFile(dashboardFile, b, 0o600)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: write %s: %v\n", dashboardFile, err)
		os.Exit(1)
	}
}
