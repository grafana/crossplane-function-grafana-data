package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	onCallAPI "github.com/grafana/amixr-api-go-client"
)

// fakeClock is a manually advanced clock for listCache tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestCache[T any](clock *fakeClock) *listCache[T] {
	c := newListCache[T](10*time.Minute, time.Minute, 30*time.Second)
	c.now = clock.Now
	return c
}

// countingFetch returns a fetch func that counts calls and returns the result
// of next for each call number (starting at 1).
func countingFetch(calls *int, next func(n int) ([]string, error)) func() ([]string, error) {
	return func() ([]string, error) {
		*calls++
		return next(*calls)
	}
}

func always(items ...string) func(int) ([]string, error) {
	return func(int) ([]string, error) { return items, nil }
}

var errRateLimited = errors.New("429 Too Many Requests")

func TestListCache(t *testing.T) {
	t.Run("ReusesListWithinTTL", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, always("a"))

		for range 3 {
			if _, err := c.get("k", fetch); err != nil {
				t.Fatalf("get: %v", err)
			}
			clock.Advance(time.Minute)
		}
		if calls != 1 {
			t.Errorf("fetch calls = %d, want 1", calls)
		}
	})

	t.Run("RefetchesAfterTTL", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, always("a"))

		_, _ = c.get("k", fetch)
		clock.Advance(10 * time.Minute)
		_, _ = c.get("k", fetch)
		if calls != 2 {
			t.Errorf("fetch calls = %d, want 2", calls)
		}
	})

	t.Run("KeysAreIsolated", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		a, _ := c.get("a", func() ([]string, error) { return []string{"from-a"}, nil })
		b, _ := c.get("b", func() ([]string, error) { return []string{"from-b"}, nil })
		if a[0] != "from-a" || b[0] != "from-b" {
			t.Errorf("got a=%v b=%v", a, b)
		}
	})

	t.Run("FirstFetchErrorIsReturned", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, func(int) ([]string, error) { return nil, errRateLimited })

		if _, err := c.get("k", fetch); !errors.Is(err, errRateLimited) {
			t.Fatalf("get: err = %v, want %v", err, errRateLimited)
		}
	})

	t.Run("FailureBackoffWithoutList", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, func(n int) ([]string, error) {
			if n == 1 {
				return nil, errRateLimited
			}
			return []string{"a"}, nil
		})

		_, _ = c.get("k", fetch)
		clock.Advance(10 * time.Second)
		if _, err := c.get("k", fetch); !errors.Is(err, errRateLimited) {
			t.Errorf("get during backoff: err = %v, want last error", err)
		}
		if calls != 1 {
			t.Errorf("fetch calls during backoff = %d, want 1", calls)
		}
		clock.Advance(20 * time.Second)
		if got, err := c.get("k", fetch); err != nil || len(got) != 1 {
			t.Errorf("get after backoff = %v, %v; want [a], nil", got, err)
		}
		if calls != 2 {
			t.Errorf("fetch calls after backoff = %d, want 2", calls)
		}
	})

	t.Run("ServesPreviousListWhenRefetchFails", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, func(n int) ([]string, error) {
			switch n {
			case 1:
				return []string{"old"}, nil
			case 2:
				return nil, errRateLimited
			default:
				return []string{"new"}, nil
			}
		})

		_, _ = c.get("k", fetch)
		clock.Advance(10 * time.Minute) // TTL expired, refetch fails
		if got, err := c.get("k", fetch); err != nil || got[0] != "old" {
			t.Fatalf("get after failed refetch = %v, %v; want [old], nil", got, err)
		}
		clock.Advance(10 * time.Second) // inside backoff: no new fetch
		if got, err := c.get("k", fetch); err != nil || got[0] != "old" {
			t.Fatalf("get during backoff = %v, %v; want [old], nil", got, err)
		}
		if calls != 2 {
			t.Errorf("fetch calls during backoff = %d, want 2", calls)
		}
		clock.Advance(20 * time.Second) // backoff over: fetch again
		if got, err := c.get("k", fetch); err != nil || got[0] != "new" {
			t.Fatalf("get after backoff = %v, %v; want [new], nil", got, err)
		}
	})

	t.Run("RefreshIsRateLimited", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, always("a"))

		_, _ = c.get("k", fetch)
		clock.Advance(30 * time.Second)
		_, _ = c.refresh("k", fetch)
		if calls != 1 {
			t.Errorf("refresh within minRefresh: fetch calls = %d, want 1", calls)
		}
		clock.Advance(30 * time.Second)
		_, _ = c.refresh("k", fetch)
		if calls != 2 {
			t.Errorf("refresh after minRefresh: fetch calls = %d, want 2", calls)
		}
	})

	t.Run("RefreshReturnsNewerListFromAnotherCaller", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := countingFetch(&calls, func(n int) ([]string, error) {
			if n == 1 {
				return []string{"old"}, nil
			}
			return []string{"new"}, nil
		})

		_, _ = c.get("k", fetch)
		clock.Advance(5 * time.Minute)
		_, _ = c.refresh("k", fetch) // another caller refreshes
		clock.Advance(time.Second)
		got, err := c.refresh("k", fetch) // inside minRefresh: no fetch, but the newer list
		if err != nil || got[0] != "new" {
			t.Errorf("refresh = %v, %v; want [new], nil", got, err)
		}
		if calls != 2 {
			t.Errorf("fetch calls = %d, want 2", calls)
		}
	})

	t.Run("ConcurrentGetsShareOneFetch", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		var calls atomic.Int32
		release := make(chan struct{})
		fetch := func() ([]string, error) {
			calls.Add(1)
			<-release
			return []string{"a"}, nil
		}

		// Callers either join the in-flight fetch or, if they arrive after
		// it finished, find the stored list; either way there is one fetch.
		var wg sync.WaitGroup
		for i := range 20 {
			if i == 10 {
				close(release)
			}
			wg.Go(func() { _, _ = c.get("k", fetch) })
		}
		wg.Wait()
		if n := calls.Load(); n != 1 {
			t.Errorf("fetch calls = %d, want 1", n)
		}
	})
}

