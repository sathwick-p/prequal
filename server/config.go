package server

import (
	"net"
	"os"
	"strconv"
	"time"
)

type Config struct {
	LogRequests           bool
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	IdleConnTimeout       time.Duration
	BackendDialTimeout    time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		LogRequests:           true,
		MaxIdleConns:          1024,
		MaxIdleConnsPerHost:   256,
		IdleConnTimeout:       90 * time.Second,
		BackendDialTimeout:    2 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (c *Config) ApplyEnv() {
	if benchmarkModeEnabled() {
		c.LogRequests = false
	}
	overrideBoolEnv("PREQUAL_LOG_REQUESTS", &c.LogRequests)
	overrideIntEnv("PREQUAL_BACKEND_MAX_IDLE_CONNS", &c.MaxIdleConns)
	overrideIntEnv("PREQUAL_BACKEND_MAX_IDLE_CONNS_PER_HOST", &c.MaxIdleConnsPerHost)
	overrideDurationMsEnv("PREQUAL_BACKEND_IDLE_CONN_TIMEOUT_MS", &c.IdleConnTimeout)
	overrideDurationMsEnv("PREQUAL_BACKEND_DIAL_TIMEOUT_MS", &c.BackendDialTimeout)
	overrideDurationMsEnv("PREQUAL_BACKEND_RESPONSE_HEADER_TIMEOUT_MS", &c.ResponseHeaderTimeout)
	overrideDurationMsEnv("PREQUAL_BACKEND_EXPECT_CONTINUE_TIMEOUT_MS", &c.ExpectContinueTimeout)
}

func (c Config) transport() *net.Dialer {
	return &net.Dialer{
		Timeout:   c.BackendDialTimeout,
		KeepAlive: 30 * time.Second,
	}
}

func benchmarkModeEnabled() bool {
	return stringsEqualFold(os.Getenv("PREQUAL_BENCHMARK_MODE"), "true")
}

func stringsEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		aa := a[i]
		bb := b[i]
		if aa >= 'A' && aa <= 'Z' {
			aa += 'a' - 'A'
		}
		if bb >= 'A' && bb <= 'Z' {
			bb += 'a' - 'A'
		}
		if aa != bb {
			return false
		}
	}
	return true
}

func overrideBoolEnv(key string, target *bool) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.ParseBool(value); err == nil {
		*target = parsed
	}
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

func overrideDurationMsEnv(key string, target *time.Duration) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = time.Duration(parsed) * time.Millisecond
	}
}
