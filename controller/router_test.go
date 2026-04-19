package controller

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// pathTypePtr returns a pointer to a PathType value.
func pathTypePtr(pt networkingv1.PathType) *networkingv1.PathType {
	return &pt
}

// mustAddRoute calls AddRoute and fails the test immediately on error.
func mustAddRoute(t *testing.T, r *Router, host, path string, pt *networkingv1.PathType, key string, port int32, algo string) {
	t.Helper()
	if err := r.AddRoute(host, path, pt, key, port, algo); err != nil {
		t.Fatalf("AddRoute(%q, %q) unexpected error: %v", host, path, err)
	}
}

// makeIngress builds a minimal Ingress object for RemoveRoute.
func makeIngress(host string, paths ...string) *networkingv1.Ingress {
	httpPaths := make([]networkingv1.HTTPIngressPath, 0, len(paths))
	for _, p := range paths {
		httpPaths = append(httpPaths, networkingv1.HTTPIngressPath{Path: p})
	}
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: httpPaths,
						},
					},
				},
			},
		},
	}
}

func TestRouter_BasicPrefixMatching(t *testing.T) {
	cases := []struct {
		name      string
		reqPath   string
		wantKey   string
		wantMatch bool
	}{
		{"exact registered path", "/api", "svc-api", true},
		{"one level deeper", "/api/foo", "svc-api", true},
		{"multi level deeper", "/api/v2/bar", "svc-api", true},
		{"unrelated path", "/other", "", false},
		// The segment trie matches on path segments, not byte prefixes.
		// "/api" does NOT match "/apiv2" because "apiv2" != "api".
		{"partial segment does not share path prefix", "/apiv2", "", false},
	}

	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api", 8080, "round_robin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match("example.com", tc.reqPath)
			if tc.wantMatch {
				if got == nil {
					t.Fatalf("Match(%q) = nil, want non-nil", tc.reqPath)
				}
				if got.Key != tc.wantKey {
					t.Errorf("Match(%q).Key = %q, want %q", tc.reqPath, got.Key, tc.wantKey)
				}
			} else {
				if got != nil {
					t.Errorf("Match(%q) = %+v, want nil", tc.reqPath, got)
				}
			}
		})
	}
}

func TestRouter_LongestPrefixWins(t *testing.T) {
	cases := []struct {
		name    string
		reqPath string
		wantKey string
	}{
		{"shallow path matches short prefix", "/api/v1/users", "svc-api"},
		{"deeper path matches longer prefix", "/api/v2/something", "svc-api-v2"},
		{"nested path under longer prefix", "/api/v2/nested/deep", "svc-api-v2"},
	}

	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api", 8080, "round_robin")
	mustAddRoute(t, r, "example.com", "/api/v2", pathTypePtr(networkingv1.PathTypePrefix), "svc-api-v2", 9090, "round_robin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match("example.com", tc.reqPath)
			if got == nil {
				t.Fatalf("Match(%q) = nil, want key=%q", tc.reqPath, tc.wantKey)
			}
			if got.Key != tc.wantKey {
				t.Errorf("Match(%q).Key = %q, want %q", tc.reqPath, got.Key, tc.wantKey)
			}
		})
	}
}

func TestRouter_ExactMatch(t *testing.T) {
	cases := []struct {
		name      string
		reqPath   string
		wantMatch bool
	}{
		{"exact registered path matches", "/health", true},
		{"trailing segment does not match", "/health/check", false},
		{"prefix of registered path does not match", "/healt", false},
		{"longer unrelated path does not match", "/healthz", false},
	}

	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/health", pathTypePtr(networkingv1.PathTypeExact), "svc-health", 8081, "round_robin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match("example.com", tc.reqPath)
			if tc.wantMatch && got == nil {
				t.Fatalf("Match(%q) = nil, want non-nil", tc.reqPath)
			}
			if !tc.wantMatch && got != nil {
				t.Errorf("Match(%q) = %+v, want nil", tc.reqPath, got)
			}
		})
	}
}

