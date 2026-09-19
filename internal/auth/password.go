package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Memory      uint32 = 64 * 1024
	argon2Iterations  uint32 = 3
	argon2Parallelism uint8  = 2
	argon2SaltLength         = 16
	argon2KeyLength          = 32
	argon2Version            = 19
	passwordMinRunes         = 12
	passwordMaxRunes         = 128
	passwordMaxBytes         = 1024
)

const (
	Argon2Memory      = argon2Memory
	Argon2Iterations  = argon2Iterations
	Argon2Parallelism = argon2Parallelism
	Argon2SaltLength  = argon2SaltLength
	Argon2KeyLength   = argon2KeyLength
)

var (
	// ErrInvalidPassword is returned when a password does not satisfy the
	// domain length and boundary rules.
	ErrInvalidPassword = errors.New("invalid password")
	// ErrPasswordTooShort and ErrPasswordTooLong are useful for callers that
	// need to distinguish validation failures without exposing the password.
	ErrPasswordTooShort = errors.New("password too short")
	ErrPasswordTooLong  = errors.New("password too long")
	// ErrPasswordWhitespace reports leading or trailing Unicode whitespace.
	ErrPasswordWhitespace = errors.New("password has leading or trailing whitespace")
	// ErrPasswordBytes reports a password whose UTF-8 representation is above
	// the input limit used before Argon2id is invoked.
	ErrPasswordBytes = errors.New("password is too many bytes")
	// ErrInvalidPasswordHash reports an encoded hash that is not the exact
	// Argon2id format produced by this package.
	ErrInvalidPasswordHash = errors.New("invalid password hash")
	// ErrPasswordMismatch is returned for a valid hash that does not match.
	ErrPasswordMismatch = errors.New("password mismatch")
)

// PasswordHasher hashes passwords with Argon2id. Random is injectable so
// deterministic readers can be used by unit tests; production uses crypto/rand.
type PasswordHasher struct {
	random io.Reader
}

// NewPasswordHasher constructs a hasher. A nil reader means crypto/rand.Reader.
func NewPasswordHasher(randomReader io.Reader) *PasswordHasher {
	if randomReader == nil {
		randomReader = rand.Reader
	}
	return &PasswordHasher{random: randomReader}
}

// HashPassword hashes a password using crypto/rand.Reader.
func HashPassword(password string) (string, error) {
	return NewPasswordHasher(nil).Hash(password)
}

// Hash is a concise alias for HashPassword.
func Hash(password string) (string, error) { return HashPassword(password) }

// HashPasswordWithRandom is the injectable form of HashPassword.
func HashPasswordWithRandom(password string, randomReader io.Reader) (string, error) {
	return NewPasswordHasher(randomReader).Hash(password)
}

