package pool

import (
	"prequal/controller"
	"time"
)


type ProbeEntry struct{
	Backend string
	Endpoint *controller.Endpoint
	RIF int64
	Latency time.Duration
	TimeStamp time.Time
	usesLeft int
}