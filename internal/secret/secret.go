package secret

import (
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

const service = "gotify-desktop"

var ErrNotFound = errors.New("secret: not found")

type Tokens interface {
	Get(serverID int64) (string, error)
	Set(serverID int64, token string) error
	Delete(serverID int64) error
}

type keyringTokens struct{}

// Keyring stores tokens in the OS credential store.
func Keyring() Tokens { return keyringTokens{} }

func user(id int64) string { return fmt.Sprintf("server-%d", id) }

func (keyringTokens) Get(id int64) (string, error) {
	v, err := keyring.Get(service, user(id))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (keyringTokens) Set(id int64, token string) error { return keyring.Set(service, user(id), token) }

func (keyringTokens) Delete(id int64) error {
	err := keyring.Delete(service, user(id))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
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
