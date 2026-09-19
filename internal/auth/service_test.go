package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"mywebsite/internal/database/dbgen"
)

type fakeStore struct {
	adminCount       int64
	admin            dbgen.Admin
	adminErr         error
	countErr         error
	createAdminErr   error
	createSessionErr error
	touchErr         error
	revokeErr        error
	sessionResult    sql.Result
	touchResult      sql.Result
	revokeResult     sql.Result
	session          dbgen.GetAdminSessionByTokenHashRow
	createAdmin      dbgen.CreateAdminParams
	createSession    dbgen.CreateAdminSessionParams
	touchSession     dbgen.TouchAdminSessionParams
	revokeSession    dbgen.RevokeAdminSessionParams
	operations       []string
	nextID           int64
}

func (store *fakeStore) CountAdmins(context.Context) (int64, error) {
	store.operations = append(store.operations, "count")
	return store.adminCount, store.countErr
}

func (store *fakeStore) CreateAdmin(_ context.Context, params dbgen.CreateAdminParams) (sql.Result, error) {
	store.operations = append(store.operations, "create-admin")
	store.createAdmin = params
	return fakeResult{id: store.nextID, rows: 1}, store.createAdminErr
}

func (store *fakeStore) GetAdminByUsername(context.Context, string) (dbgen.Admin, error) {
	store.operations = append(store.operations, "get-admin")
	return store.admin, store.adminErr
}

func (store *fakeStore) CreateAdminSession(_ context.Context, params dbgen.CreateAdminSessionParams) (sql.Result, error) {
	store.operations = append(store.operations, "create-session")
	store.createSession = params
	result := store.sessionResult
	if result == nil {
		result = fakeResult{id: store.nextID, rows: 1}
	}
	return result, store.createSessionErr
}

func (store *fakeStore) GetAdminSessionByTokenHash(_ context.Context, tokenHash []byte) (dbgen.GetAdminSessionByTokenHashRow, error) {
	store.operations = append(store.operations, "get-session")
	if !bytes.Equal(tokenHash, store.session.TokenHash) {
		return dbgen.GetAdminSessionByTokenHashRow{}, sql.ErrNoRows
	}
	return store.session, nil
}

func (store *fakeStore) TouchAdminSession(_ context.Context, params dbgen.TouchAdminSessionParams) (sql.Result, error) {
	store.operations = append(store.operations, "touch")
	store.touchSession = params
	result := store.touchResult
	if result == nil {
		result = fakeResult{id: 1, rows: 1}
	}
	return result, store.touchErr
}

func (store *fakeStore) RevokeAdminSession(_ context.Context, params dbgen.RevokeAdminSessionParams) (sql.Result, error) {
	store.operations = append(store.operations, "revoke")
	store.revokeSession = params
	result := store.revokeResult
	if result == nil {
		result = fakeResult{id: 1, rows: 1}
	}
	return result, store.revokeErr
}

type fakeResult struct {
	id            int64
	rows          int64
	lastInsertErr error
	rowsErr       error
}

func (result fakeResult) LastInsertId() (int64, error) { return result.id, result.lastInsertErr }
func (result fakeResult) RowsAffected() (int64, error) { return result.rows, result.rowsErr }

func TestCreateAdminIsUniqueAndStoresUTCArgon2id(t *testing.T) {
	store := &fakeStore{nextID: 7}
	now := time.Date(2026, 9, 19, 10, 11, 12, 123456000, time.FixedZone("test", 8*60*60))
	service := NewService(store,
		WithClock(func() time.Time { return now }),
		WithRandomReader(bytes.NewReader(bytes.Repeat([]byte{0x10}, argon2SaltLength))),
	)
	if err := service.CreateAdmin(context.Background(), "admin user", "a sufficiently long password"); err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	if len(store.operations) != 2 || store.operations[0] != "count" || store.operations[1] != "create-admin" {
		t.Fatalf("operations = %v, want count then create-admin", store.operations)
	}
	if !store.createAdmin.CreatedAt.Equal(now.UTC()) || !store.createAdmin.UpdatedAt.Equal(now.UTC()) {
		t.Fatalf("timestamps = %v, %v; want UTC %v", store.createAdmin.CreatedAt, store.createAdmin.UpdatedAt, now.UTC())
	}
	if err := VerifyPassword("a sufficiently long password", store.createAdmin.PasswordHash); err != nil {
		t.Fatalf("stored hash does not verify: %v", err)
	}

	store.adminCount = 1
	if err := service.CreateAdmin(context.Background(), "another", "a sufficiently long password"); !errors.Is(err, ErrAdminAlreadyExists) {
		t.Fatalf("second CreateAdmin() error = %v, want ErrAdminAlreadyExists", err)
	}
	if len(store.operations) != 3 || store.operations[2] != "count" {
		t.Fatalf("second create operations = %v", store.operations)
	}
}

