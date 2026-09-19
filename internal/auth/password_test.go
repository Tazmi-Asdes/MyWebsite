package auth

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	password := "correct horse battery staple"
	hash, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{0x2a}, argon2SaltLength)))
	if err != nil {
		t.Fatalf("HashPasswordWithRandom() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("hash prefix = %q", hash)
	}
	if err := VerifyPassword(password, hash); err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if err := VerifyPassword(hash, password); err != nil {
		t.Fatalf("hash-first VerifyPassword() error = %v", err)
	}
	if !errors.Is(VerifyPassword("wrong password", hash), ErrPasswordMismatch) {
		t.Fatalf("wrong password did not return ErrPasswordMismatch")
	}
	if !errors.Is(VerifyPassword(password, hash+"x"), ErrInvalidPasswordHash) {
		t.Fatalf("malformed hash did not return ErrInvalidPasswordHash")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     error
	}{
		{name: "short", password: "short", want: ErrPasswordTooShort},
		{name: "leading whitespace", password: " password-long", want: ErrPasswordWhitespace},
		{name: "trailing whitespace", password: "password-long ", want: ErrPasswordWhitespace},
		{name: "too many runes", password: strings.Repeat("x", 129), want: ErrPasswordTooLong},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidatePassword(test.password); !errors.Is(err, test.want) {
				t.Fatalf("ValidatePassword() error = %v, want %v", err, test.want)
			}
		})
	}
}
