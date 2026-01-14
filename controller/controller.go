package controller

import (
	"fmt"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	discoveryLister "k8s.io/client-go/listers/discovery/v1"
	networkingLister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"log"
	"sync"
)

type BackendIPStore struct {
	mu  sync.RWMutex
	ips map[string][]string
}

type Controller struct {
	informerFactory  informers.SharedInformerFactory
	store            *BackendIPStore
	networkingLister networkingLister.IngressLister
	discoveryLister  discoveryLister.EndpointSliceLister
}

func NewBackendIPStore() *BackendIPStore {
	return &BackendIPStore{
		ips: make(map[string][]string),
	}
}

func NewController(factory informers.SharedInformerFactory, store *BackendIPStore) Controller {
	c := &Controller{
		informerFactory:  factory,
		store:            store,
		networkingLister: factory.Networking().V1().Ingresses().Lister(),
		discoveryLister:  factory.Discovery().V1().EndpointSlices().Lister(),
	}

	ingressInformer := factory.Networking().V1().Ingresses().Informer()

	ingressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onIngressEvent,
		UpdateFunc: func(_, obj interface{}) { c.onIngressEvent(obj) },
		DeleteFunc: c.onIngressEvent,
	})
	return *c
}
func (c *Controller) onIngressEvent(obj interface{}) {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		ingress, ok = tombstone.Obj.(*networkingv1.Ingress)
		if !ok {
			return
		}
	}
	val, ok := ingress.Labels["ingress.class"]
	if !ok || val != "prequal" {
		log.Printf("[SKIP] Ingress %s/%s: not our class (got %q)\n", ingress.Namespace, ingress.Name, val)
		return
	}
	log.Printf("[EVENT] Ingress %s/%s triggered\n", ingress.Namespace, ingress.Name)
	namespace := ingress.Namespace
	syncedServices := make(map[string]bool)
	var serviceName string
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			svc := path.Backend.Service
			if svc != nil {
				serviceName = svc.Name
			} else if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
				serviceName = ingress.Spec.DefaultBackend.Service.Name
			}
			if serviceName == "" {
				continue
			}
			if syncedServices[serviceName] {
				continue
			}
			syncedServices[serviceName] = true
			log.Printf("[INGRESS] %s/%s -> service: %s\n", ingress.Namespace, ingress.Name, serviceName)
			c.syncServiceEndpoints(namespace, serviceName)
		}
	}
}

func (c *Controller) syncServiceEndpoints(namespace, serviceName string) {
	selector := labels.SelectorFromSet(labels.Set{
		"kubernetes.io/service-name": serviceName,
	})

	// list all endpoint slices matching this service from cache

	slices, err := c.discoveryLister.EndpointSlices(namespace).List(selector)
	if err != nil {
		log.Printf("[ERROR] Failed to list EndpointSlices for %s/%s: %v\n", namespace, serviceName, err)
		return
	}

	var allIPs []string
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				allIPs = append(allIPs, endpoint.Addresses...)
			}
		}
	}

	key := fmt.Sprintf("%s/%s", serviceName, namespace)
	if len(allIPs) == 0 {
		c.store.Delete(key)
		log.Printf("[SYNC] %s: no ready endpoints\n", key)
	} else {
		c.store.Set(key, allIPs)
		log.Printf("[SYNC] %s: %v\n", key, allIPs)
	}
}
func (c *BackendIPStore) Set(key string, ips []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ips[key] = ips
}
func (c *BackendIPStore) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.ips, key)
}
func (c *BackendIPStore) Get(key string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ips[key]
}

func (c *Controller) Run(stop <-chan struct{}) error {
	log.Println("Starting controller...")

	// Wait for cache to sync before processing
	endpointsliceInfomer := c.informerFactory.Discovery().V1().EndpointSlices().Informer()
	ingressInformer := c.informerFactory.Networking().V1().Ingresses().Informer()
	if !cache.WaitForCacheSync(stop, endpointsliceInfomer.HasSynced, ingressInformer.HasSynced) {
		return fmt.Errorf("failed to sync cache")
	}
	log.Println("Cache synced, watching Ingresses...")

	// Block until stop signal
	<-stop
	return nil
}
