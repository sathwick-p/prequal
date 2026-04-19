# Prequal: Public-Claim Benchmarking and Stress-Test Playbook

## 1. Purpose

This document is for the stronger standard:

- not just “run some benchmarks”
- not just “see whether the code works”
- but “collect evidence that is credible enough to support a public engineering claim”

This is the playbook to use when you want other AI agents to:

- execute large benchmark campaigns
- run long-duration stress tests
- collect metrics and raw evidence
- generate charts and reports
- set up local observability with Prometheus and Grafana

This document is intentionally stricter than `benchmarking.md`.

---

## 2. Claim Standard

Be explicit about the strength of the claim.

### Claim level A: Internal engineering result

Acceptable wording:

- `prequal` outperformed `round-robin` and `least-connections` in our test setup
- under our workloads, `prequal` improved p99 latency

Evidence bar:

- one environment
- repeatable scripts
- saved raw results

### Claim level B: Public engineering writeup

Acceptable wording:

- in this ingress-controller implementation, under the tested workloads, `prequal` reduced tail latency compared with `round-robin` and `least-connections`

Evidence bar:

- multiple environments
- raw data retained
- limitations clearly stated
- reproducible commands/config included

### Claim level C: Strong public algorithm claim

Example wording:

- this is a strong or superior load-balancing algorithm

Do **not** make this claim unless you have:

- multiple environments
- multiple workload classes
- repeated runs
- statistically defensible comparisons
- explicit external baselines
- transparent limitations

Current recommendation:

- safe target is Claim level B after full execution of this playbook
- Claim level C is **not** justified yet

---

## 3. What Is Still Missing For A Strong Public Claim

The current repo is in good shape for benchmarking, but not yet sufficient to “prove” broad superiority.

The main gaps are:

- no external baseline beyond your own proxy implementations
- no published long-duration campaign yet
- no public result bundle format yet
- no Prometheus/Grafana stack checked into the repo yet
- no backend-side exported metrics beyond probe response behavior
- no explicit fault-injection backend for stale timestamps / malformed probe payloads
- no queue-depth metric for the controller workqueue
- no node/system-level observability stack defined in-repo

Implication:

- you can publish an engineering evaluation
- you should not yet publish a universal superiority claim

---

## 4. Must-Have Deliverables Before Publishing

Before any serious public writeup, produce all of these:

- benchmark manifests
- exact commands used
- environment description
- raw load-generator outputs
- Prometheus metric snapshots or time-series export
- summary tables
- graphs for p50/p95/p99/p99.9
- graphs for request rate and error rate
- graphs for probe behavior
- graphs for controller/backend CPU and memory
- a limitations section
- a result archive per run

Recommended artifact layout:

```text
results/
  2026-04-19-uniform-open-loop/
    run-metadata.json
    k6-summary.json
    prometheus-export/
    kubectl-top.txt
    controller-logs.txt
    backend-logs.txt
    charts/
    report.md
```

---

## 5. Required Benchmark Campaigns

To support a strong public writeup, run all of these campaigns.

### Campaign 1: Baseline correctness and smoke

Purpose:

- validate benchmark harness
- validate manifests
- validate metrics collection

Runs:

- uniform workload
- all three algorithms
- short runs only

Duration:

- 5-10 minutes total

### Campaign 2: Algorithm comparison under controlled load

Purpose:

- establish the main comparison set

Runs:

- uniform workload
- heterogeneous workload
- all three algorithms
- open-loop traffic

Duration:

- at least 3-5 runs per scenario
- 2-5 minutes measurement window per run

### Campaign 3: Saturation and tail-latency ramp

Purpose:

- find where each algorithm breaks down

Runs:

- open-loop fixed-rate load
- steadily rising rate
- heterogeneous workload

Need to capture:

- onset of p95 growth
- onset of p99 growth
- error onset
- probe queue stress

### Campaign 4: Multi-route isolation

Purpose:

- verify route-scoped state isolation

Runs:

- multi-route workload
- weighted host traffic
- all three algorithms

Need to capture:

- route-local pool occupancy
- route-local backend selection
- no evidence of state bleed across routes

### Campaign 5: Long-duration stability

Purpose:

- prove the system holds up over time, not just for short bursts

Runs:

- uniform workload
- heterogeneous workload
- `prequal` and at least one baseline

Duration:

- 1 hour minimum
- ideally 6 hours
- stretch target: 24 hours

Need to capture:

- memory drift
- queue buildup
- pool behavior over time
- error spikes
- reconcile spikes

### Campaign 6: Extreme stress

Purpose:

- identify hard failure modes

Runs:

- highest sustainable open-loop rates
- high concurrency
- route scale
- backend scale

Need to capture:

