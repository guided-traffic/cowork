package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Memory keeps what one process of cowork-mcp leaves the next: when
// session_start last ran for a binding, so "since the last session" has a
// meaning (docs/adr/0042 D5), and the model Claude Code named to the
// SessionStart hook in a project directory, or to the PostModelSwitch hook
// after a switch, which the MCP server of that directory puts into its agent
// mark (docs/adr/0067 D5, docs/adr/0036 D3).
type Memory interface {
	// LastStart is the time of the previous start for the binding; ok is
	// false when there was none.
	LastStart(key MemoryKey) (at time.Time, ok bool, err error)
	// SetLastStart records a start.
	SetLastStart(key MemoryKey, at time.Time) error
	// Model is the model last recorded for the project directory; "" when
	// none was, or the last record was "".
	Model(projectDir string) (string, error)
	// SetModel records the model of the session started or switched in the
	// project directory; "" records that a start named none.
	SetModel(projectDir, model string) error
}

// MemoryKey names a binding of an installation: its team's slug and its
// project's key.
type MemoryKey struct {
	Installation, Team, Project string
}

// InMemory is a Memory that lives as long as the process: a host without a
// place to keep a file, and the tests.
type InMemory struct {
	mu     sync.Mutex
	at     map[MemoryKey]time.Time
	models map[string]string
}

// LastStart reads the time recorded for the key.
func (m *InMemory) LastStart(key MemoryKey) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.at[key]
	return t, ok, nil
}

// SetLastStart records the time for the key.
func (m *InMemory) SetLastStart(key MemoryKey, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.at == nil {
		m.at = map[MemoryKey]time.Time{}
	}
	m.at[key] = at
	return nil
}

// Model reads the model recorded for the project directory.
func (m *InMemory) Model(projectDir string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.models[projectDir], nil
}

// SetModel records the model for the project directory.
func (m *InMemory) SetModel(projectDir, model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.models == nil {
		m.models = map[string]string{}
	}
	m.models[projectDir] = model
	return nil
}

// FileMemory keeps the times in one small file per installation and binding
// under a directory, the user's cache directory by default — the only file
// the MCP server writes (docs/adr/0042 D5, docs/adr/0067 D5) —, and the model
// in one file per project directory, which the SessionStart and
// PostModelSwitch hooks write and the server reads. A missing file is no
// previous session, and no model.
type FileMemory struct {
	Dir string
}

// DefaultMemoryDir is the directory under the user's cache directory.
func DefaultMemoryDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find the user's cache directory: %w", err)
	}
	return filepath.Join(cache, "cowork-mcp"), nil
}

// memoryFile is what a file holds.
type memoryFile struct {
	Installation string    `json:"installation"`
	Binding      string    `json:"binding"`
	LastStart    time.Time `json:"last_start"`
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9.-]+`)

// path is the file of a key: the installation's host and port, the team and
// the project, readable and safe as a file name.
func (m FileMemory) path(key MemoryKey) string {
	host := key.Installation
	if u, err := url.Parse(key.Installation); err == nil && u.Host != "" {
		host = u.Host + u.Path
	}
	name := unsafeName.ReplaceAllString(host+"_"+key.Team+"_"+key.Project, "_")
	return filepath.Join(m.Dir, name+".json")
}

// LastStart reads the time recorded for the key.
func (m FileMemory) LastStart(key MemoryKey) (time.Time, bool, error) {
	raw, err := os.ReadFile(m.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read the session memory: %w", err)
	}
	var f memoryFile
	if err := json.Unmarshal(raw, &f); err != nil || f.LastStart.IsZero() {
		// A damaged file is no previous session; the next start rewrites it.
		return time.Time{}, false, nil
	}
	return f.LastStart, true, nil
}

// SetLastStart writes the time for the key.
func (m FileMemory) SetLastStart(key MemoryKey, at time.Time) error {
	return m.write(m.path(key), memoryFile{Installation: key.Installation, Binding: key.Team + "/" + key.Project, LastStart: at.UTC()})
}

// modelFile is what the file of a project directory holds: the directory,
// so that another directory's file is never read as its own, and the model.
type modelFile struct {
	ProjectDir string `json:"project_dir"`
	Model      string `json:"model"`
}

// modelPath is the file of a project directory, named by a hash of the path:
// a path made safe as a file name would let two directories share a file.
func (m FileMemory) modelPath(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return filepath.Join(m.Dir, "model-"+hex.EncodeToString(sum[:8])+".json")
}

// Model reads the model recorded for the project directory.
func (m FileMemory) Model(projectDir string) (string, error) {
	raw, err := os.ReadFile(m.modelPath(projectDir))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the session memory: %w", err)
	}
	var f modelFile
	if err := json.Unmarshal(raw, &f); err != nil || f.ProjectDir != projectDir {
		// A damaged file, or another directory's, names no model.
		return "", nil
	}
	return f.Model, nil
}

// SetModel writes the model for the project directory.
func (m FileMemory) SetModel(projectDir, model string) error {
	return m.write(m.modelPath(projectDir), modelFile{ProjectDir: projectDir, Model: model})
}

// write writes one file of the memory: to a file beside it first, then
// renamed over it, so a reader never sees half a file.
func (m FileMemory) write(path string, v any) error {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return fmt.Errorf("make the session memory's directory: %w", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode the session memory: %w", err)
	}
	tmp, err := os.CreateTemp(m.Dir, ".memory-*")
	if err != nil {
		return fmt.Errorf("write the session memory: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write the session memory: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write the session memory: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write the session memory: %w", err)
	}
	return nil
}