func TestRouter_ExactMatchVsPrefix(t *testing.T) {
	// Exact "/health" matches only "/health".
	// Prefix "/" matches "/", "/other", etc.
	//
	// The segment trie walks path segments and tracks the deepest Prefix match.
	// For "/health/check", the trie walks to the "health" node (Exact, not Prefix),
	// then can't find "check" child, so it falls back to the deepest Prefix: "/".
	cases := []struct {
		name      string
		reqPath   string
		wantMatch bool
		wantKey   string
	}{
		{"exact path matched by exact route", "/health", true, "svc-health-exact"},
		// "/health/check" falls back to Prefix "/" because Exact "/health" only matches "/health" exactly.
		{"sub-path of exact route falls back to prefix", "/health/check", true, "svc-root"},
		{"root matched by prefix route", "/", true, "svc-root"},
		{"other path matched by prefix route", "/other", true, "svc-root"},
	}

	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/health", pathTypePtr(networkingv1.PathTypeExact), "svc-health-exact", 8081, "round_robin")
	mustAddRoute(t, r, "example.com", "/", pathTypePtr(networkingv1.PathTypePrefix), "svc-root", 80, "round_robin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match("example.com", tc.reqPath)
			if tc.wantMatch {
				if got == nil {
					t.Fatalf("Match(%q) = nil, want key=%q", tc.reqPath, tc.wantKey)
				}
				if got.Key != tc.wantKey {
					t.Errorf("Match(%q).Key = %q, want %q", tc.reqPath, got.Key, tc.wantKey)
				}
			} else {
				if got != nil {
					t.Errorf("Match(%q) = %+v, want nil", tc.reqPath, got)
				}
			}
		})
	}
}

func TestRouter_DefaultHostFallback(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		reqPath   string
		wantKey   string
		wantMatch bool
	}{
		{"known host matches its own route", "example.com", "/api", "svc-api", true},
		{"unknown host falls back to default", "unknown.io", "/api", "svc-default", true},
		{"empty host matches default explicitly", "", "/api", "svc-default", true},
	}

	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api", 8080, "round_robin")
	mustAddRoute(t, r, "", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-default", 9090, "round_robin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match(tc.host, tc.reqPath)
			if tc.wantMatch {
				if got == nil {
					t.Fatalf("Match(%q, %q) = nil, want key=%q", tc.host, tc.reqPath, tc.wantKey)
				}
				if got.Key != tc.wantKey {
					t.Errorf("Match(%q, %q).Key = %q, want %q", tc.host, tc.reqPath, got.Key, tc.wantKey)
				}
			} else {
				if got != nil {
					t.Errorf("Match(%q, %q) = %+v, want nil", tc.host, tc.reqPath, got)
				}
			}
		})
	}
}

func TestRouter_UnknownHostNoDefault(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		reqPath string
	}{
		{"completely unknown host", "unknown.io", "/api"},
		{"empty host with no default registered", "", "/api"},
	}

	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api", 8080, "round_robin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match(tc.host, tc.reqPath)
			if got != nil {
				t.Errorf("Match(%q, %q) = %+v, want nil", tc.host, tc.reqPath, got)
			}
		})
	}
}

func TestRouter_RouteRemoval(t *testing.T) {
	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api", 8080, "round_robin")
	mustAddRoute(t, r, "example.com", "/static", pathTypePtr(networkingv1.PathTypePrefix), "svc-static", 8081, "round_robin")

	// Confirm routes exist before removal.
	if got := r.Match("example.com", "/api/foo"); got == nil {
		t.Fatal("pre-condition: Match(/api/foo) = nil, want non-nil")
	}
	if got := r.Match("example.com", "/static/img"); got == nil {
		t.Fatal("pre-condition: Match(/static/img) = nil, want non-nil")
	}

	ingress := makeIngress("example.com", "/api")
	r.RemoveRoute(ingress)

	cases := []struct {
		name      string
		reqPath   string
		wantMatch bool
		wantKey   string
	}{
		{"removed route returns nil", "/api", false, ""},
		{"removed route sub-path returns nil", "/api/foo", false, ""},
		{"untouched route still matches", "/static/img", true, "svc-static"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match("example.com", tc.reqPath)
			if tc.wantMatch {
				if got == nil {
					t.Fatalf("Match(%q) = nil, want key=%q", tc.reqPath, tc.wantKey)
				}
				if got.Key != tc.wantKey {
					t.Errorf("Match(%q).Key = %q, want %q", tc.reqPath, got.Key, tc.wantKey)
				}
			} else {
				if got != nil {
					t.Errorf("Match(%q) = %+v, want nil", tc.reqPath, got)
				}
			}
		})
	}
}

