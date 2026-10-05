package secret

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const service = "gotify-desktop"

var ErrNotFound = errors.New("secret: not found")

// ErrUnavailable is returned when the OS credential store does not answer in
// time, as a Secret Service that D-Bus cannot start never does.
var ErrUnavailable = errors.New("the system keyring did not answer")

// timeout bounds every call to the OS credential store.
var timeout = 15 * time.Second

// bounded runs f, giving up after timeout. A call that never returns keeps its goroutine.
func bounded[T any](f func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := f()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(timeout):
		var zero T
		return zero, ErrUnavailable
	}
}

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
	v, err := bounded(func() (string, error) { return keyring.Get(service, user(id)) })
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (keyringTokens) Set(id int64, token string) error {
	_, err := bounded(func() (struct{}, error) { return struct{}{}, keyring.Set(service, user(id), token) })
	return err
}

func (keyringTokens) Delete(id int64) error {
	_, err := bounded(func() (struct{}, error) { return struct{}{}, keyring.Delete(service, user(id)) })
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
