# Prequal: Benchmark Report (frozen 2026-04-20)

This is the publishable summary of the prequal benchmark campaign as of 2026-04-20. The campaign is **frozen** at this point — no further runs are planned under the current scope. Everything below is backed by committed evidence in `benchmark/results/` and narrated in the investigation logs at `benchmark/investigations/`.

---

## 1. Current claim

Two statements we can support:

- **Claim 1 (supported).** In the paper-aligned regime — 16 backends, 16× capacity skew between fast and slow replicas, I/O-bound service times — our implementation of prequal delivers a large and reproducible p99 tail-latency advantage over both `round-robin` and `least-connections`. The advantage reproduces across **two cluster topologies on the same host** (E-A single-host kind with controller colocated on a worker; E-B multi-node kind with controller isolated on the control-plane and backends topology-spread across workers). The decision-rule 2× threshold is cleared in both C2 (open-loop 500 rps) and C3 (rate ramp 100→1500 rps).

- **Claim 2 (supported, and deliberately symmetric).** In a **small-fleet CPU-bound regime** — 4 backends, SHA256-bound workload, 30-VU closed-loop — our implementation of prequal does **not** win. It pays a ~25% throughput overhead and does not improve tail latency. This is an honest characterisation, not a failure to report: the algorithm's benefits are regime-dependent, and we report both sides of that.

## 2. Headline numbers

### C2 heterogeneous-open-loop (500 rps × 300 s, 5 reps interleaved, IO-bound, skew=16, 14 fast + 2 slow)

| algorithm          | E-A p99 ms | E-B p99 ms | E-A p99.9 ms | E-B p99.9 ms |
|--------------------|-----------:|-----------:|-------------:|-------------:|
| **prequal**        | **80.60**  | **94.20**  | 127.49       | 272.38       |
| round-robin        | 807.12     | 807.32     | 834.90       | 887.90       |
| least-connections  | 802.46     | 802.84     | 808.82       | 1006.78      |
| **prequal vs best baseline (p99)** | **10.0×** | **8.6×**  |              |              |

### C3 heterogeneous-ramp (100→1500 rps, 370 s, 5 reps interleaved)

| algorithm          | E-A p99 ms | E-B p99 ms | E-A p99.9 ms | E-B p99.9 ms |
|--------------------|-----------:|-----------:|-------------:|-------------:|
| **prequal**        | **117.34** | **123.39** | 808.59       | 824.08       |
| round-robin        | 833.56     | 831.58     | 1249.13      | 1250.41      |
| least-connections  | 802.25     | 867.93     | 815.34       | 1596.51      |
| **prequal vs best baseline (p99)** | **7.1×** | **6.8×**   |              |              |

Throughput is tied across all three algorithms in both campaigns (~500 rps C2, ~695 rps C3). p50 is identical (~53 ms, the fast service time). The entire advantage is in the tail, which is exactly what the Prequal paper predicts.

### Per-backend selection, prequal (E-B C2)

14 fast backends share ~96 % of traffic; 2 slow backends receive **<0.1 sel/s each** (effectively blackholed). `prequal_selection_algorithm_total{algorithm="random_fallback"}` is **0** on every run — the pool never starves.

## 3. Caveats kept equally visible

The following are not footnotes — they are part of the claim.

- **Both environments share the same Docker host.** E-A and E-B differ in cluster topology (controller placement, pod spread) but run on the same Docker Desktop VM, same kernel, same hardware. This is **strong testbed evidence, not final cross-infrastructure proof**. A Claim-Level-B writeup by `public-claim-playbook.md` section 13's standard still wants an independent cloud or bare-metal cluster.
- **The backend is a simulator.** `IO_BOUND_MODE=1` replaces the SHA256 loop with `tokio::time::sleep(iterations × 50 µs)`. Real service time has variance, retries, backpressure, and connection-pool effects. Our numbers are cleaner than real-world numbers will be.
- **The regime is specific.** 14 fast + 2 slow at 16× skew is the paper's regime, which the algorithm was designed for. On **uniform-capacity** workloads (`workload-uniform.yaml`) all three algorithms converge on the fast service time; no separation is expected and none observed. On **small-fleet CPU-bound** workloads (C1), prequal loses by ~25% throughput (see Claim 2). The regime matters.
- **"Controller isolated on control-plane" is better E-B than E-A, but still not independent.** E-B's control-plane shares the Docker VM with the workers. The kube-apiserver, etcd, and kube-scheduler are neighbours of the controller on that node. This shows up as a slightly elevated prequal p99.9 on E-B (272 ms vs 127 ms on C2) — not enough to affect the verdict, but visible.
- **Methodology is non-optional.** The original 3-rep sequential runs on pre-pivot heterogeneous workload showed prequal *losing* by 10× until we fixed methodology (interleaved order, controller reset, env capture). The same data under the controlled protocol showed the algorithm tied, and under the paper-aligned regime showed the 10× win we claim above. Pool-state leakage between sequential runs is a real failure mode and any reproduction attempt needs the controlled protocol: `benchmark/scripts/run_interleaved_campaign.sh` with `RESET_CONTROLLER=1 POOL_RESET_WARMUP_SEC=15`.

