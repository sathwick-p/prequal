package main

import (
	"encoding/json"
	"log"
	"prequal/controller"
	"net/http"
)

func StartDebugServer(c *controller.Controller) {
	http.HandleFunc("/routes", func(w http.ResponseWriter, r *http.Request) {
		routes := c.GetRouterSnapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(routes)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/routes", http.StatusFound)
	})

	log.Println("Debug server listening on :8081")
	go http.ListenAndServe(":8081", nil)
}
