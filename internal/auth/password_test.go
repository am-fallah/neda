package auth

import (
	"strings"
	"testing"
)

const testPassword = "test-password-123"

var testParams = Params{
	Memory:      8 * 1024,
	Iterations:  1,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"valid", testPassword, false},
		{"eleven characters", "abcdefghijk", true},
		{"twelve characters", "abcdefghijkl", false},
		{"whitespace only", strings.Repeat(" ", 12), true},
		{"1024 bytes", strings.Repeat("a", 1024), false},
		{"1025 bytes", strings.Repeat("a", 1025), true},
		{"twelve multibyte runes", strings.Repeat("پ", 12), false},
		{"eleven multibyte runes", strings.Repeat("پ", 11), true},
		{"invalid utf8", string([]byte{0xff, 0xfe}), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePassword(test.password)
			if test.wantErr && err == nil {
				t.Fatal("ValidatePassword succeeded, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ValidatePassword: %v", err)
			}
		})
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := HashPassword(testPassword, testParams)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("hash = %q, want encoded Argon2id parameters", encoded)
	}
	ok, err := VerifyPassword(testPassword, encoded)
	if err != nil {
		t.Fatalf("VerifyPassword correct: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword correct = false")
	}
	ok, err = VerifyPassword("wrong-password-123", encoded)
	if err != nil {
		t.Fatalf("VerifyPassword wrong: %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword wrong = true")
	}

	// Verification uses the parameters encoded in the hash, not current defaults.
	ok, err = VerifyPassword(testPassword, encoded)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword with stored params = %v, %v", ok, err)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	malformed := []string{
		"",
		"plaintext",
		"$argon2id$v=19$m=8192,t=1,p=1$bad$bad",
		"$argon2i$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh",
		"$argon2id$v=18$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh",
		"$argon2id$v=19$m=8192,t=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh",
		"$argon2id$v=19$m=8192,t=1,p=1,m=8192$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh",
		"$argon2id$v=19$m=8192,t=1,p=257$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh",
		"$argon2id$v=19$m=999999999,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh",
		"$argon2id$v=19$m=8192,t=1,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$AAAA",
	}
	for _, encoded := range malformed {
		t.Run(encoded, func(t *testing.T) {
			if _, err := VerifyPassword(testPassword, encoded); err == nil {
				t.Fatal("VerifyPassword accepted malformed hash")
			}
		})
	}
}

func TestHashPasswordRejectsInvalidParams(t *testing.T) {
	tests := []struct {
		name   string
		params Params
	}{
		{"low memory", Params{Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}},
		{"zero iterations", Params{Memory: 8192, Iterations: 0, Parallelism: 1, SaltLength: 16, KeyLength: 32}},
		{"zero parallelism", Params{Memory: 8192, Iterations: 1, Parallelism: 0, SaltLength: 16, KeyLength: 32}},
		{"short salt", Params{Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 32}},
		{"short key", Params{Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 8}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := HashPassword(testPassword, test.params); err == nil {
				t.Fatal("HashPassword accepted invalid params")
			}
		})
	}
}

func BenchmarkHashPasswordDefault(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := HashPassword(testPassword, DefaultParams); err != nil {
			b.Fatal(err)
		}
	}
}
