# Prequal: Evidence Asset Specification

## 1. Purpose

This document is the concrete asset checklist for a publication-grade benchmark campaign.

Use this document when assigning work to other AI agents.

It answers:

- what files and folders must exist
- what dashboards must exist
- what scripts must exist
- what outputs must be collected
- what is still missing before evidence collection can begin

This document is intentionally concrete.

---

## 2. Deliverable Tree

The repository should eventually contain at least this structure:

```text
benchmark/
  README.md
  benchmarking-matrix.md
  public-claim-playbook.md
  evidence-asset-spec.md
  manifests/
    controller-benchmark.yaml
    workload-uniform.yaml
    workload-heterogeneous.yaml
    workload-multiroute.yaml
    workload-route-scale.yaml
    workload-long-duration.yaml
    fault-probe-timeout.yaml
    fault-probe-500.yaml
    fault-probe-malformed.yaml
    fault-probe-stale-timestamp.yaml
  k6/
    steady_state.js
    open_loop.js
    multi_route.js
    rate_ramp.js
    burst.js
    long_duration.js
    overload.js
  scripts/
    churn.sh
    deploy_benchmark_stack.sh
    run_campaign.sh
    collect_results.sh
    archive_results.sh
    render_report.sh
  results/
    .gitkeep
  report/
    templates/
      run-report.md
      campaign-report.md
    charts/
      latency.py
      throughput.py
      resources.py
      probes.py
      control_plane.py
  observability/
    docker-compose.yml
    prometheus/
      prometheus.yml
      recording-rules.yml
      alert-rules.yml
    grafana/
      provisioning/
        datasources/
          prometheus.yml
        dashboards/
          dashboards.yml
      dashboards/
        request-overview.json
        algorithm-behavior.json
        probe-system.json
        control-plane.json
        resources.json
        long-duration.json
```

Some of these do not exist yet. They should.

---

## 3. Asset Status

### Already exists

- `benchmark/README.md`
- `benchmark/public-claim-playbook.md`
- `benchmark/manifests/controller-benchmark.yaml`
- `benchmark/manifests/workload-uniform.yaml`
- `benchmark/manifests/workload-heterogeneous.yaml`
- `benchmark/manifests/workload-multiroute.yaml`
- `benchmark/k6/steady_state.js`
- `benchmark/k6/open_loop.js`
- `benchmark/k6/multi_route.js`
- `benchmark/scripts/churn.sh`

### Missing and should be created next

- benchmark matrix doc
- rate-ramp script
- burst script
- long-duration script
- overload script
- result collector script
- artifact archiver script
- report generator script
- Prometheus stack assets
- Grafana provisioning assets
- Grafana dashboards
- fault-injection manifests
- route-scale workload manifest
- long-duration workload manifest

---

## 4. Exact Assets To Create

### 4.1 Benchmark Matrix Doc

Create:

- `benchmark/benchmarking-matrix.md`

Contents:

- every planned scenario
- every algorithm
- every environment
- every repetition count
- owner agent
- status
- output directory

Columns:

- campaign id
- scenario
- environment
- algorithm
- script
- manifest
- rate model
- duration
- repetitions
- owner
- status

### 4.2 Additional k6 Scripts

Create:

- `benchmark/k6/rate_ramp.js`
- `benchmark/k6/burst.js`
- `benchmark/k6/long_duration.js`
- `benchmark/k6/overload.js`

Required behavior:

- `rate_ramp.js`
  run stepwise constant-arrival-rate phases with increasing `RATE`

- `burst.js`
  alternate idle windows and burst windows

- `long_duration.js`
  stable open-loop run for 1h+ with optional periodic markers

- `overload.js`
  push well beyond expected saturation and keep the system there

All scripts should:

- accept env vars
- emit JSON summaries
- use host headers correctly
- keep request body constant unless explicitly varied

### 4.3 Additional Manifests

Create:

- `benchmark/manifests/workload-route-scale.yaml`
- `benchmark/manifests/workload-long-duration.yaml`
- `benchmark/manifests/fault-probe-timeout.yaml`
- `benchmark/manifests/fault-probe-500.yaml`
- `benchmark/manifests/fault-probe-malformed.yaml`
- `benchmark/manifests/fault-probe-stale-timestamp.yaml`

Purpose:

- route scale
  many hosts/routes/services

- long duration
  stable production-like benchmark topology

- timeout fault
  probe handler sleeps beyond timeout

- 500 fault
  probe handler returns 500

- malformed fault
  probe handler returns invalid JSON

- stale timestamp fault
  probe handler returns old timestamps

### 4.4 Result Collector and Archiver

Create:

- `benchmark/scripts/run_campaign.sh`
- `benchmark/scripts/collect_results.sh`
- `benchmark/scripts/archive_results.sh`

Responsibilities:

- `run_campaign.sh`
  orchestrates one full scenario run

- `collect_results.sh`
  pulls:
  - k6 JSON output
  - `kubectl top`
  - relevant logs
  - route snapshot
  - Prometheus snapshots/export