- maximum throughput before instability
- failure mode when overloaded
- whether failures degrade gracefully

### Campaign 7: Churn and control-plane stability

Purpose:

- validate ingress-controller behavior during change

Runs:

- live traffic
- backend scale up/down
- ingress updates
- endpoint churn

Need to capture:

- reconcile latency
- user-visible error spikes
- recovery time

### Campaign 8: Probe degradation and fault handling

Purpose:

- validate probe-path resilience

Runs:

- probe timeouts
- probe 500s
- malformed probe responses
- disabled background probing

Need to capture:

- probe failure counts
- dropped probes
- latency effect
- fallback behavior

---

## 6. Environments Required

For public evidence, use at least two environments.

### Environment A: Local or `kind`

Purpose:

- script validation
- smoke checks
- rapid iteration

Not sufficient for publication by itself.

### Environment B: Multi-node cluster

Purpose:

- primary engineering comparison environment

Examples:

- multi-node `kind`
- local VM cluster
- cloud test cluster

This is the minimum serious environment.

### Environment C: Independent second serious environment

Purpose:

- reduce “works only in one setup” risk

Examples:

- if B is cloud, C should be local multi-node
- if B is local multi-node, C should be cloud

Without this, your public conclusions are weaker.

---

## 7. Observability Stack Required

The current controller already exposes metrics on:

- `/metrics` on the debug server

Current debug endpoints:

- `/metrics`
- `/healthz`
- `/readyz`
- `/routes`

For serious campaigns, add a local observability stack:

- Prometheus
- Grafana

Recommended supporting components:

- node-exporter
- cAdvisor or kubelet/cAdvisor metrics if available
- kube-state-metrics if you want pod/deployment state in dashboards

If you stay entirely local, a Docker Compose stack is acceptable.
If you run in Kubernetes, a simple local monitoring namespace is acceptable.

---

## 8. Prometheus Requirements

Prometheus should scrape:

- controller `/metrics`
- backend metrics if/when added
- node/system metrics
- Kubernetes pod/container metrics where available

Current controller metrics already exported:

- `prequal_probes_sent_total`
- `prequal_probes_succeeded_total`
- `prequal_probes_failed_total`
- `prequal_probes_dropped_total`
- `prequal_pool_occupancy`
- `prequal_probe_queue_depth`
- `prequal_selection_algorithm_total`
- `prequal_proxy_requests_total`
- `prequal_proxy_request_duration_seconds`
- `prequal_proxy_backend_selection_total`
- `prequal_proxy_no_route_total`
- `prequal_proxy_no_backends_total`
- `prequal_controller_reconciliation_total`
- `prequal_controller_reconciliation_duration_seconds`
- `prequal_active_backends`

Prometheus retention recommendation:

- smoke/local: 6-12 hours
- serious campaign: 24-72 hours

Scrape interval recommendation:

- 5s for serious tests
- 1s only if you need very high-resolution queue/latency correlation and can afford the overhead

---

## 9. Grafana Dashboard Requirements

Create dashboards for at least these categories.

### Dashboard 1: Request Overview

Panels:

- request rate
- error rate
- p50 latency
- p95 latency
- p99 latency
- p99.9 latency
- requests by route
- requests by status code

### Dashboard 2: Algorithm Behavior

Panels:

- selection counts by algorithm
- backend selection counts by route
- active backends by route
- pool occupancy by route
- no-backend events
- no-route events

### Dashboard 3: Probe System

Panels:

- probes sent/sec
- probes succeeded/sec
- probe failures by reason
- dropped probes by reason
- probe queue depth
- pool occupancy by route

### Dashboard 4: Control Plane

Panels:

- reconciliation count
- reconciliation duration
- request errors during churn windows
- active backends during scale events

### Dashboard 5: Resource Usage

Panels:

- controller CPU
- controller memory
- backend CPU
- backend memory
- node CPU
- node memory
- network throughput if available

### Dashboard 6: Long-Duration Stability

Panels:

- memory over time
- queue depth over time
- latency over time
- error rate over time
- probe drops over time

---

## 10. Instrumentation Still Worth Adding Before Public Release

These are not all required before starting, but they materially improve the credibility of the results.

### Must-add soon

- controller workqueue depth metric
- controller workqueue processing latency metric
- backend-side request metrics endpoint
- backend-side request duration histogram
- backend-side request count / error count

### Very useful

- route-key labeled fallback-selection counter
- explicit metric for empty-pool fallback
- per-route probe age summary
- per-route maintenance cycle statistics

### Nice to have

- build info metric
- git SHA metric
- benchmark mode enabled metric

---

## 11. Extreme Stress and Long-Duration Standards

For a real public claim, do not stop at 2-5 minute runs.