func TestLoginStoresOnlyTokenAndCSRFHashesAndAuthenticates(t *testing.T) {
	password := "a sufficiently long password"
	passwordHash, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{1}, argon2SaltLength)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 2, 3, 4, 0, time.FixedZone("local", 8*60*60))
	store := &fakeStore{
		admin:  dbgen.Admin{ID: 42, Username: "admin user", PasswordHash: passwordHash},
		nextID: 99,
	}
	service := NewService(store,
		WithClock(func() time.Time { return now }),
		WithSessionTimeouts(8*time.Hour, 24*time.Hour),
		WithRandomReader(bytes.NewReader(append(bytes.Repeat([]byte{0x22}, 32), bytes.Repeat([]byte{0x23}, 32)...))),
	)
	login, err := service.Login(context.Background(), "admin user", password)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if login.Admin.Username != "admin user" || login.Admin.ID != 42 || login.SessionID != 99 {
		t.Fatalf("login admin/session = %+v", login)
	}
	if len(store.createSession.TokenHash) != sha256.Size || len(store.createSession.CsrfHash) != sha256.Size {
		t.Fatalf("stored digest lengths = %d/%d", len(store.createSession.TokenHash), len(store.createSession.CsrfHash))
	}
	rawToken, _ := base64.RawURLEncoding.DecodeString(login.Token)
	rawCSRF, _ := base64.RawURLEncoding.DecodeString(login.CSRFToken)
	tokenHash := sha256.Sum256(rawToken)
	csrfHash := sha256.Sum256(rawCSRF)
	if !bytes.Equal(store.createSession.TokenHash, tokenHash[:]) || !bytes.Equal(store.createSession.CsrfHash, csrfHash[:]) {
		t.Fatal("database did not receive SHA-256 token and CSRF digests")
	}
	if bytes.Contains(store.createSession.TokenHash, rawToken) || bytes.Contains(store.createSession.CsrfHash, rawCSRF) {
		t.Fatal("database digest unexpectedly contains client token material")
	}

	store.session = dbgen.GetAdminSessionByTokenHashRow{
		ID:                99,
		TokenHash:         append([]byte(nil), tokenHash[:]...),
		AdminID:           42,
		CsrfHash:          append([]byte(nil), csrfHash[:]...),
		LastSeenAt:        now.UTC(),
		AbsoluteExpiresAt: now.UTC().Add(24 * time.Hour),
		Username:          "admin user",
	}
	authenticated, err := service.Authenticate(context.Background(), login.Token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if authenticated.ID != 99 || authenticated.AdminID != 42 || authenticated.Username != "admin user" {
		t.Fatalf("authenticated session = %+v", authenticated)
	}
	if err := service.ValidateCSRF(authenticated, login.CSRFToken); err != nil {
		t.Fatalf("ValidateCSRF() error = %v", err)
	}
	if !errors.Is(service.ValidateCSRF(authenticated, login.Token), ErrCSRFInvalid) {
		t.Fatal("wrong CSRF token accepted")
	}
	if err := service.Logout(context.Background(), authenticated.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if !store.revokeSession.RevokedAt.Valid || !store.revokeSession.RevokedAt.Time.Equal(now.UTC()) {
		t.Fatalf("revoke timestamp = %+v", store.revokeSession.RevokedAt)
	}
}

func TestAuthenticateIdleAndAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	serviceNow := now
	tokenRaw := bytes.Repeat([]byte{7}, tokenBytes)
	token := base64.RawURLEncoding.EncodeToString(tokenRaw)
	tokenHash := sha256.Sum256(tokenRaw)
	store := &fakeStore{session: dbgen.GetAdminSessionByTokenHashRow{
		ID: 1, TokenHash: tokenHash[:], CsrfHash: bytes.Repeat([]byte{8}, sha256.Size), AdminID: 2,
		Username: "admin user", LastSeenAt: now.Add(-9 * time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}}
	service := NewService(store, WithClock(func() time.Time { return serviceNow }), WithSessionTimeouts(8*time.Hour, 24*time.Hour))
	if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("idle-expired Authenticate() error = %v", err)
	}
	serviceNow = now.Add(25 * time.Hour)
	store.session.LastSeenAt = serviceNow.Add(-time.Hour)
	if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("absolute-expired Authenticate() error = %v", err)
	}
}

