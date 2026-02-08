package prequal

import (
	"fmt"
	"time"
)


type Stats struct {
	inflight int64
	ewmaLatency time.Duration
	lastUpdated time.Time
} 
func GetEndpointKey(routeKey, addr string, port int32) string {
	return fmt.Sprintf("%s|%s:%d", routeKey, addr, port)
}
