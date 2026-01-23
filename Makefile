# Makefile for prequal ingress controller

# Configuration
IMAGE_NAME ?= prequal
IMAGE_TAG ?= latest
IMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: build run test clean docker-build docker-push deploy undeploy reload logs help

## Build locally
build:
	@echo "Building prequal..."
	go build -o bin/prequal .

## Run locally (for development)
run:
	@echo "Running locally..."
	go run .

## Run tests
test:
	go test ./...

## Clean build artifacts
clean:
	rm -rf bin/
	go clean

## Build Docker image
docker-build:
	@echo "Building Docker image: $(IMAGE)"
	docker build -t $(IMAGE) .

## Push to registry (set REGISTRY env var)
docker-push:
	@echo "Pushing $(IMAGE)..."
	docker push $(IMAGE)

## Load image into kind cluster (for local testing with kind)
## Set KIND_CLUSTER_NAME if your cluster isn't named "kind"
KIND_CLUSTER_NAME ?= clm

kind-load:
	@echo "Loading $(IMAGE) into kind cluster '$(KIND_CLUSTER_NAME)'..."
	kind load docker-image $(IMAGE) --name $(KIND_CLUSTER_NAME)

## Deploy test workloads (echo servers)
deploy-test:
	@echo "Deploying test workloads..."
	kubectl apply -f test.yaml

## Undeploy test workloads
undeploy-test:
	@echo "Removing test workloads..."
	kubectl delete -f test.yaml --ignore-not-found

## Deploy controller to cluster
deploy:
	@echo "Deploying prequal controller..."
	kubectl apply -f deploy/controller.yaml

## Undeploy controller
undeploy:
	@echo "Removing prequal controller..."
	kubectl delete -f deploy/controller.yaml --ignore-not-found

## Full reload: build, load to kind, restart deployment
reload: docker-build kind-load
	@echo "Restarting controller deployment..."
	kubectl rollout restart deployment/prequal-controller

## View controller logs
logs:
	kubectl logs -f deployment/prequal-controller

## Quick test - curl through the proxy
test-proxy:
	@echo "Testing proxy (via NodePort 30080)..."
	@NODE_IP=$$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'); \
	echo "Node IP: $$NODE_IP"; \
	curl -v -H "Host: echo.example.com" http://$$NODE_IP:30080/

## Check routes via debug endpoint
debug-routes:
	@NODE_IP=$$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'); \
	curl -s http://$$NODE_IP:30081/routes | jq .

## Port forward for local testing
port-forward:
	@echo "Port forwarding: localhost:8080 -> proxy, localhost:8081 -> debug"
	kubectl port-forward deployment/prequal-controller 8080:8080 8081:8081

## Full setup: deploy everything
setup: deploy-test deploy
	@echo "Setup complete! Use 'make test-proxy' to test."

## Full teardown
teardown: undeploy undeploy-test
	@echo "Teardown complete."

## Show status
status:
	@echo "=== Pods ==="
	@kubectl get pods -l app=prequal-controller
	@kubectl get pods -l app=echo
	@echo ""
	@echo "=== Services ==="
	@kubectl get svc prequal-proxy echo-service
	@echo ""
	@echo "=== Ingress ==="
	@kubectl get ingress test-ingress

## Help
help:
	@echo "Prequal Ingress Controller - Makefile Commands"
	@echo ""
	@echo "Development:"
	@echo "  make build        - Build binary locally"
	@echo "  make run          - Run locally (needs kubeconfig)"
	@echo "  make test         - Run tests"
	@echo "  make clean        - Clean build artifacts"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build - Build Docker image"
	@echo "  make docker-push  - Push to registry (set REGISTRY=your-registry)"
	@echo "  make kind-load    - Load image into kind cluster"
	@echo ""
	@echo "Kubernetes:"
	@echo "  make deploy       - Deploy controller to cluster"
	@echo "  make undeploy     - Remove controller from cluster"
	@echo "  make deploy-test  - Deploy test echo servers"
	@echo "  make undeploy-test- Remove test echo servers"
	@echo "  make setup        - Deploy everything"
	@echo "  make teardown     - Remove everything"
	@echo ""
	@echo "Operations:"
	@echo "  make reload       - Rebuild and redeploy (for development)"
	@echo "  make logs         - View controller logs"
	@echo "  make status       - Show cluster status"
	@echo "  make port-forward - Port forward for local testing"
	@echo ""
	@echo "Testing:"
	@echo "  make test-proxy   - Send test request through proxy"
	@echo "  make debug-routes - View routes from debug endpoint"
