package app

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"mywebsite/internal/database"
)

const (
	defaultHTTPAddr            = "127.0.0.1:8080"
	defaultReadHeaderTimeout   = 5 * time.Second
	defaultReadTimeout         = 15 * time.Second
	defaultWriteTimeout        = 15 * time.Second
	defaultIdleTimeout         = 60 * time.Second
	defaultShutdownTimeout     = 30 * time.Second
	maxGracefulShutdownTimeout = 30 * time.Second
)

// Config contains the HTTP and database settings needed to run the server.
type Config struct {
	HTTPAddr          string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	Database          database.Config
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

	databaseConfig, err := databaseConfigValue(lookup)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:          addr,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ShutdownTimeout:   shutdownTimeout,
		Database:          databaseConfig,
	}, nil
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
