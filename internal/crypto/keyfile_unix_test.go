//go:build unix

package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckKeyFileOwnerRejectsDifferentUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkKeyFileOwner(info, os.Geteuid()); err != nil {
		t.Fatalf("current owner rejected: %v", err)
	}
	if err := checkKeyFileOwner(info, os.Geteuid()+1); err == nil || !strings.Contains(err.Error(), "owned by the runtime user") {
		t.Fatalf("different owner error = %v", err)
	}
}
