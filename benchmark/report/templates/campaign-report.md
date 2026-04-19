# Campaign Report

## Executive Summary

<!-- Fill in after reviewing all runs in this campaign. -->

Summary of findings across all scenarios and algorithms. Reference the claim level
supported by this evidence set (see public-claim-playbook.md section 2).

Recommended public wording (from public-claim-playbook.md section 17):

> In our ingress-controller implementation and benchmark setup, `prequal` improved
> tail latency over `round-robin` and `least-connections` under heterogeneous backend load.

> Across the tested workloads, `prequal` showed better tail-latency behavior than the
> implemented baselines while preserving route isolation and stable control-plane behavior.

---

## Scenario Matrix

| campaign_id | scenario | algorithm | status | run_count | p95_ms | p99_ms |
|-------------|----------|-----------|--------|-----------|--------|--------|
| <!-- fill --> | <!-- fill --> | <!-- fill --> | <!-- fill --> | <!-- fill --> | <!-- fill --> | <!-- fill --> |

---

## Comparison Tables

### p95 Latency by Algorithm

| Scenario | prequal p95 (ms) | round-robin p95 (ms) | least-connections p95 (ms) |
|----------|-----------------|----------------------|---------------------------|
| <!-- fill --> | | | |

### p99 Latency by Algorithm

| Scenario | prequal p99 (ms) | round-robin p99 (ms) | least-connections p99 (ms) |
|----------|-----------------|----------------------|---------------------------|
| <!-- fill --> | | | |

### Error Rate by Algorithm

| Scenario | prequal error % | round-robin error % | least-connections error % |
|----------|----------------|---------------------|--------------------------|
| <!-- fill --> | | | |

---

## Chart Gallery

<!-- Place rendered charts in ./charts/*.png and reference below -->

![Latency comparison](./charts/latency.png)
![Throughput comparison](./charts/throughput.png)
![Resource usage](./charts/resources.png)
![Probe behavior](./charts/probes.png)
![Control plane](./charts/control_plane.png)

---

## Limitations

- Results were collected in a single environment unless otherwise noted.
- No external ingress baseline (NGINX, Envoy, Traefik) was included in this campaign.
- Single-node or limited-node clusters may not reflect production-scale behavior.
- Short-duration runs (< 5 min) should not be used as sole evidence for tail-latency claims.
- Add any run-specific anomalies or caveats here.

---

## Public-Safe Wording

From public-claim-playbook.md section 17:

**Safe:**
> In our ingress-controller implementation and benchmark setup, `prequal` improved tail
> latency over `round-robin` and `least-connections` under heterogeneous backend load.

**Safe if fully executed:**
> Across the tested workloads, `prequal` showed better tail-latency behavior than the
> implemented baselines while preserving route isolation and stable control-plane behavior.

**Not yet safe:**
> This is the best load-balancing algorithm.
> This proves general superiority across ingress controllers.
> This is production-proven better than existing ingress systems.
