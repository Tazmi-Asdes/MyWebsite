package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"mywebsite/internal/cache"
	"mywebsite/internal/database"
)

const (
	defaultHTTPAddr               = "127.0.0.1:8080"
	defaultAppEnv                 = "development"
	defaultSessionIdleTimeout     = 8 * time.Hour
	defaultSessionAbsoluteTimeout = 24 * time.Hour
	defaultCookieSecure           = false
	defaultReadHeaderTimeout      = 5 * time.Second
	defaultReadTimeout            = 15 * time.Second
	defaultWriteTimeout           = 15 * time.Second
	defaultIdleTimeout            = 60 * time.Second
	defaultShutdownTimeout        = 30 * time.Second
	defaultMaxUploadBytes         = int64(5 * 1024 * 1024)
	defaultMaxImagePixels         = int64(20_000_000)
	defaultMaxImageEdge           = int64(8_000)
	defaultGitHubAPIBaseURL       = "https://api.github.com"
	defaultGitHubTimeout          = 5 * time.Second
	maxGracefulShutdownTimeout    = 30 * time.Second
)

// Config contains the HTTP and database settings needed to run the server.
type Config struct {
	AppEnv                  string
	PublicBaseURL           string
	SessionIdleDuration     time.Duration
	SessionAbsoluteDuration time.Duration
	CookieSecure            bool
	HTTPAddr                string
	ReadHeaderTimeout       time.Duration
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	IdleTimeout             time.Duration
	ShutdownTimeout         time.Duration
	UploadDir               string
	MaxUploadBytes          int64
	MaxImagePixels          int64
	MaxImageEdge            int64
	GitHubAPIBaseURL        string
	GitHubTimeout           time.Duration
	Database                database.Config
	Redis                   cache.RedisConfig
	RedisPasswordFile       string
	RedisEnabled            bool
	TrustedProxyCIDRs       []string
}

// LoadConfig reads the process environment and validates all supported HTTP
// and database settings. It deliberately does not open a database or inspect
// the password file.
func LoadConfig() (Config, error) {
	return ParseConfig(os.LookupEnv)
}

