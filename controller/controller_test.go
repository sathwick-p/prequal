package controller

import (
	"testing"

	discovery "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	fakek8s "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
)

// newPrequalIngress creates an Ingress with the prequal label and the given rules.
func newPrequalIngress(namespace, name string, rules []networkingv1.IngressRule) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"ingress.class": "prequal",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: rules,
		},
	}
}

// newController creates a Controller backed by a fake clientset seeded with
// the provided runtime.Object instances, starts the informer factory, and
// waits for the caches to sync before returning.
func newController(t *testing.T, objects ...runtime.Object) *Controller {
	t.Helper()

	client := fakek8s.NewSimpleClientset(objects...)
	factory := informers.NewSharedInformerFactory(client, 0)
	store := NewBackendIPStore()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	c := NewController(factory, store, queue)

	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		queue.ShutDown()
	})

	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	return c
}

// ---------------------------------------------------------------------------
// syncIngress tests
// ---------------------------------------------------------------------------

func TestSyncIngress_PopulatesRoutes(t *testing.T) {
	ingress := newPrequalIngress("default", "my-ingress", []networkingv1.IngressRule{
		{
			Host: "example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/api",
							PathType: pathTypePtr(networkingv1.PathTypePrefix),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "my-service",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						},
					},
				},
			},
		},
	})

	c := newController(t, ingress)
	c.syncIngress(ingress)

	match := c.router.Match("example.com", "/api")
	if match == nil {
		t.Fatal("expected route to be present after syncIngress, got nil")
	}
	if match.Key != "default/my-service:8080" {
		t.Errorf("unexpected route key: got %q, want %q", match.Key, "default/my-service:8080")
	}
}

func TestSyncIngress_SkipsNonPrequalIngress(t *testing.T) {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "other-ingress",
			Labels:    map[string]string{"ingress.class": "nginx"},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: pathTypePtr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "my-service",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := newController(t, ingress)
	c.syncIngress(ingress)

	match := c.router.Match("example.com", "/")
	if match != nil {
		t.Errorf("expected no route for non-prequal ingress, got %+v", match)
	}
}

func TestSyncIngress_MultipleHostsAndPaths(t *testing.T) {
	ingress := newPrequalIngress("default", "multi-ingress", []networkingv1.IngressRule{
		{
			Host: "foo.example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/a",
							PathType: pathTypePtr(networkingv1.PathTypePrefix),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-a",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						},
						{
							Path:     "/b",
							PathType: pathTypePtr(networkingv1.PathTypeExact),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-b",
									Port: networkingv1.ServiceBackendPort{Number: 9090},
								},
							},
						},
					},
				},
			},
		},
		{
			Host: "bar.example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/c",
							PathType: pathTypePtr(networkingv1.PathTypePrefix),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-c",
									Port: networkingv1.ServiceBackendPort{Number: 7070},
								},
							},
						},
					},
				},
			},
		},
	})

	c := newController(t, ingress)
	c.syncIngress(ingress)

	cases := []struct {
		host, path string
		wantKey    string
	}{
		{"foo.example.com", "/a", "default/svc-a:8080"},
		{"foo.example.com", "/b", "default/svc-b:9090"},
		{"bar.example.com", "/c", "default/svc-c:7070"},
	}

	for _, tc := range cases {
		m := c.router.Match(tc.host, tc.path)
		if m == nil {
			t.Errorf("Match(%q, %q): got nil, want key %q", tc.host, tc.path, tc.wantKey)
			continue
		}
		if m.Key != tc.wantKey {
			t.Errorf("Match(%q, %q): got key %q, want %q", tc.host, tc.path, m.Key, tc.wantKey)
		}
	}
}

// ---------------------------------------------------------------------------
// handleIngressDeletion tests
// ---------------------------------------------------------------------------

func TestHandleIngressDeletion_RemovesRoutes(t *testing.T) {
	ingress := newPrequalIngress("default", "del-ingress", []networkingv1.IngressRule{
		{
			Host: "delete.example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/del",
							PathType: pathTypePtr(networkingv1.PathTypePrefix),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "del-svc",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						},
					},
				},
			},
		},
	})

	c := newController(t, ingress)
	c.syncIngress(ingress)

	if m := c.router.Match("delete.example.com", "/del"); m == nil {
		t.Fatal("precondition: route should exist before deletion")
	}

	c.handleIngressDeletion(ingress)

	if m := c.router.Match("delete.example.com", "/del"); m != nil {
		t.Errorf("expected route to be removed after deletion, got %+v", m)
	}
}

