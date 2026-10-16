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
	if config.TrustedProxyCIDRs != nil {
		t.Fatalf("TrustedProxyCIDRs = %v, want nil by default", config.TrustedProxyCIDRs)
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

func TestParseConfigTrustedProxyCIDRs(t *testing.T) {
	values := requiredDatabaseValues()
	values["TRUSTED_PROXY_CIDRS"] = " 192.0.2.42/24, 2001:db8::1/128, 192.0.2.0/24 "
	config, err := ParseConfig(mapLookup(values))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	want := []string{"192.0.2.0/24", "2001:db8::1/128"}
	if len(config.TrustedProxyCIDRs) != len(want) {
		t.Fatalf("TrustedProxyCIDRs = %v, want %v", config.TrustedProxyCIDRs, want)
	}
	for index := range want {
		if config.TrustedProxyCIDRs[index] != want[index] {
			t.Fatalf("TrustedProxyCIDRs = %v, want %v", config.TrustedProxyCIDRs, want)
		}
	}

	values["TRUSTED_PROXY_CIDRS"] = ""
	config, err = ParseConfig(mapLookup(values))
	if err != nil {
		t.Fatalf("ParseConfig(empty TRUSTED_PROXY_CIDRS) error = %v", err)
	}
	if config.TrustedProxyCIDRs != nil {
		t.Fatalf("TrustedProxyCIDRs for an empty value = %v, want nil", config.TrustedProxyCIDRs)
	}
}

func TestParseConfigRejectsInvalidTrustedProxyCIDRs(t *testing.T) {
	for _, value := range []string{
		"invalid",
		"192.0.2.1",
		"192.0.2.0/33",
		"2001:db8::/129",
		",192.0.2.0/24",
		"192.0.2.0/24,,2001:db8::/32",
		"192.0.2.0/24,",
		" ",
	} {
		values := requiredDatabaseValues()
		values["TRUSTED_PROXY_CIDRS"] = value
		if _, err := ParseConfig(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
			t.Fatalf("ParseConfig(TRUSTED_PROXY_CIDRS=%q) error = %v, want a TRUSTED_PROXY_CIDRS error", value, err)
		}
	}
}

func TestParseConfigRedisURLRedactsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		field   string
		value   string
		path    string
		secrets []string
	}{
		{
			name:    "credentials",
			value:   "redis://fake-user:user-secret@redis:6379/0",
			secrets: []string{"fake-user", "user-secret"},
		},
		{
			name:    "query",
			value:   "redis://redis:6379/0?token=query-secret",
			secrets: []string{"query-secret"},
		},
		{
			name:  "empty query",
			value: "redis://redis:6379/0?",
		},
		{
			name:    "fragment",
			value:   "redis://redis:6379/0#fragment-secret",
			secrets: []string{"fragment-secret"},
		},
		{
			name:  "empty fragment",
			value: "redis://redis:6379/0#",
		},
		{
			name:    "malformed",
			value:   "redis://fake-user:bad-url-secret@redis%zz:6379/0?token=bad-url-query-secret",
			secrets: []string{"fake-user", "bad-url-secret", "bad-url-query-secret"},
		},
		{
			name:    "invalid database path",
			value:   "redis://redis:6379/api-key-secret",
			path:    "api-key-secret",
			secrets: []string{"api-key-secret"},
		},
		{
			name:    "oversized database path",
			value:   "redis://redis:6379/" + strings.Repeat("9", 100),
			path:    strings.Repeat("9", 100),
			field:   "REDIS_DB",
			secrets: []string{strings.Repeat("9", 100)},
		},
		{
			name:    "invalid REDIS_DB value",
			key:     "REDIS_DB",
			field:   "REDIS_DB",
			value:   "db-secret-value",
			path:    "db-secret-value",
			secrets: []string{"db-secret-value"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := requiredDatabaseValues()
			key := test.key
			if key == "" {
				key = "REDIS_URL"
			}
			field := test.field
			if field == "" {
				field = "REDIS_URL"
			}
			values[key] = test.value
			_, err := ParseConfig(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("ParseConfig() error = %v, want a %s error", err, field)
			}
			if strings.Contains(err.Error(), test.value) {
				t.Fatalf("ParseConfig() error exposed %s: %v", key, err)
			}
			if test.path != "" && strings.Contains(err.Error(), test.path) {
				t.Fatalf("ParseConfig() error exposed database path %q: %v", test.path, err)
			}
			for _, secret := range test.secrets {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("ParseConfig() error exposed %q: %v", secret, err)
				}
			}
		})
	}
}

func TestParseConfigPreservesRedisAddressAndPasswordFile(t *testing.T) {
	values := requiredDatabaseValues()
	values["REDIS_ADDR"] = "Redis:6379"
	values["REDIS_DB"] = "2"
	values["REDIS_PASSWORD_FILE"] = "C:\\secrets\\redis-password"
	config, err := ParseConfig(mapLookup(values))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if !config.RedisEnabled || config.Redis.Addr != "Redis:6379" || config.Redis.DB != 2 || config.RedisPasswordFile != values["REDIS_PASSWORD_FILE"] {
		t.Fatalf("Redis config = %+v, password file = %q, want address, database, and password file preserved", config.Redis, config.RedisPasswordFile)
	}

	values = requiredDatabaseValues()
	values["REDIS_URL"] = "redis://Redis:6379/3"
	values["REDIS_PASSWORD_FILE"] = "C:\\secrets\\redis-password"
	config, err = ParseConfig(mapLookup(values))
	if err != nil {
		t.Fatalf("ParseConfig(valid REDIS_URL) error = %v", err)
	}
	if !config.RedisEnabled || config.Redis.Addr != "Redis:6379" || config.Redis.DB != 3 || config.RedisPasswordFile != values["REDIS_PASSWORD_FILE"] {
		t.Fatalf("Redis URL config = %+v, password file = %q, want URL host, database, and password file preserved", config.Redis, config.RedisPasswordFile)
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