// ParseConfig is the injectable form of LoadConfig used by tests and callers
// that provide configuration from a source other than the process environment.
func ParseConfig(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, fmt.Errorf("configuration lookup function is nil")
	}

	appEnv, err := configuredValue(lookup, "APP_ENV", defaultAppEnv)
	if err != nil {
		return Config{}, err
	}
	if appEnv != "development" && appEnv != "production" {
		return Config{}, fmt.Errorf("invalid APP_ENV %q: must be development or production", appEnv)
	}

	publicBaseURL, err := requiredConfiguredValue(lookup, "PUBLIC_BASE_URL")
	if err != nil {
		return Config{}, err
	}
	publicBaseURL, err = validatePublicBaseURL(publicBaseURL)
	if err != nil {
		return Config{}, err
	}

	sessionIdleDuration, err := durationValue(lookup, "SESSION_IDLE_DURATION", defaultSessionIdleTimeout)
	if err != nil {
		return Config{}, err
	}
	sessionAbsoluteDuration, err := durationValue(lookup, "SESSION_ABSOLUTE_DURATION", defaultSessionAbsoluteTimeout)
	if err != nil {
		return Config{}, err
	}
	if sessionIdleDuration > sessionAbsoluteDuration {
		return Config{}, fmt.Errorf("SESSION_IDLE_DURATION must be no greater than SESSION_ABSOLUTE_DURATION")
	}
	cookieSecure, err := boolValue(lookup, "COOKIE_SECURE", defaultCookieSecure)
	if err != nil {
		return Config{}, err
	}
	if appEnv == "production" && (!cookieSecure || !strings.HasPrefix(publicBaseURL, "https://")) {
		return Config{}, fmt.Errorf("production requires PUBLIC_BASE_URL to use https and COOKIE_SECURE=true")
	}

	addr, err := configuredValue(lookup, "HTTP_ADDR", defaultHTTPAddr)
	if err != nil {
		return Config{}, err
	}
	if err := validateHTTPAddr(addr); err != nil {
		return Config{}, fmt.Errorf("invalid HTTP_ADDR %q: %w", addr, err)
	}

	readHeaderTimeout, err := durationValue(lookup, "HTTP_READ_HEADER_TIMEOUT", defaultReadHeaderTimeout)
	if err != nil {
		return Config{}, err
	}
	readTimeout, err := durationValue(lookup, "HTTP_READ_TIMEOUT", defaultReadTimeout)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := durationValue(lookup, "HTTP_WRITE_TIMEOUT", defaultWriteTimeout)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationValue(lookup, "HTTP_IDLE_TIMEOUT", defaultIdleTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationValue(lookup, "HTTP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	if shutdownTimeout > maxGracefulShutdownTimeout {
		return Config{}, fmt.Errorf("HTTP_SHUTDOWN_TIMEOUT must be at most %s, got %s", maxGracefulShutdownTimeout, shutdownTimeout)
	}

	uploadDir, err := requiredConfiguredValue(lookup, "UPLOAD_DIR")
	if err != nil {
		return Config{}, err
	}
	maxUploadBytes, err := positiveInt64Value(lookup, "MAX_UPLOAD_BYTES", defaultMaxUploadBytes)
	if err != nil {
		return Config{}, err
	}
	maxImagePixels, err := positiveInt64Value(lookup, "MAX_IMAGE_PIXELS", defaultMaxImagePixels)
	if err != nil {
		return Config{}, err
	}
	maxImageEdge, err := positiveInt64Value(lookup, "MAX_IMAGE_EDGE", defaultMaxImageEdge)
	if err != nil {
		return Config{}, err
	}
	githubAPIBaseURL, err := configuredValue(lookup, "GITHUB_API_BASE_URL", defaultGitHubAPIBaseURL)
	if err != nil {
		return Config{}, err
	}
	githubAPIBaseURL, err = validateGitHubAPIBaseURL(githubAPIBaseURL)
	if err != nil {
		return Config{}, err
	}
	githubTimeout, err := durationValue(lookup, "GITHUB_TIMEOUT", defaultGitHubTimeout)
	if err != nil {
		return Config{}, err
	}

	databaseConfig, err := databaseConfigValue(lookup)
	if err != nil {
		return Config{}, err
	}
	redisConfig, redisPasswordFile, redisEnabled, err := redisConfigValue(lookup)
	if err != nil {
		return Config{}, err
	}
	trustedProxyCIDRs, err := trustedProxyCIDRsValue(lookup)
	if err != nil {
		return Config{}, err
	}

	return Config{
		AppEnv:                  appEnv,
		PublicBaseURL:           publicBaseURL,
		SessionIdleDuration:     sessionIdleDuration,
		SessionAbsoluteDuration: sessionAbsoluteDuration,
		CookieSecure:            cookieSecure,
		HTTPAddr:                addr,
		ReadHeaderTimeout:       readHeaderTimeout,
		ReadTimeout:             readTimeout,
		WriteTimeout:            writeTimeout,
		IdleTimeout:             idleTimeout,
		ShutdownTimeout:         shutdownTimeout,
		UploadDir:               uploadDir,
		MaxUploadBytes:          maxUploadBytes,
		MaxImagePixels:          maxImagePixels,
		MaxImageEdge:            maxImageEdge,
		GitHubAPIBaseURL:        githubAPIBaseURL,
		GitHubTimeout:           githubTimeout,
		Database:                databaseConfig,
		Redis:                   redisConfig,
		RedisPasswordFile:       redisPasswordFile,
		RedisEnabled:            redisEnabled,
		TrustedProxyCIDRs:       trustedProxyCIDRs,
	}, nil
}

func redisConfigValue(lookup func(string) (string, bool)) (cache.RedisConfig, string, bool, error) {
	addr, addrSet := lookup("REDIS_ADDR")
	urlValue, urlSet := lookup("REDIS_URL")
	passwordFile, passwordFileSet := lookup("REDIS_PASSWORD_FILE")
	dbValue, dbSet := lookup("REDIS_DB")
	if !addrSet && !urlSet && !passwordFileSet && !dbSet {
		return cache.RedisConfig{}, "", false, nil
	}
	if addrSet && urlSet {
		return cache.RedisConfig{}, "", false, fmt.Errorf("REDIS_ADDR and REDIS_URL cannot both be set")
	}
	if urlSet {
		parsed, err := url.Parse(urlValue)
		if err != nil || parsed.Scheme != "redis" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(urlValue, "#") || parsed.Opaque != "" {
			return cache.RedisConfig{}, "", false, fmt.Errorf("invalid REDIS_URL: must be a redis URL without credentials, query, or fragment")
		}
		addr = parsed.Host
		if parsed.Path != "" && parsed.Path != "/" {
			value := strings.TrimPrefix(parsed.Path, "/")
			if dbSet {
				return cache.RedisConfig{}, "", false, fmt.Errorf("REDIS_URL database conflicts with REDIS_DB")
			}
			if !isNonNegativeInteger(value) {
				return cache.RedisConfig{}, "", false, fmt.Errorf("invalid REDIS_URL database path: must be a non-negative integer")
			}
			dbValue = value
			dbSet = true
		}
	}
	if !addrSet && !urlSet {
		addr = "127.0.0.1:6379"
	} else if strings.TrimSpace(addr) == "" {
		return cache.RedisConfig{}, "", false, fmt.Errorf("REDIS_ADDR must not be empty")
	}
	if strings.TrimSpace(addr) != addr {
		return cache.RedisConfig{}, "", false, fmt.Errorf("REDIS_ADDR must not contain leading or trailing whitespace")
	}
	db := 0
	if dbSet {
		parsed, err := strconv.Atoi(dbValue)
		if err != nil || parsed < 0 {
			return cache.RedisConfig{}, "", false, fmt.Errorf("invalid REDIS_DB: must be a non-negative integer")
		}
		db = parsed
	}
	if passwordFileSet {
		if passwordFile == "" {
			return cache.RedisConfig{}, "", false, fmt.Errorf("REDIS_PASSWORD_FILE must not be empty")
		}
		if strings.TrimSpace(passwordFile) != passwordFile {
			return cache.RedisConfig{}, "", false, fmt.Errorf("REDIS_PASSWORD_FILE must not contain leading or trailing whitespace")
		}
	}
	return cache.RedisConfig{
		Addr:    addr,
		DB:      db,
		Prefix:  cache.DefaultPrefix,
		TTL:     cache.DefaultTTL,
		Timeout: cache.DefaultTimeout,
	}, passwordFile, true, nil
}

