package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"time"
	"unicode"
	"unicode/utf8"

	"mywebsite/internal/database/dbgen"
)

const (
	DefaultIdleTimeout     = 8 * time.Hour
	DefaultAbsoluteTimeout = 24 * time.Hour
	tokenBytes             = 32
)

var (
	ErrInvalidUsername        = errors.New("invalid username")
	ErrUsernameInvalid        = ErrInvalidUsername
	ErrAdminAlreadyExists     = errors.New("admin already exists")
	ErrAlreadyExists          = ErrAdminAlreadyExists
	ErrAdminExists            = ErrAdminAlreadyExists
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrCredentialsInvalid     = ErrInvalidCredentials
	ErrAuthenticationFailed   = ErrInvalidCredentials
	ErrStore                  = errors.New("authentication store error")
	ErrInvalidSession         = errors.New("invalid session")
	ErrSessionInvalid         = ErrInvalidSession
	ErrAuthenticationRequired = ErrInvalidSession
	ErrSessionRevoked         = errors.New("session revoked")
	ErrSessionExpired         = errors.New("session expired")
	ErrCSRFInvalid            = errors.New("invalid csrf token")
	ErrInvalidCSRF            = ErrCSRFInvalid
	ErrCSRF                   = ErrCSRFInvalid
)

// Store is the smallest database contract needed by the authentication
// service. The generated dbgen parameter and result types remain at this
// boundary so no extra SQL or repository methods are required.
type Store interface {
	CountAdmins(context.Context) (int64, error)
	CreateAdmin(context.Context, dbgen.CreateAdminParams) (sql.Result, error)
	GetAdminByUsername(context.Context, string) (dbgen.Admin, error)
	CreateAdminSession(context.Context, dbgen.CreateAdminSessionParams) (sql.Result, error)
	GetAdminSessionByTokenHash(context.Context, []byte) (dbgen.GetAdminSessionByTokenHashRow, error)
	TouchAdminSession(context.Context, dbgen.TouchAdminSessionParams) (sql.Result, error)
	RevokeAdminSession(context.Context, dbgen.RevokeAdminSessionParams) (sql.Result, error)
}

