package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

var ErrNotFound = errors.New("secret: not found")

type Tokens interface {
	Get(serverID int64) (string, error)
	Set(serverID int64, token string) error
	Delete(serverID int64) error
}

// File stores tokens in a JSON file only its owner can read. The OS keyring
// is not used: unsigned builds change their code signature on every update,
// and macOS then asks again for access to the keychain.
type File struct {
	path string
	mu   sync.Mutex
}

// NewFile stores tokens in path, which need not exist yet.
func NewFile(path string) *File { return &File{path: path} }

func (f *File) load() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(f.path), err)
	}
	return m, nil
}

// save replaces the file through a rename, so a crash leaves the old one.
func (f *File) save(m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), filepath.Base(f.path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

func key(id int64) string { return strconv.FormatInt(id, 10) }

func (f *File) Get(id int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := m[key(id)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *File) Set(id int64, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key(id)] = token
	return f.save(m)
}

func (f *File) Delete(id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[key(id)]; !ok {
		return nil
	}
	delete(m, key(id))
	return f.save(m)
}

type Memory struct {
	mu sync.Mutex
	m  map[int64]string
}

func (s *Memory) Get(id int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Memory) Set(id int64, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[int64]string{}
	}
	s.m[id] = token
	return nil
}

func (s *Memory) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}