### Minimum

- one 1-hour run per main scenario

### Better

- one 6-hour run for:
  - `prequal`
  - `round-robin`
  - heterogeneous workload

### Strong

- one 24-hour long-duration run for `prequal`
- one 24-hour comparison run for one baseline

For extreme stress, define explicit phases:

1. below saturation
2. at expected saturation edge
3. above saturation
4. sustained overload

Need to measure:

- does throughput flatten
- does latency explode
- does memory drift
- does queue depth keep growing
- do probe drops spike
- does the controller remain responsive

---

## 12. Statistical and Reporting Discipline

Do not publish single-run screenshots.

For every scenario:

- repeat 3-5 times minimum
- report median and spread
- include worst run, not just best run

Report structure should include:

- scenario definition
- environment description
- algorithm/config
- run count
- raw throughput
- p50/p95/p99/p99.9
- error rate
- CPU/memory
- probe overhead
- notes on anomalies

Use exact dates and commit SHAs in reports.

---

## 13. External Baseline Requirement

If you want stronger public claims, add at least one external baseline.

Examples:

- NGINX Ingress
- Envoy-based ingress/gateway
- Traefik

You do not need to claim algorithmic superiority to them immediately, but you should at least compare proxy overhead and general routing behavior against one known system.

Without an external baseline, your public claim is limited to:

- “better than our internal baselines”

not:

- “better than modern ingress balancing generally”

---

## 14. AI Agent Work Packages

Split the work into independent agent-owned packages.

### Agent 1: Observability Stack

Own:

- Prometheus config
- Grafana config
- scrape targets
- dashboard JSONs

Deliver:

- `observability/` assets
- startup instructions
- dashboard import instructions

### Agent 2: Benchmark Harness

Own:

- `k6` scripts
- scenario configs
- rate ramp variants
- long-duration scripts

Deliver:

- script set
- run matrix
- output conventions

### Agent 3: Fault Injection

Own:

- malformed probe responder
- timeout/failure responder
- stale timestamp test path

Deliver:

- fault-injection manifests
- scenario instructions

### Agent 4: Results Collector

Own:

- result directory layout
- metadata schema
- automated metric export
- artifact bundling

Deliver:

- `results/` schema
- collector scripts
- archive generation

### Agent 5: Report Generator

Own:

- summary tables
- chart generation
- Markdown report template

Deliver:

- per-run report
- cross-run comparison report
- final publication draft

### Agent 6: External Baseline

Own:

- one external ingress deployment
- comparable workload setup
- same load scripts / same environment

Deliver:

- baseline comparison data
- fairness notes

---

## 15. Result Schema

Each run should produce a machine-readable metadata file.

Recommended fields:

```json
{
  "run_id": "2026-04-19-heterogeneous-open-loop-prequal-01",
  "date_utc": "2026-04-19T12:00:00Z",
  "git_sha": "COMMIT_SHA",
  "environment": "kind-multinode",
  "scenario": "heterogeneous-open-loop",
  "algorithm": "prequal",
  "duration_seconds": 300,
  "target_rate_rps": 500,
  "workload_manifest": "benchmark/manifests/workload-heterogeneous.yaml",
  "load_script": "benchmark/k6/open_loop.js",
  "controller_env": {},
  "backend_env": {},
  "notes": ""
}
```

Do not rely on memory or ad hoc filenames.

---

## 16. Publication Checklist

Before publishing any claim, verify all of these:

- raw results retained
- commit SHA recorded
- environment recorded
- exact commands recorded
- limitations section written
- at least one long-duration run completed
- at least one extreme-stress run completed
- at least one multi-route isolation run completed
- at least one churn run completed
- repeated runs completed
- charts generated from raw data, not screenshots

If any of these are missing, the public claim should be softened.

---

## 17. Recommended Public Wording

Safe wording:

- In our ingress-controller implementation and benchmark setup, `prequal` improved tail latency over `round-robin` and `least-connections` under heterogeneous backend load.

Still safe if fully executed:

- Across the tested workloads, `prequal` showed better tail-latency behavior than the implemented baselines while preserving route isolation and stable control-plane behavior.

Not yet safe:

- This is the best load-balancing algorithm.
- This proves general superiority across ingress controllers.
- This is production-proven better than existing ingress systems.

---

## 18. Immediate Next Steps

1. Add Prometheus and Grafana assets to the repo.
2. Add result schema and archive scripts.
3. Add long-duration and rate-ramp `k6` scripts.
4. Add fault-injection manifests.
5. Run the first full campaign:
   - uniform
   - heterogeneous
   - multi-route
   - churn
6. Generate a first engineering report from raw data.

Only after that should you decide whether the evidence is strong enough for a public writeup.