// Admin is the non-sensitive administrator view returned after authentication.
// It deliberately has no password hash field.
type Admin struct {
	ID        uint64
	Username  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LoginResult contains one newly-created session and its client tokens. Token
// values are returned only here; the Store receives their SHA-256 digests.
type LoginResult struct {
	Admin             Admin
	SessionID         uint64
	Token             string
	CSRFToken         string
	CSRF              string
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

// Session is the internal authenticated session context. CSRFHash contains the
// database digest and never the client-visible CSRF token.
type Session struct {
	ID                uint64
	AdminID           uint64
	Username          string
	CSRFHash          []byte
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

// AuthenticatedSession is a descriptive alias for Session.
type AuthenticatedSession = Session

// ServiceConfig configures authentication dependencies and session lifetime.
type ServiceConfig struct {
	Store           Store
	Random          io.Reader
	Now             func() time.Time
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
}

// Clock is the optional clock shape accepted by NewService.
type Clock interface {
	Now() time.Time
}

// Option customizes a Service. Options are intentionally small to keep fake
// stores and deterministic clocks straightforward in unit tests.
type Option func(*Service)

func WithRandom(randomReader io.Reader) Option {
	return func(service *Service) {
		if randomReader != nil {
			service.random = randomReader
		}
	}
}

// WithRandomReader is an explicit alias for WithRandom.
func WithRandomReader(randomReader io.Reader) Option { return WithRandom(randomReader) }

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

// WithNow is an alias for WithClock.
func WithNow(now func() time.Time) Option { return WithClock(now) }

func WithSessionTimeouts(idleTimeout, absoluteTimeout time.Duration) Option {
	return func(service *Service) {
		if idleTimeout > 0 {
			service.idleTimeout = idleTimeout
		}
		if absoluteTimeout > 0 {
			service.absoluteTimeout = absoluteTimeout
		}
	}
}

// WithTimeouts is an alias for WithSessionTimeouts.
func WithTimeouts(idleTimeout, absoluteTimeout time.Duration) Option {
	return WithSessionTimeouts(idleTimeout, absoluteTimeout)
}

// Service implements administrator creation, credential verification, and
// server-side sessions.
type Service struct {
	store           Store
	random          io.Reader
	now             func() time.Time
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
}

// AuthService is a descriptive alias for Service.
type AuthService = Service

// NewService constructs an authentication service. It accepts either Option
// values, time.Duration values (idle then absolute), or a ServiceConfig. The
// flexible form keeps the domain easy to use from small command and HTTP
// adapters while retaining the fixed default lifetimes.
func NewService(store Store, arguments ...any) *Service {
	service := &Service{
		store:           store,
		random:          rand.Reader,
		now:             time.Now,
		idleTimeout:     DefaultIdleTimeout,
		absoluteTimeout: DefaultAbsoluteTimeout,
	}
	service.applyArguments(arguments...)
	return service
}

// New is a concise constructor alias.
func New(store Store, arguments ...any) *Service { return NewService(store, arguments...) }

// NewAuthService is an explicit constructor alias.
func NewAuthService(store Store, arguments ...any) *Service {
	return NewService(store, arguments...)
}

// NewServiceWithTimeouts is the explicit constructor form for callers that
// want to document the two session lifetimes at the call site.
func NewServiceWithTimeouts(store Store, idleTimeout, absoluteTimeout time.Duration) *Service {
	return NewService(store, idleTimeout, absoluteTimeout)
}

// NewServiceWithConfig returns an error when the required store is absent.
func NewServiceWithConfig(config ServiceConfig) (*Service, error) {
	if config.Store == nil {
		return nil, ErrStore
	}
	service := NewService(config.Store)
	if config.Random != nil {
		service.random = config.Random
	}
	if config.Now != nil {
		service.now = config.Now
	}
	if config.IdleTimeout > 0 {
		service.idleTimeout = config.IdleTimeout
	}
	if config.AbsoluteTimeout > 0 {
		service.absoluteTimeout = config.AbsoluteTimeout
	}
	return service, nil
}

func (service *Service) applyArguments(arguments ...any) {
	var durations []time.Duration
	for _, argument := range arguments {
		switch value := argument.(type) {
		case Option:
			if value != nil {
				value(service)
			}
		case time.Duration:
			durations = append(durations, value)
		case io.Reader:
			if value != nil {
				service.random = value
			}
		case func() time.Time:
			if value != nil {
				service.now = value
			}
		case time.Time:
			fixed := value
			service.now = func() time.Time { return fixed }
		case Clock:
			if value != nil {
				service.now = value.Now
			}
		case ServiceConfig:
			if value.Store != nil {
				service.store = value.Store
			}
			if value.Random != nil {
				service.random = value.Random
			}
			if value.Now != nil {
				service.now = value.Now
			}
			if value.IdleTimeout > 0 {
				service.idleTimeout = value.IdleTimeout
			}
			if value.AbsoluteTimeout > 0 {
				service.absoluteTimeout = value.AbsoluteTimeout
			}
		}
	}
	if len(durations) > 0 && durations[0] > 0 {
		service.idleTimeout = durations[0]
	}
	if len(durations) > 1 && durations[1] > 0 {
		service.absoluteTimeout = durations[1]
	}
}

// CreateAdmin creates the first administrator only. CountAdmins is always the
// first store operation, preventing a second administrator even if its input
// would otherwise fail validation.
func (service *Service) CreateAdmin(ctx context.Context, username, password string) error {
	if service == nil || service.store == nil {
		return ErrStore
	}
	count, err := service.store.CountAdmins(ctx)
	if err != nil {
		return ErrStore
	}
	if count != 0 {
		return ErrAdminAlreadyExists
	}
	if err := ValidateUsername(username); err != nil {
		return err
	}
	hasher := NewPasswordHasher(service.random)
	passwordHash, err := hasher.Hash(password)
	if err != nil {
		return err
	}
	now := service.currentTime()
	_, err = service.store.CreateAdmin(ctx, dbgen.CreateAdminParams{
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return ErrStore
	}
	return nil
}

// Login authenticates credentials with one generic credential error for an
// unknown user, invalid username, malformed hash, or wrong password.
func (service *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	if service == nil || service.store == nil {
		return LoginResult{}, ErrStore
	}
	if ValidateUsername(username) != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	admin, err := service.store.GetAdminByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, ErrStore
	}
	if ValidatePassword(password) != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := VerifyPassword(password, admin.PasswordHash); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	now := service.currentTime()
	tokenRaw, err := randomBytes(service.random, tokenBytes)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	csrfRaw, err := randomBytes(service.random, tokenBytes)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	tokenHash := sha256.Sum256(tokenRaw)
	csrfHash := sha256.Sum256(csrfRaw)
	absoluteExpiresAt := now.Add(service.absoluteTimeout)
	result, err := service.store.CreateAdminSession(ctx, dbgen.CreateAdminSessionParams{
		TokenHash:         cloneBytes(tokenHash[:]),
		AdminID:           admin.ID,
		CsrfHash:          cloneBytes(csrfHash[:]),
		LastSeenAt:        now,
		AbsoluteExpiresAt: absoluteExpiresAt,
		CreatedAt:         now,
	})
	if err != nil {
		return LoginResult{}, ErrStore
	}
	sessionID, err := lastInsertID(result)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	return LoginResult{
		Admin:             publicAdmin(admin),
		SessionID:         sessionID,
		Token:             base64.RawURLEncoding.EncodeToString(tokenRaw),
		CSRFToken:         base64.RawURLEncoding.EncodeToString(csrfRaw),
		CSRF:              base64.RawURLEncoding.EncodeToString(csrfRaw),
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(service.idleTimeout),
		AbsoluteExpiresAt: absoluteExpiresAt,
	}, nil
}

// Authenticate validates a client token, enforces both expiry policies, and
// touches last_seen_at only for a currently valid session.
func (service *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if service == nil || service.store == nil {
		return Session{}, ErrStore
	}
	rawToken, ok := decodeClientToken(token)
	if !ok {
		return Session{}, ErrInvalidSession
	}
	tokenHash := sha256.Sum256(rawToken)
	row, err := service.store.GetAdminSessionByTokenHash(ctx, tokenHash[:])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrInvalidSession
		}
		return Session{}, ErrStore
	}
	if row.RevokedAt.Valid {
		return Session{}, ErrSessionRevoked
	}
	now := service.currentTime()
	if !now.Before(row.AbsoluteExpiresAt) || !now.Before(row.LastSeenAt.Add(service.idleTimeout)) {
		return Session{}, ErrSessionExpired
	}
	touchResult, err := service.store.TouchAdminSession(ctx, dbgen.TouchAdminSessionParams{ID: row.ID, LastSeenAt: now})
	if err != nil {
		return Session{}, ErrStore
	}
	touchedRows, err := rowsAffected(touchResult)
	if err != nil {
		return Session{}, ErrStore
	}
	switch touchedRows {
	case 0:
		refreshedRow, err := service.store.GetAdminSessionByTokenHash(ctx, tokenHash[:])
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Session{}, ErrSessionRevoked
			}
			return Session{}, ErrStore
		}
		if refreshedRow.RevokedAt.Valid {
			return Session{}, ErrSessionRevoked
		}
		if !now.Before(refreshedRow.AbsoluteExpiresAt) || !now.Before(refreshedRow.LastSeenAt.Add(service.idleTimeout)) {
			return Session{}, ErrSessionExpired
		}
		return Session{
			ID:                refreshedRow.ID,
			AdminID:           refreshedRow.AdminID,
			Username:          refreshedRow.Username,
			CSRFHash:          cloneBytes(refreshedRow.CsrfHash),
			LastSeenAt:        refreshedRow.LastSeenAt,
			IdleExpiresAt:     refreshedRow.LastSeenAt.Add(service.idleTimeout),
			AbsoluteExpiresAt: refreshedRow.AbsoluteExpiresAt,
		}, nil
	case 1:
		// Continue with the refreshed session below.
	default:
		return Session{}, ErrStore
	}
	return Session{
		ID:                row.ID,
		AdminID:           row.AdminID,
		Username:          row.Username,
		CSRFHash:          cloneBytes(row.CsrfHash),
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(service.idleTimeout),
		AbsoluteExpiresAt: row.AbsoluteExpiresAt,
	}, nil
}

