package controller

// NewEndpoint creates a new Endpoint with the given address and port.
func NewEndpoint(addr string, port int32) *Endpoint {
	return &Endpoint{addr: addr, port: port}
}
