package secret

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileKeepsTokensAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	f := NewFile(path)
	if _, err := f.Get(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before the file exists: %v", err)
	}
	if err := f.Delete(1); err != nil {
		t.Fatalf("deleting a missing token: %v", err)
	}
	if err := f.Set(1, "a"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(2, "b"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(1); err != nil {
		t.Fatal(err)
	}
	g := NewFile(path)
	if _, err := g.Get(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted token: %v", err)
	}
	if v, err := g.Get(2); v != "b" || err != nil {
		t.Fatalf("%q, %v", v, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := fi.Mode().Perm(); mode != 0o600 {
			t.Fatalf("mode = %o", mode)
		}
	}
	if left, _ := filepath.Glob(path + ".*"); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestFileMovesACorruptFileAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	os.WriteFile(path+".corrupt", []byte("older"), 0o600)
	os.WriteFile(path, []byte("{"), 0o600)
	f := NewFile(path)
	if _, err := f.Get(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if b, err := os.ReadFile(path + ".corrupt"); err != nil || string(b) != "{" {
		t.Fatalf("moved aside: %q, %v", b, err)
	}
	if err := f.Set(1, "a"); err != nil {
		t.Fatal(err)
	}
	if v, err := NewFile(path).Get(1); v != "a" || err != nil {
		t.Fatalf("%q, %v", v, err)
	}
	if err := f.Delete(1); err != nil {
		t.Fatal(err)
	}
}

func TestFileReturnsReadErrors(t *testing.T) {
	// A directory in its place fails to read, as a locked file would.
	path := filepath.Join(t.TempDir(), "tokens.json")
	os.Mkdir(path, 0o700)
	f := NewFile(path)
	if _, err := f.Get(1); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
	if err := f.Set(1, "a"); err == nil {
		t.Fatal("Set ignored the read error")
	}
	if _, err := os.Stat(path + ".corrupt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file that could not be read was moved aside: %v", err)
	}
}