// Hash validates and encodes a password as an Argon2id PHC string.
func (h *PasswordHasher) Hash(password string) (string, error) {
	if h == nil {
		return "", ErrInvalidPassword
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}

	salt := make([]byte, argon2SaltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version,
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify is the method form of VerifyPassword.
func (h *PasswordHasher) Verify(password, encodedHash string) error {
	return VerifyPassword(password, encodedHash)
}

// VerifyPassword validates an encoded Argon2id hash and compares the derived
// key using constant-time comparison. The arguments are accepted in either
// (password, encodedHash) or (encodedHash, password) order to keep the helper
// convenient at call sites; the encoded value is detected by its PHC prefix.
func VerifyPassword(first, second string) error {
	password, encodedHash := first, second
	if strings.HasPrefix(first, "$argon2id$") && !strings.HasPrefix(second, "$argon2id$") {
		password, encodedHash = second, first
	}
	parsed, err := parsePasswordHash(encodedHash)
	if err != nil && strings.HasPrefix(first, "$argon2id$") && strings.HasPrefix(second, "$argon2id$") {
		// A valid password is allowed to begin with the PHC prefix. If the
		// first argument is not a parseable hash, retry the explicit
		// password-first interpretation before reporting a malformed hash.
		if alternate, alternateErr := parsePasswordHash(second); alternateErr == nil {
			password, encodedHash, parsed, err = first, second, alternate, nil
		}
	}
	if err != nil {
		return err
	}
	key := argon2.IDKey([]byte(password), parsed.salt, parsed.iterations, parsed.memory, parsed.parallelism, uint32(len(parsed.key)))
	if subtle.ConstantTimeCompare(key, parsed.key) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

// VerifyPasswordHash is an explicit hash-first alias for VerifyPassword.
func VerifyPasswordHash(encodedHash, password string) error {
	return VerifyPassword(password, encodedHash)
}

// Verify is a concise alias for VerifyPassword.
func Verify(password, encodedHash string) error {
	return VerifyPassword(password, encodedHash)
}

// CheckPassword reports whether password matches encodedHash. It intentionally
// collapses malformed hashes and mismatches into false for authentication use.
func CheckPassword(password, encodedHash string) bool {
	return VerifyPassword(password, encodedHash) == nil
}

// ValidatePassword applies the domain password rules without hashing it.
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrInvalidPassword
	}
	runeCount := utf8.RuneCountInString(password)
	if runeCount < passwordMinRunes {
		return fmt.Errorf("%w: %w", ErrInvalidPassword, ErrPasswordTooShort)
	}
	if runeCount > passwordMaxRunes {
		return fmt.Errorf("%w: %w", ErrInvalidPassword, ErrPasswordTooLong)
	}
	if len(password) > passwordMaxBytes {
		return fmt.Errorf("%w: %w", ErrInvalidPassword, ErrPasswordBytes)
	}
	if len(password) > 0 {
		first, _ := utf8.DecodeRuneInString(password)
		last, _ := utf8.DecodeLastRuneInString(password)
		if isUnicodeWhitespace(first) || isUnicodeWhitespace(last) {
			return fmt.Errorf("%w: %w", ErrInvalidPassword, ErrPasswordWhitespace)
		}
	}
	return nil
}

func isUnicodeWhitespace(r rune) bool {
	// Avoid importing the broad unicode package solely for the small set of
	// Unicode whitespace classifications needed at the password boundaries.
	return strings.TrimSpace(string(r)) == ""
}

type parsedPasswordHash struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

func parsePasswordHash(encoded string) (parsedPasswordHash, error) {
	var parsed parsedPasswordHash
	if len(encoded) == 0 || len(encoded) > 255 {
		return parsed, ErrInvalidPasswordHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return parsed, ErrInvalidPasswordHash
	}
	params := map[string]string{}
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" || value == "" {
			return parsed, ErrInvalidPasswordHash
		}
		if _, exists := params[key]; exists {
			return parsed, ErrInvalidPasswordHash
		}
		params[key] = value
	}
	if len(params) != 3 || params["m"] == "" || params["t"] == "" || params["p"] == "" {
		return parsed, ErrInvalidPasswordHash
	}
	memory, errMemory := strconv.ParseUint(params["m"], 10, 32)
	iterations, errIterations := strconv.ParseUint(params["t"], 10, 32)
	parallelism, errParallelism := strconv.ParseUint(params["p"], 10, 8)
	if errMemory != nil || errIterations != nil || errParallelism != nil ||
		uint32(memory) != argon2Memory || uint32(iterations) != argon2Iterations || uint8(parallelism) != argon2Parallelism {
		return parsed, ErrInvalidPasswordHash
	}
	strictEncoding := base64.RawStdEncoding.Strict()
	salt, err := strictEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != argon2SaltLength {
		return parsed, ErrInvalidPasswordHash
	}
	key, err := strictEncoding.DecodeString(parts[5])
	if err != nil || len(key) != argon2KeyLength {
		return parsed, ErrInvalidPasswordHash
	}
	parsed.memory = uint32(memory)
	parsed.iterations = uint32(iterations)
	parsed.parallelism = uint8(parallelism)
	parsed.salt = salt
	parsed.key = key
	return parsed, nil
}
