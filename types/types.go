package types

import (
	"fmt"
	"time"
)

// Route represents a resolved routing rule.
type Route struct {
	Host       string
	Path       string
	PathType   string // "Prefix" or "Exact"
	ServiceKey string // e.g., "default/api-service:80"
	Algorithm  string // e.g., "prequal", "round-robin", "least-connections"
}

// BackendRef identifies a specific backend endpoint.
type BackendRef struct {
	Address string
	Port    int32
}

func (b BackendRef) String() string {
	return fmt.Sprintf("%s:%d", b.Address, b.Port)
}

// BackendSet represents the set of available backends for a route.
type BackendSet struct {
	RouteKey  string
	Backends  []BackendRef
	UpdatedAt time.Time
}

// EndpointState represents the observed state of a backend.
type EndpointState struct {
	Backend       BackendRef
	RIF           int64
	MedianLatency time.Duration
	ObservedAt    time.Time
	Source        SignalSource
}

// SignalSource indicates where an observation came from.
type SignalSource int

const (
	SourceProxyLocal  SignalSource = iota // from proxy's own request tracking
	SourceBackendProbe                    // from backend's /prequal/probe endpoint
)

// SelectionPolicy describes how backend selection should work for a route.
type SelectionPolicy struct {
	Algorithm string
	QRIF      float64
	PoolSize  int
}

// ProbeObservation is a single probe response from a backend.
type ProbeObservation struct {
	Backend       BackendRef
	RIF           int64
	LatencyMedian time.Duration
	Timestamp     time.Time
	Source        SignalSource
}
