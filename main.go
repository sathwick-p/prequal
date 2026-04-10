package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	signal "os/signal"
	"prequal/controller"
	"prequal/loadbalancer/roundrobin"
	"prequal/server"
	"syscall"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig file (optional, uses in-cluster config if not set)")
	namespace := flag.String("namespace", "", "namespaces to be tracked")
	flag.Parse()

	var config *rest.Config
	var err error
	if *kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)

	} else if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigEnv)
	} else if home := os.Getenv("HOME"); home != "" {
		defaultPath := home + "/.kube/config"
		if _, statErr := os.Stat(defaultPath); statErr == nil {
			config, err = clientcmd.BuildConfigFromFlags("", defaultPath)
		} else {
			config, err = rest.InClusterConfig()
		}
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Printf("Error building kubeconfig: %v\n", err)
		os.Exit(1)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Error creating kuberenetes client: %v\n", err)
		os.Exit(1)
	}

	var factory informers.SharedInformerFactory
	if *namespace != "" {
		factory = informers.NewSharedInformerFactoryWithOptions(clientset, 60*time.Second, informers.WithNamespace(*namespace))
		log.Printf("Watching namespace: %s\n", *namespace)
	} else {
		factory = informers.NewSharedInformerFactory(clientset, 60*time.Second)
		log.Printf("Watching all namespaces")
	}

	store := controller.NewBackendIPStore()
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{
			Name: "prequal",
		},
	)
	ctrl := controller.NewController(factory, store, queue)
	selector := &roundrobin.RoundRobin{}
	proxyServer := server.NewProxyServer(ctrl.GetRouter(), store, selector)
	StartDebugServer(ctrl)

	// Start proxy server in background
	go func() {
		log.Println("Proxy server listening on :8080")
		if err := http.ListenAndServe(":8080", proxyServer); err != nil {
			log.Fatalf("Proxy server error: %v", err)
		}
	}()
	stop := make(chan struct{})
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		log.Printf("Shutting Down")
		close(stop)
	}()

	factory.Start(stop)

	if err := ctrl.Run(stop, 1); err != nil {
		log.Fatalf("Error running controller: %v", err)
	}

}
