# Prequal Observability Stack

Local Prometheus + Grafana stack for benchmarking the prequal ingress controller.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) 24+
- [Docker Compose](https://docs.docker.com/compose/install/) v2 (`docker compose` — note: no hyphen)

## Starting the stack

```bash
cd benchmark/observability
docker compose up -d
```

## URLs

| Service    | URL                        | Credentials   |
|------------|----------------------------|---------------|
| Prometheus | http://localhost:9090      | —             |
| Grafana    | http://localhost:3000      | admin / admin |

Grafana also allows anonymous read-only access with no login.

## How controller metrics are scraped

The controller exposes Prometheus metrics on its debug port **8081**, which is exposed as Kubernetes NodePort **30081**.

Prometheus is configured to scrape `host.docker.internal:30081/metrics`.

- **Docker Desktop (macOS / Windows):** `host.docker.internal` resolves automatically.
- **Linux (Docker Engine):** The `extra_hosts: ["host.docker.internal:host-gateway"]` in `docker-compose.yml` maps the name to your host IP automatically. No manual configuration needed.

## Dashboards

Six dashboards are provisioned automatically at startup:

| Dashboard | UID | Description |
|-----------|-----|-------------|
| Request Overview | `request-overview` | req/s, error rate, latency p50–p99.9, no-route/no-backend events |
| Algorithm Behavior | `algorithm-behavior` | selection counts by algorithm, backend selections, pool occupancy |
| Probe System | `probe-system` | probes sent/succeeded/failed/dropped, queue depth, success ratio |
| Control Plane | `control-plane` | reconciliation count + latency, active backends, no-backend events |
| Resource Usage | `resources` | controller process memory, CPU, goroutines, GC (node-exporter optional) |
| Long-Duration Stability | `long-duration` | latency, request rate, error rate, queue depth, dropped probes over 6–24h |

## Stopping and wiping

```bash
docker compose down -v
```

The `-v` flag removes named volumes (Prometheus TSDB and Grafana state). Omit it to keep data across restarts.

## Verification

Run these commands from the repository root to validate the stack configuration before starting:

```bash
# 1. Validate Docker Compose file syntax
docker compose -f benchmark/observability/docker-compose.yml config

# 2. Validate Prometheus config and rule files (requires promtool)
#    Install: go install github.com/prometheus/prometheus/cmd/promtool@latest
promtool check config benchmark/observability/prometheus/prometheus.yml
promtool check rules benchmark/observability/prometheus/recording-rules.yml benchmark/observability/prometheus/alert-rules.yml

# 3. Validate dashboard JSON (requires jq)
jq . benchmark/observability/grafana/dashboards/*.json >/dev/null && echo "All dashboard JSON valid"

# 4. Validate YAML files (requires yq or python3)
yq eval . benchmark/observability/prometheus/*.yml >/dev/null
# or: python3 -c "import yaml,sys; [yaml.safe_load(open(f)) for f in sys.argv[1:]]" benchmark/observability/prometheus/*.yml
```

If `promtool` is not installed, steps 2 can be skipped for local development but should be run in CI before publishing results.

## Adding node-exporter

For full CPU/memory/network metrics, add `node-exporter` to `docker-compose.yml`:

```yaml
  node-exporter:
    image: prom/node-exporter:v1.8.2
    network_mode: host
    pid: host
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/rootfs:ro
    command:
      - "--path.procfs=/host/proc"
      - "--path.sysfs=/host/sys"
      - "--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)($$|/)"
```

Then add a scrape config in `prometheus/prometheus.yml`:

```yaml
  - job_name: "node-exporter"
    static_configs:
      - targets: ["host.docker.internal:9100"]
```