func TestHandleIngressDeletion_CleansServiceToIngressMapping(t *testing.T) {
	ingress := newPrequalIngress("default", "mapped-ingress", []networkingv1.IngressRule{
		{
			Host: "mapped.example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: pathTypePtr(networkingv1.PathTypePrefix),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "mapped-svc",
									Port: networkingv1.ServiceBackendPort{Number: 80},
								},
							},
						},
					},
				},
			},
		},
	})

	c := newController(t, ingress)
	c.syncIngress(ingress)

	svcKey := "default/mapped-svc"
	c.syncMux.RLock()
	before := len(c.serviceToIngress[svcKey])
	c.syncMux.RUnlock()
	if before == 0 {
		t.Fatal("precondition: serviceToIngress should have an entry before deletion")
	}

	c.handleIngressDeletion(ingress)

	c.syncMux.RLock()
	after, exists := c.serviceToIngress[svcKey]
	c.syncMux.RUnlock()
	if exists && len(after) > 0 {
		t.Errorf("expected serviceToIngress[%q] to be empty after deletion, got %v", svcKey, after)
	}
}

func TestHandleIngressDeletion_SkipsNonPrequalIngress(t *testing.T) {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "ignore-me",
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "ignore.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: pathTypePtr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "ignore-svc",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := newController(t)
	// Manually add an unrelated route to verify it is not touched.
	c.router.AddRoute("other.example.com", "/", pathTypePtr(networkingv1.PathTypePrefix), "default/other-svc:80", 80, "")

	c.handleIngressDeletion(ingress)

	if m := c.router.Match("other.example.com", "/"); m == nil {
		t.Error("unrelated route was unexpectedly removed")
	}
}

// ---------------------------------------------------------------------------
// removeIngressFromMapping tests
// ---------------------------------------------------------------------------

func TestRemoveIngressFromMapping_RemovesSingleEntry(t *testing.T) {
	c := newController(t)
	c.serviceToIngress["default/svc"] = []string{"default/ingress-a"}

	c.removeIngressFromMapping("default/ingress-a")

	c.syncMux.RLock()
	_, exists := c.serviceToIngress["default/svc"]
	c.syncMux.RUnlock()
	if exists {
		t.Error("expected serviceToIngress key to be deleted when last ingress is removed")
	}
}

func TestRemoveIngressFromMapping_LeavesOtherIngresses(t *testing.T) {
	c := newController(t)
	c.serviceToIngress["default/svc"] = []string{"default/ingress-a", "default/ingress-b"}

	c.removeIngressFromMapping("default/ingress-a")

	c.syncMux.RLock()
	remaining := c.serviceToIngress["default/svc"]
	c.syncMux.RUnlock()

	if len(remaining) != 1 || remaining[0] != "default/ingress-b" {
		t.Errorf("expected [default/ingress-b] to remain, got %v", remaining)
	}
}

func TestRemoveIngressFromMapping_NoopOnMissingKey(t *testing.T) {
	c := newController(t)
	// Should not panic when the ingress does not appear in any mapping.
	c.removeIngressFromMapping("default/nonexistent")
}

// ---------------------------------------------------------------------------
// syncServiceEndpoints tests
// ---------------------------------------------------------------------------

func TestSyncServiceEndpoints_PopulatesStore(t *testing.T) {
	port := int32(8080)
	ready := true

	eps := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-svc-abc",
			Labels: map[string]string{
				"kubernetes.io/service-name": "my-svc",
			},
		},
		AddressType: discovery.AddressTypeIPv4,
		Ports: []discovery.EndpointPort{
			{Port: &port},
		},
		Endpoints: []discovery.Endpoint{
			{
				Addresses:  []string{"10.0.0.1"},
				Conditions: discovery.EndpointConditions{Ready: &ready},
			},
		},
	}

	c := newController(t, eps)

	storeKey := "default/my-svc:8080"
	c.syncServiceEndpoints("default", "my-svc", storeKey, 8080, "")

	endpoints := c.store.Get(storeKey)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint in store, got %d", len(endpoints))
	}
	if endpoints[0].Addr() != "10.0.0.1" {
		t.Errorf("unexpected endpoint address: got %q, want %q", endpoints[0].Addr(), "10.0.0.1")
	}
	if endpoints[0].Port() != 8080 {
		t.Errorf("unexpected endpoint port: got %d, want %d", endpoints[0].Port(), 8080)
	}
}