- `archive_results.sh`
  packages one run into a timestamped artifact directory or tarball

### 4.5 Report Generation

Create:

- `benchmark/report/templates/run-report.md`
- `benchmark/report/templates/campaign-report.md`
- `benchmark/scripts/render_report.sh`

Optionally create chart scripts in Python, Go, or JS.

Every run report should include:

- metadata
- environment
- algorithm/config
- load profile
- headline metrics
- anomalies
- links to raw artifacts

Every campaign report should include:

- comparison table
- per-scenario conclusions
- limitations
- recommendation

---

## 5. Observability Stack Assets

The repo does **not** yet include a local Prometheus/Grafana stack.

Create these exactly:

### 5.1 Docker Compose Stack

Create:

- `benchmark/observability/docker-compose.yml`

Services:

- `prometheus`
- `grafana`
- optionally `node-exporter`

If the benchmark target is local Kubernetes, Prometheus can still run outside the cluster and scrape NodePorts.

### 5.2 Prometheus Config

Create:

- `benchmark/observability/prometheus/prometheus.yml`
- `benchmark/observability/prometheus/recording-rules.yml`
- `benchmark/observability/prometheus/alert-rules.yml`

Prometheus scrape targets should include:

- controller debug endpoint `/metrics`
- optional node-exporter
- optional backend metrics endpoint if you add one later

Recording rules should precompute:

- request rate by route
- error rate by route
- probe success rate
- probe failure rate
- p95/p99/p99.9 approximations from histogram buckets
- backend selection rate by route/backend

Alert rules are optional for local work, but useful for:

- probe queue depth high
- probe drops non-zero
- controller reconciliation latency high
- no-backend spikes

### 5.3 Grafana Provisioning

Create:

- `benchmark/observability/grafana/provisioning/datasources/prometheus.yml`
- `benchmark/observability/grafana/provisioning/dashboards/dashboards.yml`

This should auto-load dashboards at startup.

---

## 6. Dashboard Set

Create these dashboard JSON files:

- `request-overview.json`
- `algorithm-behavior.json`
- `probe-system.json`
- `control-plane.json`
- `resources.json`
- `long-duration.json`

These should not be vague. They need defined panels.

### 6.1 Request Overview Dashboard

Panels:

- Requests/sec total
- Requests/sec by route
- Error rate total
- Error rate by route
- p50 latency by route
- p95 latency by route
- p99 latency by route
- p99.9 latency by route
- Status-code breakdown
- No-route events
- No-backend events

Preferred layout:

- top row: request rate, error rate, status breakdown
- middle rows: p50/p95/p99/p99.9
- bottom row: route-specific breakdowns

### 6.2 Algorithm Behavior Dashboard

Panels:

- selection counts by algorithm
- backend selections by route
- backend selections by backend
- active backends by route
- pool occupancy by route
- route comparison table

Preferred layout:

- top row: algorithm and route summary
- middle: per-route backend distribution
- bottom: pool occupancy and active backends

### 6.3 Probe System Dashboard

Panels:

- probes sent/sec
- probes succeeded/sec
- probes failed/sec by reason
- dropped probes/sec by reason
- probe queue depth
- pool occupancy by route
- probe success/failure ratio

Preferred layout:

- top row: sent/succeeded/failed/dropped
- middle row: queue depth and occupancy
- bottom row: reason breakdowns

### 6.4 Control-Plane Dashboard

Panels:

- reconciliation count
- reconciliation latency
- active backends by route
- no-backend events during churn
- request error spikes during churn

Preferred layout:

- top row: reconcile throughput/latency
- middle: active backends
- bottom: correlation with request errors

### 6.5 Resources Dashboard

Panels:

- controller CPU
- controller memory
- backend CPU
- backend memory
- node CPU
- node memory
- network throughput if available

Preferred layout:

- top row: controller resource usage
- middle row: backend resource usage
- bottom row: node/system metrics

### 6.6 Long-Duration Dashboard

Panels:

- latency over hours
- request rate over hours
- error rate over hours
- controller memory over hours
- backend memory over hours
- probe queue depth over hours
- probe drops over hours
- occupancy drift over hours

Purpose:

- detect slow degradation
- detect leaks
- detect queue or occupancy drift

---

## 7. Queries To Back Dashboards

The dashboards need concrete queries, not just ideas.

Examples using current metrics:

- requests/sec total:
  `sum(rate(prequal_proxy_requests_total[1m]))`

- requests/sec by route:
  `sum by (route_key) (rate(prequal_proxy_requests_total[1m]))`

- error rate:
  `sum(rate(prequal_proxy_requests_total{status_code!~"2.."}[1m])) / sum(rate(prequal_proxy_requests_total[1m]))`

- p95 latency by route:
  `histogram_quantile(0.95, sum by (le, route_key) (rate(prequal_proxy_request_duration_seconds_bucket[1m])))`

