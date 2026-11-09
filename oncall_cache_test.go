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
	c := newListCache[T](10*time.Minute, time.Minute)
	c.now = clock.Now
	return c
}

func TestListCache(t *testing.T) {
	t.Run("ReusesListWithinTTL", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := func() ([]string, error) { calls++; return []string{"a"}, nil }

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
		fetch := func() ([]string, error) { calls++; return []string{"a"}, nil }

		_, _ = c.get("k", fetch)
		clock.Advance(10 * time.Minute)
		_, _ = c.get("k", fetch)
		if calls != 2 {
			t.Errorf("fetch calls = %d, want 2", calls)
		}
	})

	t.Run("DoesNotCacheErrors", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := func() ([]string, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("429")
			}
			return []string{"a"}, nil
		}

		if _, err := c.get("k", fetch); err == nil {
			t.Fatal("first get: want error")
		}
		got, err := c.get("k", fetch)
		if err != nil || len(got) != 1 {
			t.Fatalf("second get = %v, %v; want [a], nil", got, err)
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

	t.Run("RefreshIsRateLimited", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(0, 0)}
		c := newTestCache[string](clock)
		calls := 0
		fetch := func() ([]string, error) { calls++; return []string{"a"}, nil }

		_, _ = c.get("k", fetch)
		clock.Advance(30 * time.Second)
		if _, refreshed, _ := c.refresh("k", fetch); refreshed {
			t.Error("refresh within minRefresh: refreshed = true, want false")
		}
		clock.Advance(30 * time.Second)
		if _, refreshed, _ := c.refresh("k", fetch); !refreshed {
			t.Error("refresh after minRefresh: refreshed = false, want true")
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

		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() { _, _ = c.get("k", fetch) })
		}
		// Let the goroutines reach the in-flight fetch before releasing it.
		time.Sleep(50 * time.Millisecond)
		close(release)
		wg.Wait()
		if n := calls.Load(); n != 1 {
			t.Errorf("fetch calls = %d, want 1", n)
		}
	})
}

// fakeOnCall serves paginated /api/v1/users/ and /api/v1/teams/ responses and
// counts the requests it receives.
type fakeOnCall struct {
	users    [][]*onCallAPI.User
	teams    [][]*onCallAPI.Team
	requests atomic.Int32
}

func (f *fakeOnCall) handler(t *testing.T) http.Handler {
	t.Helper()
	page := func(w http.ResponseWriter, r *http.Request, pages int, results any) {
		p := 1
		_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &p)
		var next *string
		if p < pages {
			n := fmt.Sprintf("%s?page=%d", r.URL.Path, p+1)
			next = &n
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"next": next, "results": results}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		p := 1
		_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &p)
		page(w, r, len(f.users), f.users[p-1])
	})
	teams := func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		p := 1
		_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &p)
		page(w, r, len(f.teams), f.teams[p-1])
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
	// provider config should list users (2 pages) and teams (1 page).
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

	// Another provider config gets its own lists.
	if _, err := NewOnCallClient(api, "other").GetUserID("one@example.com"); err != nil {
		t.Fatalf("other provider config: %v", err)
	}
	if n := fake.requests.Load(); n != 5 {
		t.Errorf("after other provider config: requests = %d, want 5", n)
	}

	// A user added after the list was cached is found once the list may be
	// refreshed, but repeated misses inside minRefresh do not refetch.
	fake.users[1] = append(fake.users[1], &onCallAPI.User{ID: "U3", Email: "new@example.com"})
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
