package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T, fill byte) *Key {
	t.Helper()
	material := make([]byte, KeySize)
	for i := range material {
		material[i] = fill
	}
	key, err := NewKey(material)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return key
}

func TestKeyRoundTripAndNonceFreshness(t *testing.T) {
	key := testKey(t, 7)
	first, err := key.Encrypt("telegram.bot_token", []byte("123:secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := key.Encrypt("telegram.bot_token", []byte("123:secret"))
	if err != nil {
		t.Fatalf("Encrypt second: %v", err)
	}
	if first == second {
		t.Fatal("two encryptions produced the same value; nonce was not fresh")
	}
	if !strings.HasPrefix(first, "v1:") {
		t.Fatalf("value prefix = %q, want v1:", first)
	}
	plaintext, err := key.Decrypt("telegram.bot_token", first)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plaintext) != "123:secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestKeyRejectsTamperWrongKeyAndWrongSetting(t *testing.T) {
	key := testKey(t, 1)
	encrypted, err := key.Encrypt("telegram.bot_token", []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	wrongKey := testKey(t, 2)
	if _, err := wrongKey.Decrypt("telegram.bot_token", encrypted); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong-key error = %v, want ErrAuthentication", err)
	}
	if _, err := key.Decrypt("telegram.chat_id", encrypted); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong-setting error = %v, want ErrAuthentication", err)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encrypted, "v1:"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		tampered := append([]byte(nil), raw...)
		tampered[i] ^= 0x01
		value := "v1:" + base64.StdEncoding.EncodeToString(tampered)
		if _, err := key.Decrypt("telegram.bot_token", value); err == nil {
			t.Fatalf("tampered byte %d decrypted successfully", i)
		}
	}
}

func TestKeyRejectsMalformedValues(t *testing.T) {
	key := testKey(t, 3)
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"empty", "", ErrMalformedValue},
		{"no-prefix", "abc", ErrMalformedValue},
		{"unsupported-version", "v2:abcdef", ErrUnsupportedVersion},
		{"bad-base64", "v1:!!!", ErrMalformedValue},
		{"short", "v1:AAAA", ErrMalformedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := key.Decrypt("telegram.bot_token", tc.value); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestKeyValidationAndRedaction(t *testing.T) {
	if _, err := NewKey([]byte("short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("NewKey(short) error = %v", err)
	}
	var zeroKey Key
	if _, err := zeroKey.Encrypt("k", []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("zero-value Encrypt error = %v", err)
	}
	if _, err := zeroKey.Decrypt("k", "v1:AAAA"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("zero-value Decrypt error = %v", err)
	}
	if zeroKey.Version() != 0 {
		t.Fatalf("zero-value Version = %d", zeroKey.Version())
	}
	var nilKey *Key
	if _, err := nilKey.Encrypt("k", []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("nil Encrypt error = %v", err)
	}
	if _, err := nilKey.Decrypt("k", "v1:AAAA"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("nil Decrypt error = %v", err)
	}

	key := testKey(t, 9)
	if _, err := key.Encrypt("", []byte("v")); err == nil {
		t.Fatal("Encrypt accepted an empty setting key")
	}
	if _, err := key.Encrypt("bad\x00key", []byte("v")); err == nil {
		t.Fatal("Encrypt accepted a control character")
	}

	rendered := fmt.Sprintf("%v %+v %#v %x %s | value=%v", key, key, key, key, key, *key)
	if strings.Contains(rendered, "09") || !strings.Contains(rendered, "redacted") {
		t.Fatalf("key rendering leaked or was not redacted: %q", rendered)
	}
	encoded, err := key.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "0909") {
		t.Fatalf("JSON leaked key material: %s", encoded)
	}
}

func writeKeyFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "encryption.key")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod key file: %v", err)
	}
	return path
}

func validKeyContents() string {
	material := make([]byte, KeySize)
	for i := range material {
		material[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(material) + "\n"
}

func TestLoadKeyFile(t *testing.T) {
	for _, mode := range []os.FileMode{0o400, 0o600} {
		t.Run(fmt.Sprintf("mode-%o", mode), func(t *testing.T) {
			path := writeKeyFile(t, validKeyContents(), mode)
			key, err := LoadKeyFile(path)
			if err != nil {
				t.Fatalf("LoadKeyFile: %v", err)
			}
			encrypted, err := key.Encrypt("telegram.bot_token", []byte("secret"))
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if _, err := key.Decrypt("telegram.bot_token", encrypted); err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
		})
	}
}

func TestLoadKeyFileRejectsUnsafeFiles(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		mode     os.FileMode
		want     string
	}{
		{"group-readable", validKeyContents(), 0o640, "owner-only"},
		{"world-readable", validKeyContents(), 0o604, "owner-only"},
		{"owner-executable", validKeyContents(), 0o700, "owner-only"},
		{"empty", "", 0o600, "empty"},
		{"invalid-base64", "not-base64!", 0o600, "base64"},
		{"short-key", base64.StdEncoding.EncodeToString([]byte("short")), 0o600, "exactly 32 bytes"},
		{"too-large", strings.Repeat("A", keyFileMaxBytes+1), 0o600, "too large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeKeyFile(t, tc.contents, tc.mode)
			_, err := LoadKeyFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadKeyFileRejectsSymlinkDirectoryAndMissing(t *testing.T) {
	target := writeKeyFile(t, validKeyContents(), 0o600)
	link := filepath.Join(t.TempDir(), "link.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink error = %v", err)
	}
	if _, err := LoadKeyFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
	if _, err := LoadKeyFile(filepath.Join(t.TempDir(), "missing.key")); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing error = %v", err)
	}
	if _, err := LoadKeyFile(""); err == nil {
		t.Fatal("empty path accepted")
	}
}

func TestLoadKeyFileErrorsDoNotLeakKey(t *testing.T) {
	contents := strings.Repeat("s", keyFileMaxBytes)
	path := writeKeyFile(t, contents, 0o600)
	_, err := LoadKeyFile(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), contents) {
		t.Fatal("error leaked file contents")
	}
}

func TestValidateKeyFileDoesNotRetainKey(t *testing.T) {
	path := writeKeyFile(t, validKeyContents(), 0o600)
	if err := ValidateKeyFile(path); err != nil {
		t.Fatalf("ValidateKeyFile: %v", err)
	}
}