func trustedProxyCIDRsValue(lookup func(string) (string, bool)) ([]string, error) {
	value, ok := lookup("TRUSTED_PROXY_CIDRS")
	if !ok || value == "" {
		return nil, nil
	}

	entries := strings.Split(value, ",")
	cidrs := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS: entries must be non-empty CIDRs")
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q: must be a CIDR", entry)
		}
		canonical := network.String()
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		cidrs = append(cidrs, canonical)
	}
	return cidrs, nil
}

func isNonNegativeInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func boolValue(lookup func(string) (string, bool), key string, fallback bool) (bool, error) {
	value, err := configuredValue(lookup, key, strconv.FormatBool(fallback))
	if err != nil {
		return false, err
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: must be a boolean", key, value)
	}
	return parsed, nil
}

func validatePublicBaseURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return "", fmt.Errorf("invalid PUBLIC_BASE_URL %q: must be an absolute http or https origin", value)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid PUBLIC_BASE_URL %q: must use http or https", value)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func validateGitHubAPIBaseURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return "", fmt.Errorf("invalid GITHUB_API_BASE_URL %q: must be an absolute http or https origin", value)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid GITHUB_API_BASE_URL %q: must use http or https", value)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func databaseConfigValue(lookup func(string) (string, bool)) (database.Config, error) {
	host, err := requiredConfiguredValue(lookup, "DB_HOST")
	if err != nil {
		return database.Config{}, err
	}
	port, err := requiredConfiguredValue(lookup, "DB_PORT")
	if err != nil {
		return database.Config{}, err
	}
	name, err := requiredConfiguredValue(lookup, "DB_NAME")
	if err != nil {
		return database.Config{}, err
	}
	user, err := requiredConfiguredValue(lookup, "DB_USER")
	if err != nil {
		return database.Config{}, err
	}
	passwordFile, err := requiredConfiguredValue(lookup, "DB_PASSWORD_FILE")
	if err != nil {
		return database.Config{}, err
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return database.Config{}, fmt.Errorf("invalid DB_PORT %q: must be between 1 and 65535", port)
	}

	return database.Config{
		Host:         host,
		Port:         port,
		Name:         name,
		User:         user,
		PasswordFile: passwordFile,
	}, nil
}

func configuredValue(lookup func(string) (string, bool), key, fallback string) (string, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	if strings.TrimSpace(value) != value {
		return "", fmt.Errorf("%s must not contain leading or trailing whitespace", key)
	}
	return value, nil
}

func requiredConfiguredValue(lookup func(string) (string, bool), key string) (string, error) {
	value, ok := lookup(key)
	if !ok {
		return "", fmt.Errorf("%s must be set", key)
	}
	return configuredValue(lookup, key, value)
}

func durationValue(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	value, err := configuredValue(lookup, key, fallback.String())
	if err != nil {
		return 0, err
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a duration such as 5s", key, value)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be greater than zero", key, value)
	}
	return duration, nil
}

func positiveInt64Value(lookup func(string) (string, bool), key string, fallback int64) (int64, error) {
	value, err := configuredValue(lookup, key, strconv.FormatInt(fallback, 10))
	if err != nil {
		return 0, err
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("invalid %s %q: must be a positive decimal integer", key, value)
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive decimal integer in int64 range", key, value)
	}
	return parsed, nil
}

func validateHTTPAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must include a host and port (for example 127.0.0.1:8080): %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return fmt.Errorf("port %q must be a number between 0 and 65535", port)
	}
	return nil
}
