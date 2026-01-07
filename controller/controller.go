package controller

import (
	"fmt"
	"log"
	"sync"

	discovery "k8s.io/api/discovery/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

type BackendIPStore struct {
	mu  sync.RWMutex
	ips map[string][]string //key : Namespace/Name value: []IPs
}

type Controller struct {
	informerFactory informers.SharedInformerFactory
	store           *BackendIPStore
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
	}

	endpointsInformer := factory.Discovery().V1().EndpointSlices().Informer()

	endpointsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
	})
	return *c
}

func (c *Controller) onAdd(obj interface{}) {
	endpoints, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		return
	}

	key := endpoints.Labels["kubernetes.io/service-name"]
	val := extractIps(endpoints)
	c.store.Update(key, val)
	log.Printf("[Event] ADD: %s\n", key)
}

func (c *Controller) onUpdate(oldObj, obj interface{}) {
	endpoints, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		return
	}

	key := endpoints.Labels["kubernetes.io/service-name"]
	val := extractIps(endpoints)
	c.store.Update(key, val)
	log.Printf("[Event] UPDATE: %s\n", key)

}
func extractIps(endpoints *discovery.EndpointSlice) []string {
	var ips []string
	for _, es := range endpoints.Endpoints {
		for _, ip := range es.Addresses {
			ips = append(ips, ip)
		}
	}
	return ips
}

func (c *BackendIPStore) Update(key string, val []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ips[key] = val
	log.Printf("Updated %s: %v\n", key, val)
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
