package secret

import (
	"errors"
	"testing"
	"time"
)

func TestBoundedGivesUpOnACallThatDoesNotReturn(t *testing.T) {
	defer func(d time.Duration) { timeout = d }(timeout)
	timeout = 20 * time.Millisecond
	block := make(chan struct{})
	defer close(block)
	if _, err := bounded(func() (string, error) { <-block; return "late", nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if v, err := bounded(func() (string, error) { return "tok", nil }); v != "tok" || err != nil {
		t.Fatalf("%q, %v", v, err)
	}
}
