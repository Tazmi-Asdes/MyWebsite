package database

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestReadPasswordAcceptsSingleTrailingLineEnding(t *testing.T) {
	dir := t.TempDir()
	path := dir + string(os.PathSeparator) + "password"
	if err := os.WriteFile(path, []byte("secret\r\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	password, err := readPassword(path)
	if err != nil {
		t.Fatalf("readPassword() error = %v", err)
	}
	if password != "secret" {
		t.Fatalf("password = %q, want secret", password)
	}
}

func TestReadPasswordRejectsEmptyAndExtraWhitespace(t *testing.T) {
	for _, content := range []string{"", "\n", " secret", "secret ", "secret\nextra", "secret\n\n"} {
		t.Run(strings.ReplaceAll(content, "\n", "\\n"), func(t *testing.T) {
			dir := t.TempDir()
			path := dir + string(os.PathSeparator) + "password"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if password, err := readPassword(path); err == nil {
				t.Fatalf("readPassword() password = %q, want error", password)
			}
		})
	}
}

func TestBuildMySQLConfigUsesRequiredConnectionSettings(t *testing.T) {
	driverConfig, err := buildMySQLConfig(Config{
		Host:         "mysql.example",
		Port:         "3307",
		Name:         "mywebsite",
		User:         "mywebsite",
		PasswordFile: "/run/secrets/mysql-password",
	}, "secret")
	if err != nil {
		t.Fatalf("buildMySQLConfig() error = %v", err)
	}

	if driverConfig.Net != "tcp" {
		t.Fatalf("Net = %q, want tcp", driverConfig.Net)
	}
	if driverConfig.Addr != "mysql.example:3307" {
		t.Fatalf("Addr = %q, want mysql.example:3307", driverConfig.Addr)
	}
	if driverConfig.DBName != "mywebsite" || driverConfig.User != "mywebsite" {
		t.Fatalf("database identity = (%q, %q), want mywebsite", driverConfig.DBName, driverConfig.User)
	}
	if driverConfig.ParseTime != true || driverConfig.Loc != time.UTC {
		t.Fatalf("time settings = ParseTime:%v Loc:%v, want true/UTC", driverConfig.ParseTime, driverConfig.Loc)
	}
	if driverConfig.Collation != expectedDatabaseCollation {
		t.Fatalf("Collation = %q, want %q", driverConfig.Collation, expectedDatabaseCollation)
	}
	if driverConfig.Params["charset"] != expectedConnectionCharset {
		t.Fatalf("charset param = %q, want %q", driverConfig.Params["charset"], expectedConnectionCharset)
	}
	if driverConfig.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("time_zone param = %q, want '+00:00'", driverConfig.Params["time_zone"])
	}

	dsn := driverConfig.FormatDSN()
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if parsed.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("parsed time_zone = %q, want '+00:00'", parsed.Params["time_zone"])
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Fatalf("DSN = %q, want utf8mb4 charset", dsn)
	}
	if !strings.Contains(dsn, "collation=utf8mb4_0900_ai_ci") {
		t.Fatalf("DSN = %q, want required collation", dsn)
	}
}

func TestSchemaVersionQueryUsesLatestRecordPerVersion(t *testing.T) {
	if !strings.Contains(currentSchemaVersionQuery, "MAX(history.id)") {
		t.Fatalf("query = %q, want latest history id per version", currentSchemaVersionQuery)
	}
	if !strings.Contains(currentSchemaVersionQuery, "history.version_id = current_version.version_id") {
		t.Fatalf("query = %q, want version_id correlation", currentSchemaVersionQuery)
	}
}
