// Package types defines the core domain models for the prequal ingress controller.
//
// These types represent the conceptual entities in the system:
//   - Route: a resolved routing rule from Kubernetes Ingress
//   - BackendRef: a specific backend endpoint (address + port)
//   - BackendSet: the available backends for a route
//   - EndpointState: observed load state of a backend
//   - SelectionPolicy: how backend selection should work
//   - ProbeObservation: a probe response from a backend
//
// Current code still uses package-local types in controller/, loadbalancer/, etc.
// These types define the migration target for clearer ownership boundaries.
package types
