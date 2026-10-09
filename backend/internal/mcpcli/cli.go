// Package mcpcli is the command line of cowork-mcp (docs/adr/0041,
// docs/adr/0067, docs/adr/0070): serve, the MCP server over stdio; the hook
// modes session-context, session-end and model-switch; and the workflow
// subcommands token check, lookup, export and import. One configuration, one generated
// client and one tool catalogue for all of them. cmd/cowork-mcp is its main; a
// test runs it with its own environment and streams.
package mcpcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/mcpserver"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// Build is what the linker sets on the binary (docs/adr/0041 D2).
type Build struct {
	Version, Commit, Time string
}

// Env is what the command runs with.
type Env struct {
	Args   []string
	Lookup func(string) (string, bool)
	// Stdin is a hook's input; nil when there is none, at a terminal.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Dir is the working directory when neither the hook's input nor the
	// environment names one.
	Dir   string
	Build Build
	// Memory replaces the files under the user's cache directory, Doer the
	// network, Workspace the git command line: all three for tests.
	Memory    tools.Memory
	Doer      apigen.HttpRequestDoer
	Workspace tools.Workspace
	// Transport replaces stdio for serve, for tests.
	Transport mcp.Transport
}

// Exit codes as the backend binary has them (docs/adr/0070 D4).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// The time budgets: a hook must answer within the five seconds Claude Code
// gives it (docs/adr/0067 D2), the server's start-up check within ten, one
// request within thirty.
const (
	hookBudget    = 4500 * time.Millisecond
	startBudget   = 10 * time.Second
	requestBudget = 30 * time.Second
)

const usageText = `Usage: cowork-mcp <command>

Commands:
  serve             Serve the cowork tools to an MCP host over stdio.
  session-context   Print the session block for Claude Code's SessionStart hook.
  session-end       Print the reminder for Claude Code's Stop hook, when one is due.
  model-switch      Record the model of Claude Code's PostModelSwitch hook for the agent mark.
  token check       Report whether COWORK_TOKEN works against COWORK_URL, and what it may do.
  lookup            Print the binding of the working directory's repository, or the proposal.
  export <tenant>/<PROJECT> <dir>
                    Unpack the project's export into an empty or a new directory.
  import <tenant>/<PROJECT> <path> [--dry-run]
                    Import a directory of ticket files, an archive or a Markdown file into the
                    project: the dry run's report, then its execution's; --dry-run stops after
                    the report.
  version           Print the version, the commit and the API it was built against.

token check and lookup take --json. Configuration is COWORK_URL and COWORK_TOKEN; the
reference is README.md, the setup docs/operations/claude-code.md.
`

// Run dispatches the command line.
func Run(ctx context.Context, e Env) int {
	if len(e.Args) == 0 {
		fmt.Fprint(e.Stderr, usageText)
		return exitUsage
	}
	name, args := e.Args[0], e.Args[1:]
	jsonOut := len(args) > 0 && args[len(args)-1] == "--json"
	if jsonOut {
		args = args[:len(args)-1]
	}
	if name == "token" && len(args) == 1 && args[0] == "check" {
		name, args = "token check", nil
	}
	command, ok := commandTable()[name]
	set := false
	if ok && command.flag != "" {
		args, set = withoutFlag(args, command.flag)
	}
	if !ok || len(args) != command.args || (jsonOut && !command.json) {
		fmt.Fprintf(e.Stderr, "cowork-mcp: unknown command %q\n\n%s", strings.Join(e.Args, " "), usageText)
		return exitUsage
	}
	if command.runArgs != nil {
		return command.runArgs(ctx, e, args, set)
	}
	return command.run(ctx, e, jsonOut)
}

// command is one subcommand: what it runs, and whether it takes --json; one
// that takes arguments after its name says how many and runs with them, and
// with whether its one switch, flag, was given.
type command struct {
	run     func(ctx context.Context, e Env, jsonOut bool) int
	json    bool
	args    int
	flag    string
	runArgs func(ctx context.Context, e Env, args []string, set bool) int
}

