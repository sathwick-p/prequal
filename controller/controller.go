package controller

import (
	"fmt"
	"log"
	"sync"
	"time"

	discovery "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	discoveryLister "k8s.io/client-go/listers/discovery/v1"
	networkingLister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
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
	queue            workqueue.TypedRateLimitingInterface[string]
	syncMux          sync.RWMutex
	serviceToIngress map[string][]string
	router           *Router
}

func NewBackendIPStore() *BackendIPStore {
	return &BackendIPStore{
		ips: make(map[string][]string),
	}
}

func NewController(factory informers.SharedInformerFactory, store *BackendIPStore, queue workqueue.TypedRateLimitingInterface[string]) Controller {
	c := &Controller{
		informerFactory:  factory,
		store:            store,
		networkingLister: factory.Networking().V1().Ingresses().Lister(),
		discoveryLister:  factory.Discovery().V1().EndpointSlices().Lister(),
		queue:            queue,
		serviceToIngress: make(map[string][]string),
		router:           NewRouter(),
	}
	endpointSliceInformer := factory.Discovery().V1().EndpointSlices().Informer()
	endpointSliceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onEndpointSliceEvent,
		UpdateFunc: func(_, obj interface{}) { c.onEndpointSliceEvent(obj) },
		DeleteFunc: c.onEndpointSliceEvent,
	})
	ingressInformer := factory.Networking().V1().Ingresses().Informer()

	ingressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onIngressEvent,
		UpdateFunc: func(_, obj interface{}) { c.onIngressEvent(obj) },
		DeleteFunc: c.onIngressEvent,
	})
	return *c
}
func (c *Controller) onEndpointSliceEvent(obj interface{}) {
	eps, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		eps, ok = tombstone.Obj.(*discovery.EndpointSlice)
		if !ok {
			return
		}
	}

	svcName := eps.Labels["kubernetes.io/service-name"]
	if svcName == "" {
		return
	}
	svcKey := fmt.Sprintf("%s/%s", eps.Namespace, svcName)

	c.syncMux.RLock()
	ingressKeys := c.serviceToIngress[svcKey]
	c.syncMux.RUnlock()

	for _, key := range ingressKeys {
		c.queue.Add(key)
	}
}
func (c *Controller) onIngressEvent(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	c.queue.Add(key)
}

func (c *Controller) syncIngress(ingress *networkingv1.Ingress) {
	val, ok := ingress.Labels["ingress.class"]
	algo, ok := ingress.Annotations["lb/algo"]
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
		ruleHost := rule.Host
		for _, path := range rule.HTTP.Paths {
			svc := path.Backend.Service
			pathType := path.PathType
			path := path.Path
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
			serviceKey := fmt.Sprintf("%s/%s", namespace, serviceName)
			ingressKey := fmt.Sprintf("%s/%s", namespace, ingress.Name)
			c.syncMux.Lock()
			// Check if this ingress is already in the list
			found := false
			for _, k := range c.serviceToIngress[serviceKey] {
				if k == ingressKey {
					found = true
					break
				}
			}
			if !found {
				c.serviceToIngress[serviceKey] = append(c.serviceToIngress[serviceKey], ingressKey)
			}
			c.syncMux.Unlock()
			log.Printf("[INGRESS] %s/%s -> service: %s\n", ingress.Namespace, ingress.Name, serviceName)
			c.router.AddRoute(ruleHost, path, pathType, serviceKey, svc.Port.Number, algo)
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

	key := fmt.Sprintf("%s/%s", namespace, serviceName)
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
func (c *Controller) syncAllIngresses() {
	ingresses, _ := c.networkingLister.List(labels.Everything())
	for _, ing := range ingresses {
		key, err := cache.MetaNamespaceKeyFunc(ing)
		if err != nil {
			continue
		}
		c.queue.Add(key)
	}
}
func (c *Controller) Run(stop <-chan struct{}, workers int) error {
	log.Println("Starting controller...")

	// Wait for cache to sync before processing
	endpointsliceInfomer := c.informerFactory.Discovery().V1().EndpointSlices().Informer()
	ingressInformer := c.informerFactory.Networking().V1().Ingresses().Informer()
	if !cache.WaitForCacheSync(stop, endpointsliceInfomer.HasSynced, ingressInformer.HasSynced) {
		return fmt.Errorf("failed to sync cache")
	}
	log.Println("Cache synced, watching Ingresses...")
	c.syncAllIngresses()
	for i := 0; i < workers; i++ {

		go wait.Until(c.runWorker, time.Second, stop)
	}
	go func() {
		<-stop
		c.queue.ShutDown()
	}()

	<-stop
	return nil
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	if err := c.syncKey(key); err != nil {
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

func (c *Controller) syncKey(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	ingress, err := c.networkingLister.Ingresses(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.removeIngressFromMapping(key)
			c.router.RemoveRoute()
			return nil
		}
		return err
	}
	c.syncIngress(ingress)
	return nil
}

func (c *Controller) removeIngressFromMapping(ingressKey string) {
	c.syncMux.Lock()
	defer c.syncMux.Unlock()
	for svcKey, ingressKeys := range c.serviceToIngress {
		filtered := make([]string, 0, len(ingressKeys))
		for _, k := range ingressKeys {
			if k != ingressKey {
				filtered = append(filtered, k)
			}
		}
		if len(filtered) == 0 {
			delete(c.serviceToIngress, svcKey)
		} else {
			c.serviceToIngress[svcKey] = filtered
		}
	}
	log.Printf("[CLEANUP] Removed ingress %s from service mappings\n", ingressKey)
}
