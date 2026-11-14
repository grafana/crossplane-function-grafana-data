package clients

import (
	"strings"
	"testing"
)

func TestCacheKey(t *testing.T) {
	cfg := map[string]any{"oncall_url": "https://oncall.example.com", "oncall_access_token": "secret-a"}

	k := cacheKey("pc", cfg)
	if !strings.HasPrefix(k, "pc/") {
		t.Errorf("cacheKey = %q, want prefix %q", k, "pc/")
	}
	if strings.Contains(k, "secret-a") {
		t.Errorf("cacheKey = %q contains the credential", k)
	}
	if again := cacheKey("pc", map[string]any{"oncall_access_token": "secret-a", "oncall_url": "https://oncall.example.com"}); again != k {
		t.Errorf("cacheKey is not stable across map order: %q != %q", again, k)
	}
	if other := cacheKey("pc", map[string]any{"oncall_url": "https://oncall.example.com", "oncall_access_token": "secret-b"}); other == k {
		t.Error("cacheKey did not change when the token changed")
	}
	if other := cacheKey("pc2", cfg); other == k {
		t.Error("cacheKey did not change with the provider config name")
	}
}