// fakeOnCall serves paginated /api/v1/users/ and /api/v1/teams responses and
// counts the requests it receives.
type fakeOnCall struct {
	mu       sync.Mutex
	users    [][]*onCallAPI.User
	teams    [][]*onCallAPI.Team
	requests atomic.Int32
}

func (f *fakeOnCall) addUser(page int, u *onCallAPI.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[page] = append(f.users[page], u)
}

func (f *fakeOnCall) handler(t *testing.T) http.Handler {
	t.Helper()
	write := func(w http.ResponseWriter, r *http.Request, p, pages int, results any) {
		var next *string
		if p < pages {
			n := fmt.Sprintf("%s?page=%d", r.URL.Path, p+1)
			next = &n
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"next": next, "results": results}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
	page := func(r *http.Request) int {
		p := 1
		_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &p)
		return p
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		p := page(r)
		write(w, r, p, len(f.users), f.users[p-1])
	})
	teams := func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		p := page(r)
		write(w, r, p, len(f.teams), f.teams[p-1])
	}
	mux.HandleFunc("/api/v1/teams", teams)
	mux.HandleFunc("/api/v1/teams/", teams)
	return mux
}

// useTestCaches swaps the shared caches for ones driven by clock.
func useTestCaches(t *testing.T, clock *fakeClock) {
	t.Helper()
	users, teams := onCallUsersCache, onCallTeamsCache
	onCallUsersCache = newTestCache[*onCallAPI.User](clock)
	onCallTeamsCache = newTestCache[*onCallAPI.Team](clock)
	t.Cleanup(func() { onCallUsersCache, onCallTeamsCache = users, teams })
}

