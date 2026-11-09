package main

import (
	"sync"
	"time"

	onCallAPI "github.com/grafana/amixr-api-go-client"
	"golang.org/x/sync/singleflight"
)

const (
	// onCallListTTL is how long a fetched OnCall user or team list is reused
	// before the next lookup fetches it again.
	onCallListTTL = 10 * time.Minute

	// onCallListMinRefresh is the minimum age of a cached list before a lookup
	// miss (for example a user added in the last few minutes) may force an
	// early refresh. It stops repeated misses, such as a typo in a manifest,
	// from fetching the full list on every function run.
	onCallListMinRefresh = time.Minute
)

// The function runs as a long-lived server, so these caches are shared by all
// function runs in the process. Without them every run fetched every page of
// the OnCall user and team lists, which got rate limited (HTTP 429) once
// enough OnCall resources were reconciled.
var (
	onCallUsersCache = newListCache[*onCallAPI.User](onCallListTTL, onCallListMinRefresh)
	onCallTeamsCache = newListCache[*onCallAPI.Team](onCallListTTL, onCallListMinRefresh)
)

type listEntry[T any] struct {
	items     []T
	fetchedAt time.Time
}

// listCache caches full lists per key, refetching them after ttl. Concurrent
// fetches for the same key share one request.
type listCache[T any] struct {
	ttl        time.Duration
	minRefresh time.Duration
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]listEntry[T]
	group   singleflight.Group
}

func newListCache[T any](ttl, minRefresh time.Duration) *listCache[T] {
	return &listCache[T]{
		ttl:        ttl,
		minRefresh: minRefresh,
		now:        time.Now,
		entries:    map[string]listEntry[T]{},
	}
}

// get returns the cached list for key, fetching it if it is missing or older
// than the cache TTL.
func (c *listCache[T]) get(key string, fetch func() ([]T, error)) ([]T, error) {
	if items, ok := c.fresh(key, c.ttl); ok {
		return items, nil
	}
	return c.fetch(key, fetch)
}

// refresh fetches the list for key again, unless the cached list is younger
// than minRefresh. It reports whether a new list was fetched.
func (c *listCache[T]) refresh(key string, fetch func() ([]T, error)) ([]T, bool, error) {
	if items, ok := c.fresh(key, c.minRefresh); ok {
		return items, false, nil
	}
	items, err := c.fetch(key, fetch)
	return items, err == nil, err
}

func (c *listCache[T]) fresh(key string, maxAge time.Duration) ([]T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().Sub(e.fetchedAt) >= maxAge {
		return nil, false
	}
	return e.items, true
}

func (c *listCache[T]) fetch(key string, fetch func() ([]T, error)) ([]T, error) {
	v, err, _ := c.group.Do(key, func() (any, error) {
		items, err := fetch()
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.entries[key] = listEntry[T]{items: items, fetchedAt: c.now()}
		c.mu.Unlock()
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]T), nil
}