// commandTable is the subcommands by the name typed.
func commandTable() map[string]command {
	return map[string]command{
		"serve":           {run: func(ctx context.Context, e Env, _ bool) int { return serve(ctx, e) }},
		"session-context": {run: func(ctx context.Context, e Env, _ bool) int { return sessionContext(ctx, e) }},
		"session-end":     {run: func(ctx context.Context, e Env, _ bool) int { return sessionEnd(ctx, e) }},
		"model-switch":    {run: func(_ context.Context, e Env, _ bool) int { return modelSwitch(e) }},
		"token check":     {run: tokenCheck, json: true},
		"lookup":          {run: lookupBinding, json: true},
		"export":          {args: 2, runArgs: exportProject},
		"import":          {args: 2, flag: flagDryRun, runArgs: importProject},
		"version":         {run: printVersion},
		"help":            {run: printHelp},
		"-h":              {run: printHelp},
		"--help":          {run: printHelp},
	}
}

func printVersion(_ context.Context, e Env, _ bool) int {
	fmt.Fprintf(e.Stdout, "cowork-mcp %s (commit %s, built %s, API /api/v1: %d operations)\n",
		e.Build.Version, e.Build.Commit, e.Build.Time, len(tools.Operations(tools.Catalogue())))
	return exitOK
}

func printHelp(_ context.Context, e Env, _ bool) int {
	fmt.Fprint(e.Stdout, usageText)
	return exitOK
}

// client is what a command talks to cowork with: the session, and the agent
// header it sends, settable once the client's name is known.
type client struct {
	session *tools.Session
	mu      sync.Mutex
	name    string
	model   string
	id      string
	// project, set for the server, is the project directory whose recorded
	// model the mark names while the client knows none of its own: MCP does
	// not tell a server its model, and Claude Code tells the SessionStart and
	// PostModelSwitch hooks (docs/adr/0067 D5). It is read at each request,
	// so a session started after the server, started again or switched to
	// another model is named.
	project string
}