- p99 latency by route:
  `histogram_quantile(0.99, sum by (le, route_key) (rate(prequal_proxy_request_duration_seconds_bucket[1m])))`

- p99.9 latency by route:
  `histogram_quantile(0.999, sum by (le, route_key) (rate(prequal_proxy_request_duration_seconds_bucket[1m])))`

- backend selections by route/backend:
  `sum by (route_key, backend, algorithm) (rate(prequal_proxy_backend_selection_total[1m]))`

- probe sent/sec:
  `rate(prequal_probes_sent_total[1m])`

- probe success/sec:
  `rate(prequal_probes_succeeded_total[1m])`

- probe failures by reason:
  `sum by (reason) (rate(prequal_probes_failed_total[1m]))`

- dropped probes by reason:
  `sum by (reason) (rate(prequal_probes_dropped_total[1m]))`

- queue depth:
  `prequal_probe_queue_depth`

- pool occupancy by route:
  `prequal_pool_occupancy`

- reconcile latency p95:
  `histogram_quantile(0.95, sum by (le) (rate(prequal_controller_reconciliation_duration_seconds_bucket[5m])))`

If node-exporter is present, add standard CPU/memory panels.

---

## 8. Result Schema Assets

Create:

- `benchmark/results/.gitkeep`
- `benchmark/results/schema/run-metadata.json`
- `benchmark/results/schema/run-summary.json`

Every run directory should contain:

- `run-metadata.json`
- `k6-summary.json`
- `prometheus-export/`
- `kubectl-top.txt`
- `routes.json`
- `controller-logs.txt`
- `backend-logs.txt`
- `notes.md`
- `report.md`

Optional:

- `grafana-screenshots/`

Raw data matters more than screenshots.

---

## 9. Report Assets

Create:

- `benchmark/report/templates/run-report.md`
- `benchmark/report/templates/campaign-report.md`

Run report sections:

- metadata
- scenario
- environment
- algorithm/config
- load profile
- key metrics
- charts
- anomalies
- conclusion

Campaign report sections:

- executive summary
- scenario matrix
- comparison tables
- chart gallery
- limitations
- public-safe wording

---

## 10. Fault-Injection Assets

`prequal-backend:latest` now implements `FAULT_PROBE_MODE` via env-driven mode in the Rust backend. Set `FAULT_PROBE_MODE` to one of the four values to activate the corresponding fault on the `/probe` endpoint:

- `FAULT_PROBE_MODE=timeout` — probe handler sleeps for `FAULT_TIMEOUT_MS` milliseconds (default 300000, i.e. 5 minutes) before responding, simulating a slow backend; set `FAULT_TIMEOUT_MS=<shorter>` on the backend Deployment for partial-timeout behavior
- `FAULT_PROBE_MODE=500` — probe returns HTTP 500, simulating a backend that rejects probes
- `FAULT_PROBE_MODE=malformed` — probe returns a non-JSON body, exercising the controller's parse-error path
- `FAULT_PROBE_MODE=stale_timestamp` — probe returns a valid JSON body with a timestamp offset into the past by `FAULT_STALE_OFFSET_MS` milliseconds (default 60000), exercising staleness detection

The `/work` endpoint is unaffected in all modes.

Rebuild the backend image before applying fault manifests:

```bash
cd backend && docker build -t prequal-backend:latest .
```

Future extension: per-request fault injection (e.g., fault only N% of probes) is not yet implemented. If desired, add a `FAULT_PROBE_RATE` env var and apply the fault probabilistically in the probe handler.

---

## 11. Missing Instrumentation

To improve evidence quality, add metrics for:

- controller workqueue depth
- controller workqueue retries
- controller sync failures
- empty-pool fallback selections
- per-route fallback selections
- backend-side request count
- backend-side request latency histogram
- backend-side error count

If these are not added, the campaign can still start, but the evidence will be weaker.

---

## 12. Agent Ownership Checklist

Give each AI agent a clean, non-overlapping asset set.

### Agent A: Observability

Creates:

- Prometheus config
- Grafana provisioning
- dashboard JSONs
- node-exporter integration if desired

### Agent B: Harness

Creates:

- rate-ramp, burst, long-duration, overload scripts
- campaign runner script

### Agent C: Fault Injection

Creates:

- fault backend or backend modes
- fault manifests

### Agent D: Results

Creates:

- metadata schema
- collector script
- archive script

### Agent E: Reporting

Creates:

- report templates
- chart scripts
- report renderer

### Agent F: External Baseline

Creates:

- one external ingress deployment
- same workload application
- same run matrix

---

## 13. Start Gate

You are ready to start a serious evidence campaign when all of these are true:

- benchmark manifests work
- open-loop script works
- multi-route script works
- results directory layout is defined
- Prometheus can scrape controller metrics
- Grafana dashboards render live data
- at least one long-duration script exists
- at least one fault-injection path exists

Until then, you can still benchmark, but you are not fully ready for publication-grade evidence.
