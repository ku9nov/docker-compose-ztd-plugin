package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
)

const (
	DefaultRegistryRelativePath = ".ztd/registry/projects.json"
	EnvRegistryPath             = "ZTD_REGISTRY_PATH"
)

type Entry struct {
	WorkingDir   string     `json:"workingDir"`
	RegisteredAt time.Time  `json:"registeredAt"`
	LastSeenAt   time.Time  `json:"lastSeenAt"`
	Disabled     bool       `json:"disabled,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
	LastErrorAt  *time.Time `json:"lastErrorAt,omitempty"`
}

type document struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

type Store struct {
	path string
	now  func() time.Time
}

func NewStore(path string) *Store {
	resolved := resolveRegistryPath(path)
	if resolved == "" {
		resolved = filepath.FromSlash(DefaultRegistryRelativePath)
	}
	return &Store{
		path: resolved,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func resolveRegistryPath(path string) string {
	if resolved := strings.TrimSpace(path); resolved != "" {
		return expandHomePath(resolved)
	}
	if fromEnv := strings.TrimSpace(os.Getenv(EnvRegistryPath)); fromEnv != "" {
		return expandHomePath(fromEnv)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(DefaultRegistryRelativePath))
}

func expandHomePath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			return home
		}
		return path
	}

	homePrefix := "~" + string(filepath.Separator)
	if !strings.HasPrefix(path, homePrefix) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, homePrefix))
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) List() ([]Entry, error) {
	doc, err := s.readDocument()
	if err != nil {
		return nil, err
	}
	entries := append([]Entry(nil), doc.Entries...)
	sortEntries(entries)
	return entries, nil
}

func (s *Store) Register(workingDir string) (Entry, error) {
	canonical, err := CanonicalizeWorkingDir(workingDir)
	if err != nil {
		return Entry{}, err
	}
	now := s.now()

	var out Entry
	err = s.withLock(func() error {
		doc, err := s.readDocument()
		if err != nil {
			return err
		}

		found := false
		for i := range doc.Entries {
			if doc.Entries[i].WorkingDir != canonical {
				continue
			}
			found = true
			doc.Entries[i].LastSeenAt = now
			doc.Entries[i].LastError = ""
			doc.Entries[i].LastErrorAt = nil
			out = doc.Entries[i]
			break
		}
		if !found {
			out = Entry{
				WorkingDir:   canonical,
				RegisteredAt: now,
				LastSeenAt:   now,
			}
			doc.Entries = append(doc.Entries, out)
		}

		sortEntries(doc.Entries)
		return s.writeDocument(doc)
	})
	if err != nil {
		return Entry{}, err
	}
	return out, nil
}

func (s *Store) SetLastError(workingDir string, runErr error) error {
	canonical, err := CanonicalizeWorkingDir(workingDir)
	if err != nil {
		return err
	}
	now := s.now()

	return s.withLock(func() error {
		doc, err := s.readDocument()
		if err != nil {
			return err
		}
		for i := range doc.Entries {
			if doc.Entries[i].WorkingDir != canonical {
				continue
			}
			doc.Entries[i].LastError = runErr.Error()
			doc.Entries[i].LastErrorAt = &now
			return s.writeDocument(doc)
		}
		return nil
	})
}

func (s *Store) ClearLastError(workingDir string) error {
	canonical, err := CanonicalizeWorkingDir(workingDir)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		doc, err := s.readDocument()
		if err != nil {
			return err
		}
		for i := range doc.Entries {
			if doc.Entries[i].WorkingDir != canonical {
				continue
			}
			doc.Entries[i].LastError = ""
			doc.Entries[i].LastErrorAt = nil
			return s.writeDocument(doc)
		}
		return nil
	})
}

func (s *Store) withLock(fn func() error) error {
	lockPath := s.path + ".lock"
	unlock, acquired, err := state.TryExclusiveFileLock(lockPath)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("registry lock is already held: %s", lockPath)
	}
	defer unlock()
	return fn()
}

func (s *Store) readDocument() (document, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return document{Version: 1, Entries: []Entry{}}, nil
		}
		return document{}, err
	}

	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("parse registry file %s: %w", s.path, err)
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	doc.Entries = dedupeEntries(doc.Entries)
	sortEntries(doc.Entries)
	return doc, nil
}

func (s *Store) writeDocument(doc document) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return configio.WriteAtomic(s.path, data, 0o644)
}

func CanonicalizeWorkingDir(workingDir string) (string, error) {
	raw := strings.TrimSpace(workingDir)
	if raw == "" {
		return "", fmt.Errorf("working directory is empty")
	}

	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func dedupeEntries(in []Entry) []Entry {
	if len(in) == 0 {
		return nil
	}

	byDir := make(map[string]Entry, len(in))
	for _, entry := range in {
		cleanDir := filepath.Clean(strings.TrimSpace(entry.WorkingDir))
		if cleanDir == "" || cleanDir == "." {
			continue
		}
		entry.WorkingDir = cleanDir
		current, exists := byDir[cleanDir]
		if !exists {
			byDir[cleanDir] = entry
			continue
		}
		if current.RegisteredAt.IsZero() || (!entry.RegisteredAt.IsZero() && entry.RegisteredAt.Before(current.RegisteredAt)) {
			current.RegisteredAt = entry.RegisteredAt
		}
		if entry.LastSeenAt.After(current.LastSeenAt) {
			current.LastSeenAt = entry.LastSeenAt
		}
		if current.LastError == "" {
			current.LastError = entry.LastError
			current.LastErrorAt = entry.LastErrorAt
		}
		current.Disabled = current.Disabled || entry.Disabled
		byDir[cleanDir] = current
	}

	out := make([]Entry, 0, len(byDir))
	for _, entry := range byDir {
		out = append(out, entry)
	}
	sortEntries(out)
	return out
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].WorkingDir < entries[j].WorkingDir
	})
}
