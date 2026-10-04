package cache

import (
	"context"
	"testing"
	"time"
)

func TestRedisStoreWithoutClientFailsClosed(t *testing.T) {
	store := NewRedisStoreWithClient(nil, "test:", time.Second, time.Millisecond)
	if _, err := store.CurrentVersion(context.Background()); err == nil {
		t.Fatal("CurrentVersion() unexpectedly succeeded without a client")
	}
	if _, _, err := store.Get(context.Background(), "1", "/"); err == nil {
		t.Fatal("Get() unexpectedly succeeded without a client")
	}
	if err := store.Set(context.Background(), "1", "/", Page{Body: []byte("x"), ETag: `"x"`, ContentType: "text/html"}); err == nil {
		t.Fatal("Set() unexpectedly succeeded without a client")
	}
	if err := store.Invalidate(context.Background()); err == nil {
		t.Fatal("Invalidate() unexpectedly succeeded without a client")
	}
}

func TestRedisStoreDefaults(t *testing.T) {
	store := NewRedisStoreWithClient(nil, "", 0, 0)
	if store.prefix != DefaultPrefix || store.ttl != DefaultTTL || store.timeout != DefaultTimeout {
		t.Fatalf("defaults = prefix %q ttl %s timeout %s", store.prefix, store.ttl, store.timeout)
	}
}