func (c *client) header() string {
	model := c.model
	if model == "" && c.project != "" && c.session.Memory != nil {
		model, _ = c.session.Memory.Model(c.project)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return tools.AgentHeader(c.name, model, c.id)
}

func (c *client) setName(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != "" {
		c.name = name
	}
}

// connect builds the session of a command: the generated client over the
// network — or the test's doer — with the token and the agent header on
// every request, a transport failure retried where a repetition cannot act
// twice, no redirect followed; the working directory; the memory.
func connect(e Env, cfg config, name, model, sessionID string) *client {
	c := &client{name: name, model: model, id: sessionID}
	if c.id == "" {
		// A session id of its own, short: it rides on every act's record.
		c.id = strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	doer := e.Doer
	if doer == nil {
		doer = &http.Client{Timeout: requestBudget, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	api, err := apigen.NewClientWithResponses(cfg.url,
		apigen.WithHTTPClient(tools.Retrying{Next: doer, Attempts: 3, Wait: 300 * time.Millisecond}),
		apigen.WithRequestEditorFn(tools.Editor(cfg.token, c.header, "cowork-mcp/"+e.Build.Version)))
	if err != nil {
		panic(err) // only an unparsable server URL fails, and loadConfig parsed it
	}
	c.session = tools.NewSession(api, cfg.url)
	c.session.Workspace = e.Workspace
	if c.session.Workspace == nil {
		c.session.Workspace = tools.GitWorkspace{Dir: pickDir(e, cfg)}
	}
	// Without a cache directory there is no memory: nothing since the last
	// session, and no model for the mark.
	c.session.Memory, _ = openMemory(e)
	return c
}

// openMemory is the memory a command keeps between processes: the test's, or
// the files under the user's cache directory.
func openMemory(e Env) (tools.Memory, error) {
	if e.Memory != nil {
		return e.Memory, nil
	}
	dir, err := tools.DefaultMemoryDir()
	if err != nil {
		return nil, err
	}
	return tools.FileMemory{Dir: dir}, nil
}

// serve runs the MCP server over stdio until the host closes it. A missing
// or malformed configuration ends it at once, naming the variable
// (docs/adr/0041 D5); an installation that rejects the token or whose API
// this binary does not know is refused on every call (docs/adr/0040 D5), and
// one that cannot be reached is asked again at the next call.
func serve(ctx context.Context, e Env) int {
	logger := slog.New(slog.NewTextHandler(e.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg, err := loadConfig(e.Lookup)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	c := connect(e, cfg, "", "", "")
	c.project = cfg.project
	gate := &readiness{session: c.session, version: e.Build.Version, logger: logger}
	startCtx, cancel := context.WithTimeout(ctx, startBudget)
	err = gate.check(startCtx)
	cancel()
	var tok *tools.Token
	if err == nil {
		t := c.session.Token()
		tok = &t
	}
	srv := mcpserver.New(mcpserver.Options{Session: c.session, Token: tok, Ready: gate.check, Client: c.setName,
		Version: e.Build.Version, Logger: logger})
	transport := e.Transport
	if transport == nil {
		transport = &mcp.StdioTransport{}
	}
	if err := srv.Run(ctx, transport); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		logger.Error("the MCP session ended", "error", err)
		return exitError
	}
	return exitOK
}

// readiness is the start-up check of the server: the token read and the API
// known (docs/adr/0040 D5, docs/adr/0043 D6). A definite refusal is kept; a
// failure to reach the installation is tried again at the next call.
type readiness struct {
	session *tools.Session
	version string
	logger  *slog.Logger
	mu      sync.Mutex
	done    bool
	refusal error
}

func (r *readiness) check(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return r.refusal
	}
	err := tools.CheckCompatibility(ctx, r.session, tools.Catalogue(), r.version)
	if err == nil {
		_, err = r.session.ReadToken(ctx)
	}
	var api *tools.APIError
	var incompatible *tools.IncompatibleError
	switch {
	case err == nil:
		r.done = true
	case errors.As(err, &incompatible):
		r.done, r.refusal = true, err
	case errors.As(err, &api):
		r.done, r.refusal = true, tokenRefusal(r.session, api)
	default:
		r.logger.Warn("cowork cannot be reached; the next tool call tries again", "installation", r.session.Installation, "error", err)
		return fmt.Errorf("the installation %s cannot be reached: %w", r.session.Installation, err)
	}
	if r.refusal != nil {
		r.logger.Error("cowork-mcp refuses to serve tools", "reason", r.refusal)
	}
	return r.refusal
}

// tokenRefusal says what is wrong with the token, naming the variable and the
// token page, never the token (docs/adr/0041 D5).
func tokenRefusal(s *tools.Session, api *tools.APIError) error {
	switch api.Code() {
	case "unauthenticated", "token_expired", "token_revoked":
		return fmt.Errorf("the token in %s does not work against %s (%s); make a new one on %s", EnvToken, s.Installation, api.Code(), s.TokenPage())
	}
	return fmt.Errorf("the installation %s refused the start-up check: %s", s.Installation, api.Error())
}

// hookInput is what Claude Code hands a command hook on standard input; the
// fields this binary reads (code.claude.com/docs/en/hooks).
type hookInput struct {
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	Source         string `json:"source"`
	Model          string `json:"model"`
	StopHookActive bool   `json:"stop_hook_active"`
	// ToModel is the model a PostModelSwitch hook names, the one the
	// session switched to.
	ToModel string `json:"to_model"`
	// AgentID is present only when the hook fires inside a subagent; a
	// session started with --agent names its agent_type and no agent_id.
	AgentID string `json:"agent_id"`
}

func readHook(r io.Reader) hookInput {
	var in hookInput
	if r != nil {
		_ = json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&in)
	}
	return in
}

// hookConfig is the configuration of a hook mode: an absent installation or
// token is silence (docs/adr/0067 D2), a malformed one a line naming it.
func hookConfig(e Env, in hookInput) (config, bool) {
	cfg, err := loadConfig(e.Lookup)
	if errors.Is(err, errUnconfigured) {
		return cfg, false
	}
	if err != nil {
		fmt.Fprintf(e.Stdout, "cowork: %v\n", err)
		return cfg, false
	}
	if in.Cwd != "" {
		cfg.dir = in.Cwd
	}
	return cfg, true
}

// sessionContext is the SessionStart hook (docs/adr/0067 D1, D2, D5): the
// session block on standard output, nothing in a directory with no binding to
// tell, and on an error one line naming the cause and the token page. It
// records the model Claude Code names for the server of the project
// directory. It always exits 0: a session is never blocked by it.
func sessionContext(ctx context.Context, e Env) int {
	in := readHook(e.Stdin)
	cfg, ok := hookConfig(e, in)
	if !ok {
		return exitOK
	}
	ctx, cancel := context.WithTimeout(ctx, hookBudget)
	defer cancel()
	c := connect(e, cfg, "claude-code", in.Model, in.SessionID)
	// Claude Code may leave the model out. After /clear or a compaction the
	// running Claude Code goes on with its model, and the one recorded before
	// stands; any other start without one, a session restored through
	// conversation recovery for one, records none, so the mark says unknown
	// rather than an older session's model. A failure costs the mark its model.
	if cfg.project != "" && c.session.Memory != nil && (in.Model != "" || (in.Source != "clear" && in.Source != "compact")) {
		_ = c.session.Memory.SetModel(cfg.project, in.Model)
	}
	if _, err := tools.CheckVersion(ctx, c.session, e.Build.Version); err != nil {
		fmt.Fprintln(e.Stdout, hookFailure(c.session, err))
		return exitOK
	}
	// A compaction continues the session it compacts: its start stays the
	// session's start (docs/adr/0067 D4).
	block, silent, err := tools.Start(ctx, c.session, tools.StartOptions{Record: in.Source != "compact"})
	switch {
	case err != nil:
		fmt.Fprintln(e.Stdout, hookFailure(c.session, err))
	case !silent:
		fmt.Fprint(e.Stdout, block)
	}
	return exitOK
}

// hookFailure is the one line a hook prints when cowork does not answer as it
// should (docs/adr/0067 D5).
func hookFailure(s *tools.Session, err error) string {
	var api *tools.APIError
	var incompatible *tools.IncompatibleError
	switch {
	case errors.As(err, &incompatible):
		return "cowork: " + incompatible.Error() + "."
	case errors.As(err, &api) && (api.Code() == "unauthenticated" || api.Code() == "token_expired" || api.Code() == "token_revoked"):
		return "cowork: " + tokenRefusal(s, api).Error() + "."
	case errors.As(err, &api):
		return fmt.Sprintf("cowork: the session start failed — %s. The token page is %s.", api.Error(), s.TokenPage())
	case errors.Is(err, context.DeadlineExceeded):
		return "cowork: " + s.Installation + " did not answer within the session start's time; session_start can try again."
	default:
		return fmt.Sprintf("cowork: %s cannot be reached (%v); session_start can try again later. The token page is %s.",
			s.Installation, err, s.TokenPage())
	}
}

// stopOutput is what the Stop hook prints: a message to the person, which
// continues nothing and blocks nothing (docs/adr/0067 D4).
type stopOutput struct {
	SystemMessage string `json:"systemMessage"`
}

// sessionEnd is the Stop hook (docs/adr/0067 D4): the one-line reminder when
// one is due, as a message to the person; silent otherwise, and on every
// error — the session start has said what is wrong.
func sessionEnd(ctx context.Context, e Env) int {
	in := readHook(e.Stdin)
	if in.StopHookActive {
		return exitOK
	}
	cfg, ok := hookConfig(e, in)
	if !ok {
		return exitOK
	}
	ctx, cancel := context.WithTimeout(ctx, hookBudget)
	defer cancel()
	c := connect(e, cfg, "claude-code", in.Model, in.SessionID)
	line, err := tools.Remind(ctx, c.session)
	if err != nil || line == "" {
		return exitOK
	}
	_ = json.NewEncoder(e.Stdout).Encode(stopOutput{SystemMessage: "cowork: " + line})
	return exitOK
}

// modelSwitch is the PostModelSwitch hook (docs/adr/0067 D5): it records the
// model the session switched to — by /model, an automatic fallback, opusplan
// entering or leaving plan mode, the model restored on a resume — where the
// SessionStart hook records the model it started with, so the server of the
// project directory names it in the mark of its next act. A switch inside a
// subagent is not the session's and changes nothing, nor does an input
// without to_model; an unconfigured client records nothing, as it records no
// start. Nothing goes to standard output, which Claude Code would add to the
// model's context: a configuration it cannot use or a record it cannot write
// is one line on standard error, which Claude Code keeps in its debug log. It
// always exits 0: the model has switched, and the hook cannot undo it.
func modelSwitch(e Env) int {
	in := readHook(e.Stdin)
	model := strings.TrimSpace(in.ToModel)
	if model == "" || in.AgentID != "" {
		return exitOK
	}
	cfg, err := loadConfig(e.Lookup)
	switch {
	case errors.Is(err, errUnconfigured):
		return exitOK
	case err != nil:
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitOK
	case cfg.project == "":
		return exitOK
	}
	memory, err := openMemory(e)
	if err == nil {
		err = memory.SetModel(cfg.project, model)
	}
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: the model switch is not recorded for the agent mark: %v\n", err)
	}
	return exitOK
}

// pickDir is the working directory of a command run at a terminal.
func pickDir(e Env, cfg config) string {
	if cfg.dir != "" {
		return cfg.dir
	}
	if e.Dir != "" {
		return e.Dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
