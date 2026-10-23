package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/store"
)

const cliTestPassword = "test-password-123"

type fakePrompter struct {
	terminal bool
	values   []string
	err      error
	index    int
}

func (p *fakePrompter) IsTerminal() bool { return p.terminal }

func (p *fakePrompter) ReadPassword() (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if p.index >= len(p.values) {
		return "", errors.New("no more test passwords")
	}
	value := p.values[p.index]
	p.index++
	return value, nil
}

func openCLIStore(t *testing.T, path string) *store.Store {
	t.Helper()
	database, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("database.Close: %v", err)
		}
	})
	return store.New(database.DB)
}

func TestRunSetPasswordRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSetPassword([]string{"secret"}, &stdout, &stderr, &fakePrompter{terminal: true})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "does not accept arguments") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSetPasswordRejectsNonTTY(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSetPassword(nil, &stdout, &stderr, &fakePrompter{terminal: false})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "interactive terminal") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSetPasswordMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mismatch.db")
	t.Setenv("NEDA_DB_PATH", path)
	var stdout, stderr bytes.Buffer
	prompter := &fakePrompter{terminal: true, values: []string{cliTestPassword, "test-password-124"}}
	code := runSetPassword(nil, &stdout, &stderr, prompter)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "passwords do not match") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := openCLIStore(t, path).GetUser(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUser after mismatch = %v, want ErrNotFound", err)
	}
}

func TestRunSetPasswordShortPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short.db")
	t.Setenv("NEDA_DB_PATH", path)
	var stdout, stderr bytes.Buffer
	prompter := &fakePrompter{terminal: true, values: []string{"short", "short"}}
	code := runSetPassword(nil, &stdout, &stderr, prompter)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "at least 12") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := openCLIStore(t, path).GetUser(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUser after short password = %v, want ErrNotFound", err)
	}
}

func TestRunSetPasswordSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "success.db")
	t.Setenv("NEDA_DB_PATH", path)
	var stdout, stderr bytes.Buffer
	prompter := &fakePrompter{terminal: true, values: []string{cliTestPassword, cliTestPassword}}
	code := runSetPassword(nil, &stdout, &stderr, prompter)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Password set") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), cliTestPassword) || strings.Contains(stderr.String(), cliTestPassword) {
		t.Fatal("password was printed")
	}

	user, err := openCLIStore(t, path).GetUser(context.Background())
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !strings.HasPrefix(user.PasswordHash, "$argon2id$v=19$") {
		t.Fatalf("password hash = %q, want argon2id PHC", user.PasswordHash)
	}
}

func TestRunSetPasswordReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "read-error.db")
	t.Setenv("NEDA_DB_PATH", path)
	var stdout, stderr bytes.Buffer
	prompter := &fakePrompter{terminal: true, err: errors.New("read failed")}
	code := runSetPassword(nil, &stdout, &stderr, prompter)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "read password") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunHelpListsSetPassword(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(help) = %d", code)
	}
	if !strings.Contains(stdout.String(), "set-password") {
		t.Fatalf("help = %q", stdout.String())
	}
}

func TestRunRejectsSetPasswordArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"set-password", "secret"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(set-password secret) = %d, want 2", code)
	}
}
