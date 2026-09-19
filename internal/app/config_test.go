package app

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfigDefaults(t *testing.T) {
	config, err := ParseConfig(emptyLookup)
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
}

func TestParseConfigCustomValues(t *testing.T) {
	values := map[string]string{
		"HTTP_ADDR":                "127.0.0.1:9090",
		"HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_READ_TIMEOUT":        "3s",
		"HTTP_WRITE_TIMEOUT":       "4s",
		"HTTP_IDLE_TIMEOUT":        "5s",
		"HTTP_SHUTDOWN_TIMEOUT":    "6s",
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
}

func TestParseConfigRejectsInvalidAddress(t *testing.T) {
	values := map[string]string{"HTTP_ADDR": "not-an-address"}
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR") {
		t.Fatalf("ParseConfig() error = %v, want an HTTP_ADDR error", err)
	}
}

func TestParseConfigRejectsInvalidTimeout(t *testing.T) {
	values := map[string]string{"HTTP_READ_TIMEOUT": "not-a-duration"}
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "HTTP_READ_TIMEOUT") {
		t.Fatalf("ParseConfig() error = %v, want an HTTP_READ_TIMEOUT error", err)
	}
}

func TestParseConfigLimitsShutdownTimeout(t *testing.T) {
	values := map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "31s"}
	_, err := ParseConfig(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("ParseConfig() error = %v, want a shutdown limit error", err)
	}
}

func emptyLookup(string) (string, bool) {
	return "", false
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
