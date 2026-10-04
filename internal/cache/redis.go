package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DefaultPrefix  = "mywebsite:public:"
	DefaultTTL     = 60 * time.Second
	DefaultTimeout = 100 * time.Millisecond
)

// RedisConfig describes the optional Redis page-cache connection. Password is
// supplied by the application after it reads the configured password file.
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	Prefix   string
	TTL      time.Duration
	Timeout  time.Duration
}

// RedisStore implements PageCache with a go-redis client. It does not ping at
// startup: a missing or restarting Redis only affects acceleration and is
// handled per request by the middleware.
type RedisStore struct {
	client  redis.UniversalClient
	prefix  string
	ttl     time.Duration
	timeout time.Duration
}

// NewRedisStore creates a store using a standard Redis client. The caller
// owns the client's lifecycle and must close it during server shutdown.
func NewRedisStore(config RedisConfig) *RedisStore {
	addr := strings.TrimSpace(config.Addr)
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	prefix := config.Prefix
	if strings.TrimSpace(prefix) == "" {
		prefix = DefaultPrefix
	}
	ttl := config.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     config.Password,
		DB:           config.DB,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	})
	return NewRedisStoreWithClient(client, prefix, ttl, timeout)
}

// NewRedisStoreWithClient is injectable for tests and integrations that own a
// shared go-redis client.
func NewRedisStoreWithClient(client redis.UniversalClient, prefix string, ttl, timeout time.Duration) *RedisStore {
	if strings.TrimSpace(prefix) == "" {
		prefix = DefaultPrefix
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &RedisStore{client: client, prefix: prefix, ttl: ttl, timeout: timeout}
}

func (store *RedisStore) CurrentVersion(ctx context.Context) (string, error) {
	if store == nil || store.client == nil {
		return "", fmt.Errorf("public cache Redis client is nil")
	}
	ctx, cancel := store.withTimeout(ctx)
	defer cancel()
	value, err := store.client.Get(ctx, store.versionKey()).Result()
	if err == redis.Nil {
		if setErr := store.client.Set(ctx, store.versionKey(), "1", 0).Err(); setErr != nil {
			return "", fmt.Errorf("initialize public cache version: %w", setErr)
		}
		return "1", nil
	}
	if err != nil {
		return "", fmt.Errorf("read public cache version: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("public cache version is empty")
	}
	return value, nil
}

func (store *RedisStore) Get(ctx context.Context, version, requestTarget string) (Page, bool, error) {
	if store == nil || store.client == nil {
		return Page{}, false, fmt.Errorf("public cache Redis client is nil")
	}
	if strings.TrimSpace(version) == "" {
		return Page{}, false, fmt.Errorf("public cache version is empty")
	}
	ctx, cancel := store.withTimeout(ctx)
	defer cancel()
	value, err := store.client.Get(ctx, PageKey(store.prefix, version, requestTarget)).Bytes()
	if err == redis.Nil {
		return Page{}, false, nil
	}
	if err != nil {
		return Page{}, false, fmt.Errorf("read public cache page: %w", err)
	}
	var page Page
	if err := json.Unmarshal(value, &page); err != nil {
		return Page{}, false, fmt.Errorf("decode public cache page: %w", err)
	}
	if err := ValidatePage(page); err != nil {
		return Page{}, false, err
	}
	return page, true, nil
}

func (store *RedisStore) Set(ctx context.Context, version, requestTarget string, page Page) error {
	if store == nil || store.client == nil {
		return fmt.Errorf("public cache Redis client is nil")
	}
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("public cache version is empty")
	}
	if err := ValidatePage(page); err != nil {
		return err
	}
	payload, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("encode public cache page: %w", err)
	}
	ctx, cancel := store.withTimeout(ctx)
	defer cancel()
	if err := store.client.Set(ctx, PageKey(store.prefix, version, requestTarget), payload, store.ttl).Err(); err != nil {
		return fmt.Errorf("write public cache page: %w", err)
	}
	return nil
}

func (store *RedisStore) Invalidate(ctx context.Context) error {
	if store == nil || store.client == nil {
		return fmt.Errorf("public cache Redis client is nil")
	}
	ctx, cancel := store.withTimeout(ctx)
	defer cancel()
	if _, err := store.client.Incr(ctx, store.versionKey()).Result(); err != nil {
		return fmt.Errorf("increment public cache version: %w", err)
	}
	return nil
}

func (store *RedisStore) versionKey() string {
	return strings.TrimRight(store.prefix, ":") + ":version"
}

func (store *RedisStore) withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, store.timeout)
}

// Close releases the underlying Redis client when the store owns it. Calling
// Close on a shared client is left to the caller of NewRedisStoreWithClient.
func (store *RedisStore) Close() error {
	if store == nil || store.client == nil {
		return nil
	}
	if client, ok := store.client.(*redis.Client); ok {
		return client.Close()
	}
	return nil
}
