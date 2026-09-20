package app

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfigDefaults(t *testing.T) {
	config, err := ParseConfig(mapLookup(requiredDatabaseValues()))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("HTTPAddr = %q, want %q", config.HTTPAddr, defaultHTTPAddr)
	}
	if config.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", config.ReadHeaderTimeout, defaultReadHeaderTimeout)
	}
	if config.ReadTimeout != defaultReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", config.ReadTimeout, defaultReadTimeout)
	}
	if config.WriteTimeout != defaultWriteTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", config.WriteTimeout, defaultWriteTimeout)
	}
	if config.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", config.IdleTimeout, defaultIdleTimeout)
	}
	if config.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %s, want %s", config.ShutdownTimeout, defaultShutdownTimeout)
	}
	if config.AppEnv != defaultAppEnv || config.PublicBaseURL != "https://example.test" || config.SessionIdleDuration != defaultSessionIdleTimeout || config.SessionAbsoluteDuration != defaultSessionAbsoluteTimeout || config.CookieSecure {
		t.Fatalf("application settings = %+v, want defaults", config)
	}
	if config.UploadDir != "C:\\uploads" || config.MaxUploadBytes != defaultMaxUploadBytes || config.MaxImagePixels != defaultMaxImagePixels || config.MaxImageEdge != defaultMaxImageEdge || config.GitHubAPIBaseURL != defaultGitHubAPIBaseURL || config.GitHubTimeout != defaultGitHubTimeout {
		t.Fatalf("stage2 settings = %+v, want defaults", config)
	}
	if config.Database.Host != "db" || config.Database.Port != "3306" || config.Database.Name != "mywebsite" || config.Database.User != "app" || config.Database.PasswordFile != "C:\\secrets\\db-password" {
		t.Fatalf("Database = %+v, want required database settings", config.Database)
	}
}

func TestParseConfigCustomValues(t *testing.T) {
	values := map[string]string{
		"HTTP_ADDR":                "127.0.0.1:9090",
		"HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_READ_TIMEOUT":        "3s",
		"HTTP_WRITE_TIMEOUT":       "4s",
		"HTTP_IDLE_TIMEOUT":        "5s",
		"HTTP_SHUTDOWN_TIMEOUT":    "6s",
		"UPLOAD_DIR":               "D:\\media",
		"MAX_UPLOAD_BYTES":         "123456",
		"MAX_IMAGE_PIXELS":         "345678",
		"MAX_IMAGE_EDGE":           "4096",
		"GITHUB_API_BASE_URL":      "http://github.internal/",
		"GITHUB_TIMEOUT":           "7s",
	}
	for key, value := range requiredDatabaseValues() {
		values[key] = value
	}
	config, err := ParseConfig(mapLookup(values))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if config.HTTPAddr != values["HTTP_ADDR"] {
		t.Fatalf("HTTPAddr = %q, want %q", config.HTTPAddr, values["HTTP_ADDR"])
	}
	if config.ReadHeaderTimeout != 2*time.Second || config.ReadTimeout != 3*time.Second || config.WriteTimeout != 4*time.Second || config.IdleTimeout != 5*time.Second || config.ShutdownTimeout != 6*time.Second {
		t.Fatalf("custom timeout values = %+v", config)
	}
	if config.UploadDir != values["UPLOAD_DIR"] || config.MaxUploadBytes != 123456 || config.MaxImagePixels != 345678 || config.MaxImageEdge != 4096 || config.GitHubAPIBaseURL != "http://github.internal" || config.GitHubTimeout != 7*time.Second {
		t.Fatalf("custom stage2 settings = %+v", config)
	}
}

func TestParseConfigRequiresUploadDir(t *testing.T) {
	values := requiredDatabaseValues()
	delete(values, "UPLOAD_DIR")
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "UPLOAD_DIR") {
		t.Fatalf("ParseConfig() error = %v, want an UPLOAD_DIR error", err)
	}
}

func TestParseConfigRejectsInvalidMediaLimits(t *testing.T) {
	for _, key := range []string{"MAX_UPLOAD_BYTES", "MAX_IMAGE_PIXELS", "MAX_IMAGE_EDGE"} {
		for _, value := range []string{"", "0", "-1", " 1", "1 ", "1.5", "+1", "9223372036854775808"} {
			values := requiredDatabaseValues()
			values[key] = value
			_, err := ParseConfig(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("ParseConfig(%s=%q) error = %v, want a %s error", key, value, err, key)
			}
		}
	}
}