// AuthenticateForReauth validates the token identity for a reauthentication
// request without refreshing idle activity. It intentionally permits a
// session whose idle window has elapsed so the caller can use the password to
// rotate the session, while still enforcing the absolute session lifetime.
func (service *Service) AuthenticateForReauth(ctx context.Context, token string) (Session, error) {
	if service == nil || service.store == nil {
		return Session{}, ErrStore
	}
	rawToken, ok := decodeClientToken(token)
	if !ok {
		return Session{}, ErrInvalidSession
	}
	tokenHash := sha256.Sum256(rawToken)
	row, err := service.store.GetAdminSessionByTokenHash(ctx, tokenHash[:])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrInvalidSession
		}
		return Session{}, ErrStore
	}
	if row.RevokedAt.Valid {
		return Session{}, ErrSessionRevoked
	}
	if !service.currentTime().Before(row.AbsoluteExpiresAt) {
		return Session{}, ErrSessionExpired
	}
	return Session{
		ID:                row.ID,
		AdminID:           row.AdminID,
		Username:          row.Username,
		CSRFHash:          cloneBytes(row.CsrfHash),
		LastSeenAt:        row.LastSeenAt,
		IdleExpiresAt:     row.LastSeenAt.Add(service.idleTimeout),
		AbsoluteExpiresAt: row.AbsoluteExpiresAt,
	}, nil
}

