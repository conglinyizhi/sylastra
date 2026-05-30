package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
)

// Store manages session persistence as JSON files.
type Store struct {
	dir  string
	mu   sync.Mutex
}

// NewStore creates a session store rooted at dir.
// It creates the directory if it doesn't exist.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string { return s.dir }

// ── Index ───────────────────────────────────────────────────────

type SessionIndex struct {
	Version  int           `json:"version"`
	Sessions []SessionMeta `json:"sessions"`
}

func (s *Store) loadIndex() (*SessionIndex, error) {
	path := filepath.Join(s.dir, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionIndex{Version: 1}, nil
		}
		return nil, err
	}
	var idx SessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func (s *Store) saveIndex(idx *SessionIndex) error {
	path := filepath.Join(s.dir, "index.json")
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ── CRUD ────────────────────────────────────────────────────────

// Save persists a session with its metadata and message history.
func (s *Store) Save(meta SessionMeta, history []*schema.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := SerializeSession(meta, history)
	if err != nil {
		return err
	}

	filePath := filepath.Join(s.dir, sessionFileName(meta.ID))
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return err
	}

	// Update index
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}

	found := false
	for i, sess := range idx.Sessions {
		if sess.ID == meta.ID {
			idx.Sessions[i] = meta
			found = true
			break
		}
	}
	if !found {
		idx.Sessions = append(idx.Sessions, meta)
	}

	return s.saveIndex(idx)
}

// Load retrieves a session and its metadata by ID.
func (s *Store) Load(id string) (*SessionMeta, []*schema.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := filepath.Join(s.dir, sessionFileName(id))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("load session %s: %w", id, err)
	}
	return DeserializeSession(data)
}

// List returns all session metadata from the index.
func (s *Store) List() ([]SessionMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	return idx.Sessions, nil
}

// Delete removes a session file and its index entry.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := filepath.Join(s.dir, sessionFileName(id))
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	for i, sess := range idx.Sessions {
		if sess.ID == id {
			idx.Sessions = append(idx.Sessions[:i], idx.Sessions[i+1:]...)
			break
		}
	}
	return s.saveIndex(idx)
}

// ── Helpers ─────────────────────────────────────────────────────

func sessionFileName(id string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '.' {
			return '_'
		}
		return r
	}, id)
	return clean + ".json"
}
