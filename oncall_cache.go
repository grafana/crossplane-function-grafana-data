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
)

// The function runs as a long-lived server, so these caches are shared by all
// function runs in the process. Without them every run fetched every page of
// the OnCall user and team lists, which got rate limited (HTTP 429) once
// enough OnCall resources were reconciled.
var (
	onCallUsersCache = newListCache[*onCallAPI.User](onCallListTTL, onCallListMinRefresh, onCallListFailureBackoff)
	onCallTeamsCache = newListCache[*onCallAPI.Team](onCallListTTL, onCallListMinRefresh, onCallListFailureBackoff)
)

type listEntry[T any] struct {
	items     []T
	fetchedAt time.Time // zero if no fetch has succeeded yet

	failedAt time.Time // time of the last failed fetch, zero if none
	err      error     // error of the last failed fetch
}

// listCache caches full lists per key:
//   - a list is reused for ttl, then fetched again
//   - concurrent fetches for the same key share one request
//   - if a fetch fails, the previous list (if any) keeps being served, and no
//     new fetch is attempted for failureBackoff
type listCache[T any] struct {
	ttl            time.Duration
	minRefresh     time.Duration
	failureBackoff time.Duration
	now            func() time.Time

	mu      sync.Mutex
	entries map[string]listEntry[T]
	group   singleflight.Group
}

func newListCache[T any](ttl, minRefresh, failureBackoff time.Duration) *listCache[T] {
	return &listCache[T]{
		ttl:            ttl,
		minRefresh:     minRefresh,
		failureBackoff: failureBackoff,
		now:            time.Now,
		entries:        map[string]listEntry[T]{},
	}
}

// get returns the list for key, fetching it if it is missing or older than
// ttl.
func (c *listCache[T]) get(key string, fetch func() ([]T, error)) ([]T, error) {
	return c.load(key, c.ttl, fetch)
}

// refresh returns the list for key, fetching it again unless it is younger
// than minRefresh. Callers should retry their lookup against the returned
// list either way: another run may have refreshed it moments ago.
func (c *listCache[T]) refresh(key string, fetch func() ([]T, error)) ([]T, error) {
	return c.load(key, c.minRefresh, fetch)
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

// cached returns the list for key if it was fetched less than maxAge ago, or,
// during the failure backoff, the last good list (or the last error).
func (c *listCache[T]) cached(key string, maxAge time.Duration) ([]T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	now := c.now()
	if !e.fetchedAt.IsZero() && now.Sub(e.fetchedAt) < maxAge {
		return e.items, true
	}
	if !e.failedAt.IsZero() && now.Sub(e.failedAt) < c.failureBackoff && !e.fetchedAt.IsZero() {
		return e.items, true
	}
	return nil, false
}

// fetch runs fetch and stores the result. On failure it keeps the previous
// list and returns it if there is one; otherwise it returns the error, or the
// last error while the failure backoff is active.
func (c *listCache[T]) fetch(key string, fetch func() ([]T, error)) ([]T, error) {
	c.mu.Lock()
	e := c.entries[key]
	backoff := !e.failedAt.IsZero() && c.now().Sub(e.failedAt) < c.failureBackoff
	c.mu.Unlock()
	if backoff {
		// No previous list, otherwise cached() would have returned it.
		return nil, e.err
	}

	items, err := fetch()

	c.mu.Lock()
	defer c.mu.Unlock()
	e = c.entries[key]
	if err != nil {
		e.failedAt, e.err = c.now(), err
		c.entries[key] = e
		if !e.fetchedAt.IsZero() {
			return e.items, nil
		}
		return nil, err
	}
	c.entries[key] = listEntry[T]{items: items, fetchedAt: c.now()}
	return items, nil
}
