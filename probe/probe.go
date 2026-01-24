package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/procfs"
)

// ProbeResponse is returned when the ingress controller probes this sidecar
type ProbeResponse struct {
	RIF       int    `json:"rif"`
	BackendIP string `json:"backend_ip"`
	Timestamp int64  `json:"timestamp_ms"`
}

// Observer watches /proc/net/tcp and tracks RIF for the target port
type Observer struct {
	targetPort uint64
	procFS     procfs.FS
	mu         sync.RWMutex
	currentRIF int
	localIP    string
}

const (
	tcpEstablished = 1 // TCP state for ESTABLISHED connections
)

// NewObserver creates a new observer for the given target port
func NewObserver(targetPort int) (*Observer, error) {
	fs, err := procfs.NewFS("/proc")
	if err != nil {
		return nil, err
	}

	return &Observer{
		targetPort: uint64(targetPort),
		procFS:     fs,
		localIP:    "unknown",
	}, nil
}

// Start begins background polling of connection stats
func (o *Observer) Start(pollInterval time.Duration) {
	// Do an initial update
	o.updateStats()

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for range ticker.C {
			o.updateStats()
		}
	}()
}

// updateStats reads /proc/net/tcp and counts ESTABLISHED connections
func (o *Observer) updateStats() {
	rif := 0
	localIP := "unknown"

	// Read IPv4 TCP connections
	tcp4, err := o.procFS.NetTCP()
	if err != nil {
		log.Printf("[PROBE] Error reading /proc/net/tcp: %v", err)
	} else {
		for _, conn := range tcp4 {
			if conn.LocalPort == o.targetPort && conn.St == tcpEstablished {
				rif++
				if localIP == "unknown" && conn.LocalAddr != nil {
					localIP = conn.LocalAddr.String()
				}
			}
		}
	}

	// Read IPv6 TCP connections
	tcp6, err := o.procFS.NetTCP6()
	if err != nil {
		// IPv6 might not be available, that's okay
		log.Printf("[PROBE] Note: /proc/net/tcp6 not available: %v", err)
	} else {
		for _, conn := range tcp6 {
			if conn.LocalPort == o.targetPort && conn.St == tcpEstablished {
				rif++
				if localIP == "unknown" && conn.LocalAddr != nil {
					localIP = conn.LocalAddr.String()
				}
			}
		}
	}

	o.mu.Lock()
	o.currentRIF = rif
	if localIP != "unknown" {
		o.localIP = localIP
	}
	o.mu.Unlock()
}

// GetProbeData returns the current probe data
func (o *Observer) GetProbeData() ProbeResponse {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return ProbeResponse{
		RIF:       o.currentRIF,
		BackendIP: o.localIP,
		Timestamp: time.Now().UnixMilli(),
	}
}

func main() {
	log.Println("[PROBE] Prequal sidecar starting...")

	// Get target port from environment
	portStr := os.Getenv("TARGET_PORT")
	targetPort, err := strconv.Atoi(portStr)
	if err != nil || targetPort == 0 {
		targetPort = 80 // Default to port 80 (common for containers)
		log.Printf("[PROBE] TARGET_PORT not set, defaulting to %d", targetPort)
	}

	// Get probe server port from environment
	probePortStr := os.Getenv("PROBE_PORT")
	probePort, err := strconv.Atoi(probePortStr)
	if err != nil || probePort == 0 {
		probePort = 9999 // Default probe port
	}

	log.Printf("[PROBE] Observing connections on port %d", targetPort)
	log.Printf("[PROBE] Serving probe endpoint on port %d", probePort)

	// Create observer
	observer, err := NewObserver(targetPort)
	if err != nil {
		log.Fatalf("[PROBE] Failed to create observer: %v", err)
	}

	// Start background polling (every 100ms)
	observer.Start(100 * time.Millisecond)

	// HTTP handlers
	http.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
		data := observer.GetProbeData()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("[PROBE] Error encoding response: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Start server
	addr := ":" + strconv.Itoa(probePort)
	log.Printf("[PROBE] Listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("[PROBE] Server failed: %v", err)
	}
}