// ValidateCSRF hashes the client token and compares it with an authenticated
// session's stored digest in constant time. The first argument accepts Session,
// *Session, or a raw expected hash for small middleware adapters.
func (service *Service) ValidateCSRF(expected any, clientToken string) error {
	expectedHash, ok := csrfExpectedHash(expected)
	if !ok {
		return ErrCSRFInvalid
	}
	rawToken, ok := decodeClientToken(clientToken)
	if !ok {
		return ErrCSRFInvalid
	}
	actualHash := sha256.Sum256(rawToken)
	if subtle.ConstantTimeCompare(expectedHash, actualHash[:]) != 1 {
		return ErrCSRFInvalid
	}
	return nil
}

// CheckCSRF is a boolean convenience wrapper around ValidateCSRF.
func (service *Service) CheckCSRF(expected any, clientToken string) bool {
	return service.ValidateCSRF(expected, clientToken) == nil
}

// Logout revokes one session using the current UTC time. Repeated revocation
// is harmless because the generated query already limits revoked rows.
func (service *Service) Logout(ctx context.Context, sessionID uint64) error {
	if service == nil || service.store == nil {
		return ErrStore
	}
	now := service.currentTime()
	_, err := service.store.RevokeAdminSession(ctx, dbgen.RevokeAdminSessionParams{
		ID:        sessionID,
		RevokedAt: sql.NullTime{Time: now, Valid: true},
	})
	if err != nil {
		return ErrStore
	}
	return nil
}

// Reauthenticate verifies the current administrator's password, creates a new
// session first, and then revokes the old session. A revoke failure is returned
// without attempting rollback or issuing another SQL statement.
func (service *Service) Reauthenticate(ctx context.Context, current Session, password string) (LoginResult, error) {
	if service == nil || service.store == nil {
		return LoginResult{}, ErrStore
	}
	admin, err := service.store.GetAdminByUsername(ctx, current.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, ErrStore
	}
	if ValidatePassword(password) != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := VerifyPassword(password, admin.PasswordHash); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	loginResult, err := service.createSession(ctx, admin)
	if err != nil {
		return LoginResult{}, err
	}
	now := service.currentTime()
	revokeResult, err := service.store.RevokeAdminSession(ctx, dbgen.RevokeAdminSessionParams{
		ID:        current.ID,
		RevokedAt: sql.NullTime{Time: now, Valid: true},
	})
	if err != nil {
		return LoginResult{}, ErrStore
	}
	revokedRows, err := rowsAffected(revokeResult)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	switch revokedRows {
	case 0:
		return LoginResult{}, ErrInvalidSession
	case 1:
		// The old session was revoked after the new one was created.
	default:
		return LoginResult{}, ErrStore
	}
	return loginResult, nil
}

