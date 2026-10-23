package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-openapi-client-go/client"
)

func TestGrafanaLookups(t *testing.T) {
	cases := map[string]struct {
		lookup func(*GrafanaClient) (string, error)
		path   string
		body   string
		want   string
	}{
		"team ID": {
			lookup: func(c *GrafanaClient) (string, error) { return c.GetTeam("42") },
			path:   "/api/teams/42",
			body:   `{"id":42,"name":"platform"}`,
			want:   "42",
		},
		"team name": {
			lookup: func(c *GrafanaClient) (string, error) { return c.GetTeam("platform") },
			path:   "/api/teams/search",
			body:   `{"teams":[{"id":42,"name":"platform"}]}`,
			want:   "42",
		},
		"role name": {
			lookup: func(c *GrafanaClient) (string, error) { return c.GetRoleUID("reader") },
			path:   "/api/access-control/roles",
			body:   `[{"name":"reader","uid":"role-uid"}]`,
			want:   "role-uid",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/teams/platform" {
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"message":"Team not found"}`)
					return
				}
				if r.URL.Path != tc.path {
					t.Errorf("unexpected request path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			api := client.NewHTTPClientWithConfig(nil, &client.TransportConfig{
				Host: strings.TrimPrefix(server.URL, "http://"), BasePath: "/api", Schemes: []string{"http"},
			})
			got, err := tc.lookup(NewGrafanaClient(api))
			if err != nil || got != tc.want {
				t.Fatalf("lookup = %q, %v; want %q, nil", got, err, tc.want)
			}
		})
	}
}