func TestReauthenticateCreatesThenRevokesAndDoesNotRetryOnRevokeFailure(t *testing.T) {
	password := "a sufficiently long password"
	hash, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{3}, argon2SaltLength)))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{admin: dbgen.Admin{ID: 4, Username: "admin user", PasswordHash: hash}, nextID: 8, revokeErr: errors.New("revoke failed")}
	service := NewService(store, WithRandomReader(bytes.NewReader(bytes.Repeat([]byte{4}, 64))))
	_, err = service.Reauthenticate(context.Background(), Session{ID: 7, Username: "admin user"}, password)
	if !errors.Is(err, ErrStore) {
		t.Fatalf("Reauthenticate() error = %v, want ErrStore", err)
	}
	if got := fmt.Sprint(store.operations); got != "[get-admin create-session revoke]" {
		t.Fatalf("operations = %s", got)
	}
}

func TestAuthenticateTouchRowsMustBeExactlyOne(t *testing.T) {
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	tokenRaw := bytes.Repeat([]byte{9}, tokenBytes)
	token := base64.RawURLEncoding.EncodeToString(tokenRaw)
	tokenHash := sha256.Sum256(tokenRaw)
	for _, test := range []struct {
		name      string
		result    sql.Result
		wantError error
	}{
		{name: "zero rows", result: fakeResult{rows: 0}, wantError: ErrSessionRevoked},
		{name: "multiple rows", result: fakeResult{rows: 2}, wantError: ErrStore},
		{name: "rows error", result: fakeResult{rowsErr: errors.New("rows failed")}, wantError: ErrStore},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{session: dbgen.GetAdminSessionByTokenHashRow{
				ID: 1, TokenHash: tokenHash[:], CsrfHash: bytes.Repeat([]byte{8}, sha256.Size), AdminID: 2,
				Username: "admin user", LastSeenAt: now, AbsoluteExpiresAt: now.Add(24 * time.Hour),
			}, touchResult: test.result}
			service := NewService(store, WithClock(func() time.Time { return now }))
			if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, test.wantError) {
				t.Fatalf("Authenticate() error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func TestReauthenticateRevokeZeroRowsReturnsInvalidSessionAfterCreatingNewSession(t *testing.T) {
	password := "a sufficiently long password"
	hash, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{5}, argon2SaltLength)))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		admin:        dbgen.Admin{ID: 4, Username: "admin user", PasswordHash: hash},
		nextID:       8,
		revokeResult: fakeResult{rows: 0},
	}
	service := NewService(store, WithRandomReader(bytes.NewReader(bytes.Repeat([]byte{6}, 64))))
	_, err = service.Reauthenticate(context.Background(), Session{ID: 7, Username: "admin user"}, password)
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("Reauthenticate() error = %v, want ErrInvalidSession", err)
	}
	if got := fmt.Sprint(store.operations); got != "[get-admin create-session revoke]" {
		t.Fatalf("operations = %s, want create before revoke", got)
	}
}

func TestLoginLastInsertIDFailureReturnsStoreError(t *testing.T) {
	password := "a sufficiently long password"
	hash, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{7}, argon2SaltLength)))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		admin:         dbgen.Admin{ID: 4, Username: "admin user", PasswordHash: hash},
		nextID:        8,
		sessionResult: fakeResult{id: 8, rows: 1, lastInsertErr: errors.New("insert id unavailable")},
	}
	service := NewService(store, WithRandomReader(bytes.NewReader(bytes.Repeat([]byte{8}, 64))))
	if _, err := service.Login(context.Background(), "admin user", password); !errors.Is(err, ErrStore) {
		t.Fatalf("Login() error = %v, want ErrStore", err)
	}
}
