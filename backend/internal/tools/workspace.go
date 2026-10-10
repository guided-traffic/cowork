package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// Workspace is the repository a terminal session runs in, read from the
// working directory and nowhere else (docs/adr/0041 D4).
type Workspace interface {
	// Remotes are the repository's remotes, origin first, each URL without
	// its credentials; none outside a repository.
	Remotes(ctx context.Context) ([]Remote, error)
	// Path is the working directory relative to the repository root, ""
	// at the root or outside a repository.
	Path(ctx context.Context) (string, error)
	// BindingFile is the nearest .cowork.yaml at or above the working
	// directory inside the repository (docs/adr/0066 D4), nil when none.
	BindingFile(ctx context.Context) (*BindingFile, error)
	// WorkedSince reports whether the repository shows work since a time:
	// a commit, or a changed file modified after it.
	WorkedSince(ctx context.Context, since time.Time) (bool, error)
}

// Remote is a git remote, its URL without credentials.
type Remote struct {
	Name, URL string
}

// BindingFile is a .cowork.yaml (docs/adr/0066 D4): team and project, the
// sub-directory of a monorepo it binds — by default the directory it is in —
// and the installation it belongs to.
type BindingFile struct {
	Team string `yaml:"team"`
	// Tenant is team under its name before, read for one release beside it
	// (docs/adr/0005 D1); a file read holds the team's slug in both, so that
	// lookup --json names it under both keys.
	Tenant  string `yaml:"tenant"`
	Project string `yaml:"project"`
	Path    string `yaml:"path"`
	URL     string `yaml:"url"`
	// File is where it was found, for messages.
	File string `yaml:"-"`
}

// BindingFileName is the file's name.
const BindingFileName = ".cowork.yaml"

// gitTimeout bounds one git command.
const gitTimeout = 3 * time.Second

// GitWorkspace reads a directory with the git command line.
type GitWorkspace struct {
	Dir string
}

// git runs one git command in the directory and returns its output; a
// directory outside a repository is an error from git.
func (g GitWorkspace) git(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Dir}, args...)...) // #nosec G204 -- git with fixed subcommands; the directory is the session's own
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// root is the repository's top directory, or "" outside a repository.
func (g GitWorkspace) root(ctx context.Context) string {
	out, err := g.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Remotes reads git remote -v: the fetch URLs, origin first, then by name.
func (g GitWorkspace) Remotes(ctx context.Context) ([]Remote, error) {
	if g.root(ctx) == "" {
		return nil, nil
	}
	out, err := g.git(ctx, "remote", "-v")
	if err != nil {
		return nil, err
	}
	return parseRemotes(out), nil
}

// parseRemotes reads the lines "name<TAB>url (fetch)" of git remote -v.
func parseRemotes(out []byte) []Remote {
	var remotes []Remote
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), "\t")
		if !ok || !strings.HasSuffix(rest, " (fetch)") || seen[name] {
			continue
		}
		seen[name] = true
		remotes = append(remotes, Remote{Name: name, URL: domain.SanitiseRemote(strings.TrimSuffix(rest, " (fetch)"))})
	}
	slices.SortStableFunc(remotes, func(a, b Remote) int {
		switch {
		case a.Name == b.Name:
			return 0
		case a.Name == "origin":
			return -1
		case b.Name == "origin":
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
	return remotes
}

// Path reads git rev-parse --show-prefix.
func (g GitWorkspace) Path(ctx context.Context) (string, error) {
	if g.root(ctx) == "" {
		return "", nil
	}
	out, err := g.git(ctx, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	return strings.Trim(strings.TrimSpace(string(out)), "/"), nil
}

// BindingFile walks from the working directory up to the repository root —
// or, outside a repository, looks in the working directory only.
func (g GitWorkspace) BindingFile(ctx context.Context) (*BindingFile, error) {
	dir, err := filepath.Abs(g.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolve the working directory: %w", err)
	}
	root := g.root(ctx)
	if root == "" {
		root = dir
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for {
		path := filepath.Join(dir, BindingFileName)
		f, err := readBindingFile(path)
		if err != nil || f != nil {
			if f != nil && f.Path == "" {
				if rel, err := filepath.Rel(root, dir); err == nil && rel != "." {
					f.Path = filepath.ToSlash(rel)
				}
			}
			return f, err
		}
		parent := filepath.Dir(dir)
		if dir == root || parent == dir || !strings.HasPrefix(dir, root) {
			return nil, nil
		}
		dir = parent
	}
}

// readBindingFile reads and checks one .cowork.yaml; nil when there is none.
func readBindingFile(path string) (*BindingFile, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the binding file of the session's own working directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f BindingFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s is not a binding file: %w", path, err)
	}
	f.File = path
	switch {
	case f.Team != "" && f.Tenant != "" && f.Team != f.Tenant:
		return nil, fmt.Errorf("%s: team and tenant differ: tenant is the name before of team, read for one release; keep team alone", path)
	case f.Team == "":
		f.Team = f.Tenant
	}
	f.Tenant = f.Team
	switch {
	case !domain.ValidTenantSlug(f.Team):
		return nil, fmt.Errorf("%s: team must be a team's slug", path)
	case !domain.ValidProjectKey(f.Project):
		return nil, fmt.Errorf("%s: project must be a project key, upper case", path)
	}
	if f.Path, err = domain.NormaliseRepositoryPath(f.Path); err != nil {
		return nil, fmt.Errorf("%s: path %w", path, err)
	}
	f.URL = strings.TrimRight(strings.TrimSpace(f.URL), "/")
	return &f, nil
}

// WorkedSince reads git: a commit after the time, or a file git reports as
// changed whose modification is after it. A deleted file has no time and does
// not count.
func (g GitWorkspace) WorkedSince(ctx context.Context, since time.Time) (bool, error) {
	root := g.root(ctx)
	if root == "" {
		return false, nil
	}
	out, err := g.git(ctx, "log", "-1", "--format=%H", "--since=@"+strconv.FormatInt(since.Unix(), 10))
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		return true, nil
	}
	out, err = g.git(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	for _, path := range changedPaths(out) {
		info, err := os.Stat(filepath.Join(root, path))
		if err == nil && info.ModTime().After(since) {
			return true, nil
		}
	}
	return false, nil
}

// changedPaths reads git status --porcelain=v1 -z: "XY path", and after a
// rename or copy the original path as an entry of its own, which is skipped.
func changedPaths(out []byte) []string {
	var paths []string
	entries := bytes.Split(out, []byte{0})
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		paths = append(paths, string(e[3:]))
		if e[0] == 'R' || e[0] == 'C' {
			i++
		}
	}
	return paths
}
