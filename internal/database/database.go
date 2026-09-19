package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"mywebsite/internal/database/dbgen"
)

const (
	maxOpenConnections = 20
	maxIdleConnections = 10

	expectedMySQLVersionPrefix = "8.4."
	expectedTimeZone           = "+00:00"
	expectedConnectionCharset  = "utf8mb4"
	expectedDatabaseCollation  = "utf8mb4_0900_ai_ci"
	expectedSchemaVersion      = int64(1)
)

const (
	startupValidationQuery = `
SELECT VERSION(), @@session.time_zone, @@character_set_connection, @@collation_database`

	currentSchemaVersionQuery = `
SELECT version_id, is_applied
FROM goose_db_version AS current_version
WHERE current_version.id = (
    SELECT MAX(history.id)
    FROM goose_db_version AS history
    WHERE history.version_id = current_version.version_id
)`
)

// Config contains the database settings required by the application.
// Port is kept as the validated decimal environment value so it can be passed
// directly to net.JoinHostPort without constructing a DSN by concatenation.
type Config struct {
	Host         string
	Port         string
	Name         string
	User         string
	PasswordFile string
}

// Database owns the application connection pool and its generated queries.
type Database struct {
	db      *sql.DB
	queries *dbgen.Queries
}

// Open reads the password file, creates the MySQL connection pool, and
// validates the server and session settings required by the application. It
// never runs migrations.
func Open(ctx context.Context, config Config) (*Database, error) {
	if ctx == nil {
		return nil, fmt.Errorf("database open context is nil")
	}

	password, err := readPassword(config.PasswordFile)
	if err != nil {
		return nil, err
	}

	driverConfig, err := buildMySQLConfig(config, password)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open database connection: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxIdleConnections)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := validateStartupConnection(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Database{
		db:      db,
		queries: dbgen.New(db),
	}, nil
}

// Queries returns the generated query set backed by this database.
func (d *Database) Queries() *dbgen.Queries {
	if d == nil {
		return nil
	}
	return d.queries
}

// DB returns the underlying connection pool.
func (d *Database) DB() *sql.DB {
	if d == nil {
		return nil
	}
	return d.db
}

// Close releases the connection pool.
func (d *Database) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// CheckReady verifies that the database is reachable and that the latest
// still-applied goose migration version is exactly the application version.
// It deliberately does not create or modify the goose table.
func (d *Database) CheckReady(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("database readiness context is nil")
	}
	if d == nil || d.db == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := d.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database for readiness: %w", err)
	}

	rows, err := d.db.QueryContext(ctx, currentSchemaVersionQuery)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	defer rows.Close()

	currentVersion := int64(0)
	for rows.Next() {
		var versionID int64
		var applied bool
		if err := rows.Scan(&versionID, &applied); err != nil {
			return fmt.Errorf("scan schema version: %w", err)
		}
		if applied && versionID > currentVersion {
			currentVersion = versionID
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema version rows: %w", err)
	}
	if currentVersion != expectedSchemaVersion {
		return fmt.Errorf("schema version is %d, want %d", currentVersion, expectedSchemaVersion)
	}
	return nil
}

func readPassword(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("database password file must not be empty")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read database password file: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("database password file must not be empty")
	}

	// Secret files conventionally contain one password followed by one line
	// ending. Accept LF and CRLF, but reject additional lines or any other
	// leading/trailing whitespace so the password is not silently changed.
	if bytesIndexByte(raw, '\n') >= 0 {
		newline := bytesIndexByte(raw, '\n')
		if newline != len(raw)-1 {
			return "", fmt.Errorf("database password file must contain one line")
		}
		raw = raw[:newline]
		if len(raw) > 0 && raw[len(raw)-1] == '\r' {
			raw = raw[:len(raw)-1]
		}
	}
	if bytesIndexByte(raw, '\r') >= 0 || bytesIndexByte(raw, '\n') >= 0 {
		return "", fmt.Errorf("database password file must contain one line")
	}

	password := string(raw)
	if password == "" {
		return "", fmt.Errorf("database password file must not be empty")
	}
	if strings.TrimSpace(password) != password {
		return "", fmt.Errorf("database password file must not contain leading or trailing whitespace")
	}
	return password, nil
}

func buildMySQLConfig(config Config, password string) (*mysql.Config, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if password == "" {
		return nil, fmt.Errorf("database password must not be empty")
	}

	driverConfig := mysql.NewConfig()
	driverConfig.User = config.User
	driverConfig.Passwd = password
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(config.Host, config.Port)
	driverConfig.DBName = config.Name
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Collation = expectedDatabaseCollation
	// charset is a driver DSN parameter. FormatDSN emits it and sql.Open
	// parses it back into the driver's connection charset setup; time_zone is
	// an explicit session assignment performed by the driver after connect.
	driverConfig.Params = map[string]string{
		"charset":   expectedConnectionCharset,
		"time_zone": "'+00:00'",
	}
	return driverConfig, nil
}

func validateConfig(config Config) error {
	for key, value := range map[string]string{
		"host":          config.Host,
		"port":          config.Port,
		"name":          config.Name,
		"user":          config.User,
		"password_file": config.PasswordFile,
	} {
		if value == "" {
			return fmt.Errorf("database %s must not be empty", key)
		}
	}

	port, err := strconv.Atoi(config.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("database port %q must be between 1 and 65535", config.Port)
	}
	return nil
}

func validateStartupConnection(ctx context.Context, db *sql.DB) error {
	var version string
	var timeZone string
	var charset string
	var collation string
	if err := db.QueryRowContext(ctx, startupValidationQuery).Scan(&version, &timeZone, &charset, &collation); err != nil {
		return fmt.Errorf("validate database connection: %w", err)
	}
	if !strings.HasPrefix(version, expectedMySQLVersionPrefix) {
		return fmt.Errorf("unsupported MySQL version")
	}
	if timeZone != expectedTimeZone {
		return fmt.Errorf("database session time zone is not UTC")
	}
	if charset != expectedConnectionCharset {
		return fmt.Errorf("database connection charset is not utf8mb4")
	}
	if collation != expectedDatabaseCollation {
		return fmt.Errorf("database default collation is not utf8mb4_0900_ai_ci")
	}
	return nil
}

func bytesIndexByte(raw []byte, target byte) int {
	for index, value := range raw {
		if value == target {
			return index
		}
	}
	return -1
}
