package loadbalancer

import (
	"os"
	"strconv"
	"time"
)

// ProbeConfig holds all tunables for the probe pool and prober.
type ProbeConfig struct {
	PoolMaxSize             int           // default 16
	PoolMaxAge              time.Duration // default 1s
	PoolReuseLimit          int           // default 3
	PoolMaintenanceInterval time.Duration // default 100ms
	QRIF                    float64       // default 0.75
	ProbesPerQuery          float64       // default 1.0
	ProbeWorkers            int           // default 8
	TriggerQueueSize        int           // default 1024
	ProbeTimeout            time.Duration // default 100ms
	BackgroundInterval      time.Duration // default 100ms
	MaxProbeAge             time.Duration // default 2s
	ProbePort               int           // default 8080
}

// DefaultProbeConfig returns a ProbeConfig populated with sensible defaults.
func DefaultProbeConfig() ProbeConfig {
	return ProbeConfig{
		PoolMaxSize:             16,
		PoolMaxAge:              1 * time.Second,
		PoolReuseLimit:          3,
		PoolMaintenanceInterval: 100 * time.Millisecond,
		QRIF:                    0.75,
		ProbesPerQuery:          1.0,
		ProbeWorkers:            8,
		TriggerQueueSize:        1024,
		ProbeTimeout:            100 * time.Millisecond,
		BackgroundInterval:      100 * time.Millisecond,
		MaxProbeAge:             2 * time.Second,
		ProbePort:               8080,
	}
}

// ApplyEnv overrides probe settings from environment variables so scale tests can
// sweep parameters without rebuilding binaries.
func (c *ProbeConfig) ApplyEnv() {
	overrideIntEnv("PREQUAL_POOL_MAX_SIZE", &c.PoolMaxSize)
	overrideDurationMsEnv("PREQUAL_POOL_MAX_AGE_MS", &c.PoolMaxAge)
	overrideIntEnv("PREQUAL_POOL_REUSE_LIMIT", &c.PoolReuseLimit)
	overrideDurationMsEnv("PREQUAL_POOL_MAINTENANCE_INTERVAL_MS", &c.PoolMaintenanceInterval)
	overrideFloatEnv("PREQUAL_QRIF", &c.QRIF)
	overrideFloatEnv("PREQUAL_PROBES_PER_QUERY", &c.ProbesPerQuery)
	overrideIntEnv("PREQUAL_PROBE_WORKERS", &c.ProbeWorkers)
	overrideIntEnv("PREQUAL_PROBE_QUEUE_SIZE", &c.TriggerQueueSize)
	overrideDurationMsEnv("PREQUAL_PROBE_TIMEOUT_MS", &c.ProbeTimeout)
	overrideDurationMsEnv("PREQUAL_BACKGROUND_INTERVAL_MS", &c.BackgroundInterval)
	overrideDurationMsEnv("PREQUAL_MAX_PROBE_AGE_MS", &c.MaxProbeAge)
	overrideIntEnv("PREQUAL_PROBE_PORT", &c.ProbePort)
}

func overrideIntEnv(key string, target *int) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = parsed
	}
}

func overrideFloatEnv(key string, target *float64) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		*target = parsed
	}
}

func overrideDurationMsEnv(key string, target *time.Duration) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = time.Duration(parsed) * time.Millisecond
	}
}