func TestParseConfigValidatesGitHubSettings(t *testing.T) {
	for _, value := range []string{
		"",
		"ftp://api.github.com",
		"https://api.github.com/v1",
		"https://user:password@api.github.com",
		"https://api.github.com?query=1",
		"https://api.github.com#fragment",
		"https:api.github.com",
	} {
		values := requiredDatabaseValues()
		values["GITHUB_API_BASE_URL"] = value
		_, err := ParseConfig(mapLookup(values))
		if err == nil || !strings.Contains(err.Error(), "GITHUB_API_BASE_URL") {
			t.Fatalf("ParseConfig(GITHUB_API_BASE_URL=%q) error = %v, want a GITHUB_API_BASE_URL error", value, err)
		}
	}

	for _, value := range []string{"0s", "-1s", "not-a-duration", " 5s"} {
		values := requiredDatabaseValues()
		values["GITHUB_TIMEOUT"] = value
		_, err := ParseConfig(mapLookup(values))
		if err == nil || !strings.Contains(err.Error(), "GITHUB_TIMEOUT") {
			t.Fatalf("ParseConfig(GITHUB_TIMEOUT=%q) error = %v, want a GITHUB_TIMEOUT error", value, err)
		}
	}
}

func TestParseConfigRequiresDatabaseSettings(t *testing.T) {
	for key := range requiredDatabaseValues() {
		values := requiredDatabaseValues()
		delete(values, key)
		_, err := ParseConfig(mapLookup(values))
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("ParseConfig() error = %v, want a %s error", err, key)
		}
	}
}

func TestParseConfigRejectsInvalidDatabasePort(t *testing.T) {
	for _, port := range []string{"0", "65536", "not-a-port", " 3306"} {
		values := requiredDatabaseValues()
		values["DB_PORT"] = port
		_, err := ParseConfig(mapLookup(values))
		if err == nil || !strings.Contains(err.Error(), "DB_PORT") {
			t.Fatalf("ParseConfig(DB_PORT=%q) error = %v, want a DB_PORT error", port, err)
		}
	}
}

func TestParseConfigRejectsDatabaseWhitespace(t *testing.T) {
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD_FILE"} {
		values := requiredDatabaseValues()
		values[key] = " " + values[key]
		_, err := ParseConfig(mapLookup(values))
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("ParseConfig(%s) error = %v, want whitespace error", key, err)
		}
	}
}

func TestParseConfigRejectsInvalidAddress(t *testing.T) {
	values := requiredDatabaseValues()
	values["HTTP_ADDR"] = "not-an-address"
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR") {
		t.Fatalf("ParseConfig() error = %v, want an HTTP_ADDR error", err)
	}
}

func TestParseConfigRejectsInvalidTimeout(t *testing.T) {
	values := requiredDatabaseValues()
	values["HTTP_READ_TIMEOUT"] = "not-a-duration"
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "HTTP_READ_TIMEOUT") {
		t.Fatalf("ParseConfig() error = %v, want an HTTP_READ_TIMEOUT error", err)
	}
}

func TestParseConfigLimitsShutdownTimeout(t *testing.T) {
	values := requiredDatabaseValues()
	values["HTTP_SHUTDOWN_TIMEOUT"] = "31s"
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("ParseConfig() error = %v, want a shutdown limit error", err)
	}
}

func TestParseConfigAcceptsProductionSettings(t *testing.T) {
	values := requiredDatabaseValues()
	values["APP_ENV"] = "production"
	values["PUBLIC_BASE_URL"] = "https://example.test/"
	values["SESSION_IDLE_DURATION"] = "2h"
	values["SESSION_ABSOLUTE_DURATION"] = "4h"
	values["COOKIE_SECURE"] = "true"
	config, err := ParseConfig(mapLookup(values))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if config.AppEnv != "production" || config.PublicBaseURL != "https://example.test" || config.SessionIdleDuration != 2*time.Hour || config.SessionAbsoluteDuration != 4*time.Hour || !config.CookieSecure {
		t.Fatalf("config = %+v", config)
	}
}

func TestParseConfigRejectsInvalidApplicationSettings(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "env", key: "APP_ENV", value: "staging"},
		{name: "base url", key: "PUBLIC_BASE_URL", value: "https://example.test/path"},
		{name: "session idle", key: "SESSION_IDLE_DURATION", value: "0s"},
		{name: "session order", key: "SESSION_IDLE_DURATION", value: "25h"},
		{name: "cookie secure", key: "COOKIE_SECURE", value: "maybe"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := requiredDatabaseValues()
			if test.name == "session order" {
				values["SESSION_ABSOLUTE_DURATION"] = "24h"
			}
			values[test.key] = test.value
			if _, err := ParseConfig(mapLookup(values)); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("ParseConfig() error = %v, want %s error", err, test.key)
			}
		})
	}

	values := requiredDatabaseValues()
	values["APP_ENV"] = "production"
	values["PUBLIC_BASE_URL"] = "http://example.test"
	values["COOKIE_SECURE"] = "false"
	if _, err := ParseConfig(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("production ParseConfig() error = %v", err)
	}
}

func emptyLookup(string) (string, bool) {
	return "", false
}

func requiredDatabaseValues() map[string]string {
	return map[string]string{
		"PUBLIC_BASE_URL":  "https://example.test",
		"UPLOAD_DIR":       "C:\\uploads",
		"DB_HOST":          "db",
		"DB_PORT":          "3306",
		"DB_NAME":          "mywebsite",
		"DB_USER":          "app",
		"DB_PASSWORD_FILE": "C:\\secrets\\db-password",
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
