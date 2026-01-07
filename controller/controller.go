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
	ips map[string][]string
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
		DeleteFunc: c.onDelete,
	})
	return *c
}
func (c *Controller) onDelete(obj interface{}) {
	endpoints, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		cache, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		endpoints, ok = cache.Obj.(*discovery.EndpointSlice)
		if !ok {
			return
		}
	}
	key := fmt.Sprintf("%s/%s", endpoints.Labels["kubernetes.io/service-name"], endpoints.Namespace)
	val := extractIps(endpoints)
	c.store.Delete(key, val)
	log.Printf("[Event] DELETE: %s removed IPs: %v\n", key, val)
}
func (c *BackendIPStore) Delete(key string, val []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ipList := c.ips[key]
	if len(ipList) == 0 {
		return
	}

	ipSet := make(map[string]struct{}, len(val))
	for _, v := range val {
		ipSet[v] = struct{}{}
	}
	n := 0
	for _, v := range ipList {
		if _, drop := ipSet[v]; !drop {
			ipList[n] = v
			n++
		}
	}
	ipList = ipList[:n]
	if len(ipList) == 0 {
		delete(c.ips, key)
		return
	}
	c.ips[key] = ipList

}
func (c *Controller) onAdd(obj interface{}) {
	endpoints, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		return
	}

	key := fmt.Sprintf("%s/%s", endpoints.Labels["kubernetes.io/service-name"], endpoints.Namespace)
	val := extractIps(endpoints)
	c.store.Update(key, val)
	log.Printf("[Event] ADD: %s\n", key)
}

func (c *Controller) onUpdate(oldObj, obj interface{}) {
	endpoints, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		return
	}

	key := fmt.Sprintf("%s/%s", endpoints.Labels["kubernetes.io/service-name"], endpoints.Namespace)
	val := extractIps(endpoints)
	c.store.Update(key, val)
	log.Printf("[Event] UPDATE: %s\n", key)

}
func extractIps(endpoints *discovery.EndpointSlice) []string {
	var ips []string
	for _, es := range endpoints.Endpoints {
		if es.Conditions.Ready != nil && *es.Conditions.Ready {
			for _, ip := range es.Addresses {
				ips = append(ips, ip)
			}
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
