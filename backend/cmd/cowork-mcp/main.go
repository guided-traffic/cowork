// Command cowork-mcp is the MCP server of cowork for Claude Code and any other
// MCP host (docs/adr/0040, docs/adr/0041): a thin client of the API over
// stdio, configured by COWORK_URL and COWORK_TOKEN. It also runs the hooks of
// docs/adr/0067 and the workflow subcommands of docs/adr/0070; the command
// line is internal/mcpcli.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/guided-traffic/cowork/backend/internal/mcpcli"
)

// Set by the linker; see the Makefile and the release workflow.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := mcpcli.Run(ctx, mcpcli.Env{
		Args: os.Args[1:], Lookup: os.LookupEnv, Stdin: hookInput(), Stdout: os.Stdout, Stderr: os.Stderr,
		Build: mcpcli.Build{Version: version, Commit: commit, Time: buildTime},
	})
	stop()
	os.Exit(code)
}

// hookInput is standard input when it is not a terminal: a hook's JSON, or
// the MCP host's messages, which serve reads itself.
func hookInput() io.Reader {
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
		return os.Stdin
	}
	return nil
}
