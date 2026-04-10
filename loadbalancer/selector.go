package loadbalancer

import "prequal/controller"

type Selector interface{
	Select(endpoints []*controller.Endpoint)(*controller.Endpoint, error)
}