func TestRouter_ReplaceIngressRoutes_SwapsRouteSet(t *testing.T) {
	r := NewRouter()

	r.ReplaceIngressRoutes("default/test-ingress", []RouteSpec{
		{
			Host:      "example.com",
			Path:      "/old",
			PathType:  string(networkingv1.PathTypePrefix),
			Key:       "svc-old",
			Port:      8080,
			Algorithm: "prequal",
		},
	})

	if got := r.Match("example.com", "/old/path"); got == nil || got.Key != "svc-old" {
		t.Fatalf("expected old route to exist before replacement, got %+v", got)
	}

	r.ReplaceIngressRoutes("default/test-ingress", []RouteSpec{
		{
			Host:      "example.com",
			Path:      "/new",
			PathType:  string(networkingv1.PathTypePrefix),
			Key:       "svc-new",
			Port:      9090,
			Algorithm: "prequal",
		},
	})

	if got := r.Match("example.com", "/old/path"); got != nil {
		t.Fatalf("expected old route to be removed after replacement, got %+v", got)
	}

	if got := r.Match("example.com", "/new/path"); got == nil || got.Key != "svc-new" {
		t.Fatalf("expected new route to exist after replacement, got %+v", got)
	}
}

func TestRouter_RemoveAllRoutesDeletesHost(t *testing.T) {
	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api", 8080, "round_robin")

	ingress := makeIngress("example.com", "/api")
	r.RemoveRoute(ingress)

	got := r.Match("example.com", "/api")
	if got != nil {
		t.Errorf("Match after removing all routes = %+v, want nil", got)
	}

	routes := r.GetAllRoutes()
	if _, exists := routes["example.com"]; exists {
		t.Error("host entry should be deleted after all paths removed, but it still exists")
	}
}

func TestRouter_RouteUpdate(t *testing.T) {
	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api-v1", 8080, "round_robin")

	// Overwrite with new key and port.
	mustAddRoute(t, r, "example.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-api-v2", 9090, "least_conn")

	got := r.Match("example.com", "/api/anything")
	if got == nil {
		t.Fatal("Match after update = nil, want non-nil")
	}

	cases := []struct {
		field string
		got   interface{}
		want  interface{}
	}{
		{"Key", got.Key, "svc-api-v2"},
		{"Port", got.Port, int32(9090)},
		{"Algorithm", got.Algorithm, "least_conn"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("after update: %s = %v, want %v", tc.field, tc.got, tc.want)
		}
	}

	// Ensure only one path entry exists (no duplicates).
	routes := r.GetAllRoutes()
	hc := routes["example.com"]
	if hc == nil {
		t.Fatal("host config missing after update")
	}
	if len(hc.Paths) != 1 {
		t.Errorf("expected 1 path entry after update, got %d", len(hc.Paths))
	}
}

func TestRouter_MultipleHosts(t *testing.T) {
	r := NewRouter()
	mustAddRoute(t, r, "alpha.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-alpha", 8080, "round_robin")
	mustAddRoute(t, r, "beta.com", "/api", pathTypePtr(networkingv1.PathTypePrefix), "svc-beta", 9090, "round_robin")
	mustAddRoute(t, r, "beta.com", "/admin", pathTypePtr(networkingv1.PathTypePrefix), "svc-beta-admin", 9091, "round_robin")

	cases := []struct {
		name      string
		host      string
		reqPath   string
		wantKey   string
		wantMatch bool
	}{
		{"alpha host matches alpha route", "alpha.com", "/api/data", "svc-alpha", true},
		{"beta host matches beta route", "beta.com", "/api/data", "svc-beta", true},
		{"beta admin route independent", "beta.com", "/admin/panel", "svc-beta-admin", true},
		{"alpha host does not match beta-only path", "alpha.com", "/admin/panel", "", false},
		{"gamma host is unknown", "gamma.com", "/api/data", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Match(tc.host, tc.reqPath)
			if tc.wantMatch {
				if got == nil {
					t.Fatalf("Match(%q, %q) = nil, want key=%q", tc.host, tc.reqPath, tc.wantKey)
				}
				if got.Key != tc.wantKey {
					t.Errorf("Match(%q, %q).Key = %q, want %q", tc.host, tc.reqPath, got.Key, tc.wantKey)
				}
			} else {
				if got != nil {
					t.Errorf("Match(%q, %q) = %+v, want nil", tc.host, tc.reqPath, got)
				}
			}
		})
	}
}

func TestRouter_NilPathTypeDefaultsToPrefix(t *testing.T) {
	r := NewRouter()
	mustAddRoute(t, r, "example.com", "/api", nil, "svc-api", 8080, "round_robin")

	got := r.Match("example.com", "/api/foo")
	if got == nil {
		t.Fatal("Match with nil PathType = nil, want non-nil")
	}
	if got.PathType != "Prefix" {
		t.Errorf("PathType = %q, want %q", got.PathType, "Prefix")
	}
}
