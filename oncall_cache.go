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

	// onCallListFailureBackoff is how long a failed fetch (for example a 429)
	// blocks further fetches for the same key, so a rate-limited API is not
	// hit again by every function run.
	onCallListFailureBackoff = 30 * time.Second

	// onCallListMaxStale is how old a list may get while fetches keep failing
	// before it stops being served. After that lookups fail with the fetch
	// error, so a persistent problem (for example a revoked token) surfaces
	// instead of being hidden behind an old list.
	onCallListMaxStale = time.Hour
)

var onCallListTimings = cacheTimings{
	TTL:            onCallListTTL,
	MinRefresh:     onCallListMinRefresh,
	FailureBackoff: onCallListFailureBackoff,
	MaxStale:       onCallListMaxStale,
}

// The function runs as a long-lived server, so these caches are shared by all
// function runs in the process. Without them every run fetched every page of
// the OnCall user and team lists, which got rate limited (HTTP 429) once
// enough OnCall resources were reconciled.
var (
	onCallUsersCache = newListCache[*onCallAPI.User](onCallListTimings)
	onCallTeamsCache = newListCache[*onCallAPI.Team](onCallListTimings)
)

// cacheTimings configures a listCache.
type cacheTimings struct {
	TTL            time.Duration // reuse a list this long before fetching it again
	MinRefresh     time.Duration // minimum list age before a lookup miss may refetch
	FailureBackoff time.Duration // no new fetch this long after a failed one
	MaxStale       time.Duration // stop serving a list this old when fetches fail
}

type listEntry[T any] struct {
	items     []T
	fetchedAt time.Time // zero if no fetch has succeeded yet

	failedAt time.Time // time of the last failed fetch, zero if none
	err      error     // error of the last failed fetch
}

// listCache caches full lists per key:
//   - a list is reused for TTL, then fetched again
//   - concurrent fetches for the same key share one request
//   - if a fetch fails, the previous list keeps being served until it is
//     MaxStale old, and no new fetch is attempted for FailureBackoff
type listCache[T any] struct {
	timings cacheTimings
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]listEntry[T]
	group   singleflight.Group
}

func newListCache[T any](timings cacheTimings) *listCache[T] {
	return &listCache[T]{
		timings: timings,
		now:     time.Now,
		entries: map[string]listEntry[T]{},
	}
}

// get returns the list for key, fetching it if it is missing or older than
// TTL.
func (c *listCache[T]) get(key string, fetch func() ([]T, error)) ([]T, error) {
	return c.load(key, c.timings.TTL, fetch)
}

// refresh returns the list for key, fetching it again unless it is younger
// than MinRefresh. Callers should retry their lookup against the returned
// list either way: another run may have refreshed it moments ago.
func (c *listCache[T]) refresh(key string, fetch func() ([]T, error)) ([]T, error) {
	return c.load(key, c.timings.MinRefresh, fetch)
}

// load returns the cached list if it is younger than maxAge, and fetches it
// otherwise.
func (c *listCache[T]) load(key string, maxAge time.Duration, fetch func() ([]T, error)) ([]T, error) {
	if items, ok := c.cached(key, maxAge); ok {
		return items, nil
	}
	v, err, _ := c.group.Do(key, func() (any, error) {
		// Another caller may have finished a fetch since the check above.
		if items, ok := c.cached(key, maxAge); ok {
			return items, nil
		}
		return c.fetch(key, fetch)
	})
	if err != nil {
		return nil, err
	}
	return v.([]T), nil
}

// cached returns the list for key if it was fetched less than maxAge ago or,
// during the failure backoff, if it may still be served stale.
func (c *listCache[T]) cached(key string, maxAge time.Duration) ([]T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.fetchedAt.IsZero() {
		return nil, false
	}
	now := c.now()
	if now.Sub(e.fetchedAt) < maxAge {
		return e.items, true
	}
	if c.inBackoff(e, now) && c.usableStale(e, now) {
		return e.items, true
	}
	return nil, false
}

// fetch runs fetch and stores the result. On failure it serves the previous
// list while it is younger than MaxStale, and returns the error otherwise.
// During the failure backoff it returns the last error without fetching.
func (c *listCache[T]) fetch(key string, fetch func() ([]T, error)) ([]T, error) {
	c.mu.Lock()
	e := c.entries[key]
	backoff := c.inBackoff(e, c.now())
	c.mu.Unlock()
	if backoff {
		// No usable list, otherwise cached() would have returned it.
		return nil, e.err
	}

	items, err := fetch()

	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	e = c.entries[key]
	if err != nil {
		e.failedAt, e.err = now, err
		c.entries[key] = e
		if c.usableStale(e, now) {
			return e.items, nil
		}
		return nil, err
	}
	c.entries[key] = listEntry[T]{items: items, fetchedAt: now}
	return items, nil
}

func (c *listCache[T]) inBackoff(e listEntry[T], now time.Time) bool {
	return !e.failedAt.IsZero() && now.Sub(e.failedAt) < c.timings.FailureBackoff
}

// usableStale reports whether e holds a list young enough to serve when
// fetching fails.
func (c *listCache[T]) usableStale(e listEntry[T], now time.Time) bool {
	return !e.fetchedAt.IsZero() && now.Sub(e.fetchedAt) < c.timings.MaxStale
}
