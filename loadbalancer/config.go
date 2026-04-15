package loadbalancer

import "time"

// ProbeConfig holds all tunables for the probe pool and prober.
type ProbeConfig struct {
	PoolMaxSize        int           // default 16
	PoolMaxAge         time.Duration // default 1s
	PoolReuseLimit     int           // default 3
	QRIF               float64       // default 0.75
	ProbesPerQuery     float64       // default 1.0
	ProbeTimeout       time.Duration // default 100ms
	BackgroundInterval time.Duration // default 100ms
	MaxProbeAge        time.Duration // default 2s
	ProbePort          int           // default 8080
}

// DefaultProbeConfig returns a ProbeConfig populated with sensible defaults.
func DefaultProbeConfig() ProbeConfig {
	return ProbeConfig{
		PoolMaxSize:        16,
		PoolMaxAge:         1 * time.Second,
		PoolReuseLimit:     3,
		QRIF:               0.75,
		ProbesPerQuery:     1.0,
		ProbeTimeout:       100 * time.Millisecond,
		BackgroundInterval: 100 * time.Millisecond,
		MaxProbeAge:        2 * time.Second,
		ProbePort:          8080,
	}
}
