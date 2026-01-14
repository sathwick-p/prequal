package controller

import (
	"fmt"
	"log"
	"sync"

	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	lister "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/tools/cache"
)

type BackendIPStore struct {
	mu  sync.RWMutex
	ips map[string][]string
}

type Controller struct {
	informerFactory informers.SharedInformerFactory
	store           *BackendIPStore
	lister          lister.EndpointSliceLister
}

func NewBackendIPStore() *BackendIPStore {
	return &BackendIPStore{
		ips: make(map[string][]string),
	}
}

func NewController(factory informers.SharedInformerFactory, store *BackendIPStore) Controller {
	c := &Controller{
		informerFactory: factory,
		store:           store,
		lister:          factory.Discovery().V1().EndpointSlices().Lister(),
	}

	endpointsInformer := factory.Discovery().V1().EndpointSlices().Informer()

	endpointsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onEndpointSliceEvent,
		UpdateFunc: func(_, obj interface{}) { c.onEndpointSliceEvent(obj) },
		DeleteFunc: c.onEndpointSliceEvent,
	})
	return *c
}

func (c *Controller) onEndpointSliceEvent(obj interface{}) {
	endpoints, ok := obj.(*discovery.EndpointSlice)

	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		endpoints, ok = tombstone.Obj.(*discovery.EndpointSlice)
		if !ok {
			return
		}
	}

	serviceName := endpoints.Labels["kubernetes.io/service-name"]
	if serviceName == "" {
		return
	}

	namespace := endpoints.Namespace
	c.syncServiceEndpoints(namespace, serviceName)
}

func (c *Controller) syncServiceEndpoints(namespace, serviceName string) {
	selector := labels.SelectorFromSet(labels.Set{
		"kubernetes.io/service-name": serviceName,
	})

	// list all endpoint slices matching this service from cache

	slices, err := c.lister.EndpointSlices(namespace).List(selector)
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
	informer := c.informerFactory.Discovery().V1().EndpointSlices().Informer()
	if !cache.WaitForCacheSync(stop, informer.HasSynced) {
		return fmt.Errorf("failed to sync cache")
	}
	log.Println("Cache synced, watching EndpointSlices...")

	// Block until stop signal
	<-stop
	return nil
}