// ReauthenticateBySessionID is a convenience for adapters that have retained
// only the session ID and username.
func (service *Service) ReauthenticateBySessionID(ctx context.Context, sessionID uint64, username, password string) (LoginResult, error) {
	return service.Reauthenticate(ctx, Session{ID: sessionID, Username: username}, password)
}

func (service *Service) createSession(ctx context.Context, admin dbgen.Admin) (LoginResult, error) {
	now := service.currentTime()
	tokenRaw, err := randomBytes(service.random, tokenBytes)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	csrfRaw, err := randomBytes(service.random, tokenBytes)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	tokenHash := sha256.Sum256(tokenRaw)
	csrfHash := sha256.Sum256(csrfRaw)
	absoluteExpiresAt := now.Add(service.absoluteTimeout)
	result, err := service.store.CreateAdminSession(ctx, dbgen.CreateAdminSessionParams{
		TokenHash:         cloneBytes(tokenHash[:]),
		AdminID:           admin.ID,
		CsrfHash:          cloneBytes(csrfHash[:]),
		LastSeenAt:        now,
		AbsoluteExpiresAt: absoluteExpiresAt,
		CreatedAt:         now,
	})
	if err != nil {
		return LoginResult{}, ErrStore
	}
	sessionID, err := lastInsertID(result)
	if err != nil {
		return LoginResult{}, ErrStore
	}
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfRaw)
	return LoginResult{
		Admin:             publicAdmin(admin),
		SessionID:         sessionID,
		Token:             base64.RawURLEncoding.EncodeToString(tokenRaw),
		CSRFToken:         csrfToken,
		CSRF:              csrfToken,
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(service.idleTimeout),
		AbsoluteExpiresAt: absoluteExpiresAt,
	}, nil
}

func ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 64 {
		return ErrInvalidUsername
	}
	for index := 0; index < len(username); index++ {
		if username[index] >= 0x80 {
			return ErrInvalidUsername
		}
		if username[index] < 0x20 || username[index] == 0x7f {
			return ErrInvalidUsername
		}
	}
	first, _ := utf8.DecodeRuneInString(username)
	last, _ := utf8.DecodeLastRuneInString(username)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return ErrInvalidUsername
	}
	return nil
}

func publicAdmin(admin dbgen.Admin) Admin {
	return Admin{ID: admin.ID, Username: admin.Username, CreatedAt: admin.CreatedAt.UTC(), UpdatedAt: admin.UpdatedAt.UTC()}
}

func (service *Service) currentTime() time.Time {
	if service == nil || service.now == nil {
		return time.Now().UTC()
	}
	return service.now().UTC()
}

func randomBytes(randomReader io.Reader, size int) ([]byte, error) {
	if randomReader == nil {
		randomReader = rand.Reader
	}
	buffer := make([]byte, size)
	if _, err := io.ReadFull(randomReader, buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

func decodeClientToken(token string) ([]byte, bool) {
	if token == "" {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return nil, false
	}
	return raw, true
}

func csrfExpectedHash(value any) ([]byte, bool) {
	switch expected := value.(type) {
	case Session:
		return expected.CSRFHash, len(expected.CSRFHash) == sha256.Size
	case *Session:
		if expected == nil {
			return nil, false
		}
		return expected.CSRFHash, len(expected.CSRFHash) == sha256.Size
	case []byte:
		return expected, len(expected) == sha256.Size
	case [sha256.Size]byte:
		return expected[:], true
	default:
		return nil, false
	}
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func rowsAffected(result sql.Result) (int64, error) {
	if result == nil {
		return 0, ErrStore
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, ErrStore
	}
	return rows, nil
}

func lastInsertID(result sql.Result) (uint64, error) {
	if result == nil {
		return 0, ErrStore
	}
	id, err := result.LastInsertId()
	if err != nil || id < 0 {
		return 0, ErrStore
	}
	return uint64(id), nil
}
