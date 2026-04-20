package main

import (
	"encoding/json"
	"log"
	"net/http"
	_ "net/http/pprof"
	"prequal/controller"
	"runtime"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func StartDebugServer(c *controller.Controller) {
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	http.HandleFunc("/routes", func(w http.ResponseWriter, r *http.Request) {
		routes := c.GetRouterSnapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(routes)
	})
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/routes", http.StatusFound)
	})

	log.Println("Debug server listening on :8081 (pprof at /debug/pprof/)")
	go http.ListenAndServe(":8081", nil)
}