func TestOnCallClientSharesListsAcrossRuns(t *testing.T) {
	fake := &fakeOnCall{
		users: [][]*onCallAPI.User{
			{{ID: "U1", Email: "one@example.com"}},
			{{ID: "U2", Email: "two@example.com"}},
		},
		teams: [][]*onCallAPI.Team{
			{{ID: "T1", Name: "team-one"}},
		},
	}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	api, err := onCallAPI.New(srv.URL, "token")
	if err != nil {
		t.Fatalf("onCallAPI.New: %v", err)
	}

	clock := &fakeClock{now: time.Unix(0, 0)}
	useTestCaches(t, clock)

	// Each function run builds a new OnCallClient. Only the first run for a
	// cache key should list users (2 pages) and teams (1 page).
	for run := range 5 {
		c := NewOnCallClient(api, "pc")
		if id, err := c.GetUserID("two@example.com"); err != nil || id != "U2" {
			t.Fatalf("run %d: GetUserID = %q, %v; want U2", run, id, err)
		}
		if id, err := c.GetTeamID("team-one"); err != nil || id != "T1" {
			t.Fatalf("run %d: GetTeamID = %q, %v; want T1", run, id, err)
		}
	}
	if n := fake.requests.Load(); n != 3 {
		t.Errorf("after 5 runs: requests = %d, want 3", n)
	}

	// Another cache key gets its own lists.
	if _, err := NewOnCallClient(api, "other").GetUserID("one@example.com"); err != nil {
		t.Fatalf("other cache key: %v", err)
	}
	if n := fake.requests.Load(); n != 5 {
		t.Errorf("after other cache key: requests = %d, want 5", n)
	}

	// A user added after the list was cached: repeated misses inside
	// minRefresh do not refetch, the first miss after it does.
	fake.addUser(1, &onCallAPI.User{ID: "U3", Email: "new@example.com"})
	for range 3 {
		if _, err := NewOnCallClient(api, "pc").GetUserID("new@example.com"); err == nil {
			t.Fatal("new user inside minRefresh: want not found")
		}
	}
	if n := fake.requests.Load(); n != 5 {
		t.Errorf("misses inside minRefresh: requests = %d, want 5", n)
	}
	clock.Advance(time.Minute)
	if id, err := NewOnCallClient(api, "pc").GetUserID("new@example.com"); err != nil || id != "U3" {
		t.Fatalf("new user after minRefresh: GetUserID = %q, %v; want U3", id, err)
	}
	if n := fake.requests.Load(); n != 7 {
		t.Errorf("after refresh: requests = %d, want 7", n)
	}
}

func TestOnCallClientUsesListRefreshedByAnotherRun(t *testing.T) {
	fake := &fakeOnCall{
		users: [][]*onCallAPI.User{{{ID: "U1", Email: "one@example.com"}}},
		teams: [][]*onCallAPI.Team{{}},
	}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	api, err := onCallAPI.New(srv.URL, "token")
	if err != nil {
		t.Fatalf("onCallAPI.New: %v", err)
	}
	clock := &fakeClock{now: time.Unix(0, 0)}
	useTestCaches(t, clock)

	// Run A loads the list before the new user exists.
	a := NewOnCallClient(api, "pc")
	if _, err := a.GetUserID("one@example.com"); err != nil {
		t.Fatalf("run A: %v", err)
	}

	// The user is added; run B misses and refreshes the shared list.
	fake.addUser(0, &onCallAPI.User{ID: "U2", Email: "new@example.com"})
	clock.Advance(5 * time.Minute)
	if id, err := NewOnCallClient(api, "pc").GetUserID("new@example.com"); err != nil || id != "U2" {
		t.Fatalf("run B: GetUserID = %q, %v; want U2", id, err)
	}

	// Run A misses moments later. The shared list is too new to refetch, but
	// it already has the user, so A must find it there.
	clock.Advance(time.Second)
	if id, err := a.GetUserID("new@example.com"); err != nil || id != "U2" {
		t.Errorf("run A after B refreshed: GetUserID = %q, %v; want U2", id, err)
	}
}