## 4. What this testbed proves

- Prequal's HCL selection correctly identifies and avoids slow replicas across 16 backends, in both cluster topologies, under steady-state (C2) and past-saturation ramp (C3). Per-backend selection rates confirm the algorithm's core behavior.
- The algorithm's benefit is reproducible across two cluster topologies on the same host, with consistent baseline behavior. `public-claim-playbook.md` section 6's "2 environments minimum" bar is met on this testbed.
- The controller is cheap. CPU profiling under prequal load shows no user-code function exceeding 1 % flat; mutex contention is ~1.1 µs per request. The cost that does exist (~25 % on C1) localises to network / probe competition, not to hot paths in the controller itself.

## 5. What this testbed does NOT prove

- That results generalise to independent hosts. See caveats above.
- That results generalise to real-service backends with variance, backpressure, cold caches, or downstream-RPC failure modes.
- That results generalise to fleet sizes meaningfully different from 16 (smaller or much larger).
- That the algorithm is the right default for every regime. On small-fleet CPU-bound workloads it is not.

## 6. Pointers to deeper evidence

Investigation logs (read top-to-bottom; each is self-contained):

- [`investigations/2026-04-19-c2-tail-spike.md`](investigations/2026-04-19-c2-tail-spike.md) — the original methodology failure, 7 competing hypotheses, root cause = pool-state leakage across sequential runs. Closed.
- [`investigations/2026-04-20-regime-pivot.md`](investigations/2026-04-20-regime-pivot.md) — the regime pivot (IO-bound backend, 16 backends, skew=16) + E-B cross-environment confirmation. Closed, outcome A.
- [`investigations/2026-04-20-prequal-overhead-profiling.md`](investigations/2026-04-20-prequal-overhead-profiling.md) — pprof CPU / mutex / block / heap analysis of the controller under prequal load. Closed.

Raw data:

- `results/aggregated/2026-04-20-C2-pivot-heterogeneous.json` — E-A first-pass C2
- `results/aggregated/2026-04-20-C3-pivot-ramp.json` — E-A first-pass C3
- `results/aggregated/2026-04-20-C2-eb-heterogeneous.json` — E-B C2 (primary)
- `results/aggregated/2026-04-20-C3-eb-ramp.json` — E-B C3 (primary)
- `results/aggregated/2026-04-19-C1-controlled-uniform-smoke.json` — C1 uniform (prequal loses, background)
- 30 per-run directories each under E-A and E-B, fully reproducible with `run_interleaved_campaign.sh`

Dashboards (Grafana PNG exports via the `grafana-image-renderer` sidecar):

- `results/screenshots/2026-04-20-C2-eb/` — 6 dashboards, primary evidence
- `results/screenshots/2026-04-20-C3-eb/` — 6 dashboards, primary evidence

Cluster config for reproduction:

- `kind-config-e-b.yaml` — canonical E-B topology
- `manifests/controller-benchmark.yaml` — controller pinned to control-plane
- `manifests/workload-heterogeneous.yaml` — 14 fast + 2 slow IO-bound, topology spread

## 7. Status: frozen

**No further runs are planned under this scope.** The benchmarking matrix has C1 + C2 + C3 closed. Campaigns C4 (multi-route isolation), C5 (long-duration stability), C6 (overload), C7 (churn), C8 (fault injection — backend code exists, never exercised live), and C9 (NGINX external baseline — infrastructure exists, never deployed) remain as future work.

If you want to strengthen the claim further, the two highest-leverage next steps are:

1. Run the same controlled C2 + C3 pivot protocol on an **independent cloud or bare-metal cluster** to clear the same-host caveat.
2. Run **C4 multi-route isolation** on E-B to validate the route-scoped pool design, since we claim it in the writeup but haven't exercised it in the current evidence chain.

Neither is in scope as of the freeze.

---

*Report committed as part of the campaign freeze. For the live matrix and per-row status, see [`benchmarking-matrix.md`](benchmarking-matrix.md).*
