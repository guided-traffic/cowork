package tools

import (
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

// Memory keeps when session_start last ran for a binding, so "since the last
// session" has a meaning (docs/adr/0042 D5). It holds timestamps only.
type Memory interface {
	// LastStart is the time of the previous start for the binding; ok is
	// false when there was none.
	LastStart(key MemoryKey) (at time.Time, ok bool, err error)
	// SetLastStart records a start.
	SetLastStart(key MemoryKey, at time.Time) error
}

// MemoryKey names a binding of an installation.
type MemoryKey struct {
	Installation, Tenant, Project string
}

// InMemory is a Memory that lives as long as the process: a host without a
// place to keep a file, and the tests.
type InMemory struct {
	mu sync.Mutex
	at map[MemoryKey]time.Time
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

// FileMemory keeps the times in one small file per installation and binding
// under a directory, the user's cache directory by default — the only file
// the MCP server writes (docs/adr/0042 D5, docs/adr/0067 D5). A missing file
// is no previous session.
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

// path is the file of a key: the installation's host and port, the tenant
// and the project, readable and safe as a file name.
func (m FileMemory) path(key MemoryKey) string {
	host := key.Installation
	if u, err := url.Parse(key.Installation); err == nil && u.Host != "" {
		host = u.Host + u.Path
	}
	name := unsafeName.ReplaceAllString(host+"_"+key.Tenant+"_"+key.Project, "_")
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

// SetLastStart writes the time for the key: to a file beside it first, then
// renamed over it, so a reader never sees half a file.
func (m FileMemory) SetLastStart(key MemoryKey, at time.Time) error {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return fmt.Errorf("make the session memory's directory: %w", err)
	}
	raw, err := json.Marshal(memoryFile{Installation: key.Installation, Binding: key.Tenant + "/" + key.Project, LastStart: at.UTC()})
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
	if err := os.Rename(tmp.Name(), m.path(key)); err != nil {
		return fmt.Errorf("write the session memory: %w", err)
	}
	return nil
}