func TestSyncServiceEndpoints_DeletesStoreEntryWhenNoReadyEndpoints(t *testing.T) {
	port := int32(8080)
	notReady := false

	eps := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-svc-xyz",
			Labels: map[string]string{
				"kubernetes.io/service-name": "my-svc",
			},
		},
		AddressType: discovery.AddressTypeIPv4,
		Ports: []discovery.EndpointPort{
			{Port: &port},
		},
		Endpoints: []discovery.Endpoint{
			{
				Addresses:  []string{"10.0.0.2"},
				Conditions: discovery.EndpointConditions{Ready: &notReady},
			},
		},
	}

	c := newController(t, eps)

	storeKey := "default/my-svc:8080"
	// Pre-populate the store so we can verify it gets deleted.
	c.store.Set(storeKey, []*Endpoint{{addr: "10.0.0.2", port: 8080}})

	c.syncServiceEndpoints("default", "my-svc", storeKey, 8080, "")

	endpoints := c.store.Get(storeKey)
	if len(endpoints) != 0 {
		t.Errorf("expected store entry to be deleted, got %v", endpoints)
	}
}

func TestSyncServiceEndpoints_TreatsNilReadyAsReady(t *testing.T) {
	port := int32(8080)

	eps := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-svc-nil-ready",
			Labels: map[string]string{
				"kubernetes.io/service-name": "my-svc",
			},
		},
		AddressType: discovery.AddressTypeIPv4,
		Ports: []discovery.EndpointPort{
			{Port: &port},
		},
		Endpoints: []discovery.Endpoint{
			{
				Addresses: []string{"10.0.0.3"},
			},
		},
	}

	c := newController(t, eps)

	storeKey := "default/my-svc:8080"
	c.syncServiceEndpoints("default", "my-svc", storeKey, 8080, "")

	endpoints := c.store.Get(storeKey)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint in store, got %d", len(endpoints))
	}
	if endpoints[0].Addr() != "10.0.0.3" {
		t.Errorf("unexpected endpoint address: got %q, want %q", endpoints[0].Addr(), "10.0.0.3")
	}
}

func TestSyncServiceEndpoints_MatchesByPortName(t *testing.T) {
	port := int32(9090)
	portName := "http"
	ready := true

	eps := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "named-svc-abc",
			Labels: map[string]string{
				"kubernetes.io/service-name": "named-svc",
			},
		},
		AddressType: discovery.AddressTypeIPv4,
		Ports: []discovery.EndpointPort{
			{Name: &portName, Port: &port},
		},
		Endpoints: []discovery.Endpoint{
			{
				Addresses:  []string{"192.168.1.1"},
				Conditions: discovery.EndpointConditions{Ready: &ready},
			},
		},
	}

	c := newController(t, eps)

	storeKey := "default/named-svc:http"
	c.syncServiceEndpoints("default", "named-svc", storeKey, 0, "http")

	endpoints := c.store.Get(storeKey)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0].Port() != 9090 {
		t.Errorf("expected port 9090, got %d", endpoints[0].Port())
	}
}

// ---------------------------------------------------------------------------
// isIngressPrequal tests
// ---------------------------------------------------------------------------

func TestIsIngressPrequal_ViaLabel(t *testing.T) {
	c := newController(t)
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"ingress.class": "prequal"},
		},
	}
	if !c.isIngressPrequal(ing) {
		t.Error("expected true for ingress with ingress.class=prequal label")
	}
}

func TestIsIngressPrequal_ViaIngressClassName(t *testing.T) {
	c := newController(t)
	className := "prequal"
	ing := &networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
		},
	}
	if !c.isIngressPrequal(ing) {
		t.Error("expected true for ingress with IngressClassName=prequal")
	}
}

func TestIsIngressPrequal_ReturnsFalseForOtherClass(t *testing.T) {
	c := newController(t)
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"ingress.class": "nginx"},
		},
	}
	if c.isIngressPrequal(ing) {
		t.Error("expected false for ingress with ingress.class=nginx")
	}
}
