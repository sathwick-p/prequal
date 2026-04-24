# Building a Prequal-Style Load Balancer in Go: From Paper to Kubernetes Controller to Benchmarks

The paper behind this project has one of the best titles in systems research: [*Load is not what you should balance: Introducing Prequal*](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf).

That title is not just rhetorical. It is the whole argument.

The usual instinct in distributed systems is to spread traffic so every backend looks equally busy. If one replica is at 20% CPU and another is at 80%, the obvious move is to feed the idle one and avoid the busy one. In a clean, single-tenant world that sounds correct. In a messy shared environment, it can be the wrong objective.

Prequal takes a different view. Instead of asking, "Which backend looks least loaded?" it asks, "Which backend is least likely to make the next request wait?" That sounds subtle, but it changes the entire design:

- the signal becomes request latency plus requests-in-flight, not CPU
- the load balancer actively probes backends instead of waiting for passive observations
- selection becomes quantile-based and latency-aware instead of purely load-aware
- the real win shows up in the tail, not necessarily in average latency

This repository is a Go reimplementation of that idea, packaged as a Kubernetes ingress controller and backed by a fairly serious benchmark harness. It is not Google's production Stubby implementation from the paper. But it is much more than a toy port. It has a control plane, a reverse proxy dataplane, a route-local probe pool, a Rust benchmark backend that exposes Prequal probe metadata, Prometheus metrics, Grafana dashboards, benchmark scripts, investigation logs, and enough failed experiments to make the final results believable.

This post is a technical walkthrough of both the paper and this codebase:

- what problem Prequal is trying to solve
- how the Go implementation is structured
- how the hot-cold lexicographic rule is encoded in code
- how the async probing path works
- how the benchmarking infrastructure was built
- where the implementation intentionally diverges from the paper
- what the benchmark results actually say, including where Prequal loses

I also want to make one thing explicit up front: the most interesting part of this repo is not just that it implements Prequal. It is that the repo preserves the engineering process of getting to a result you can trust. There are wrong runs, methodological mistakes, a regime pivot, overhead profiling, and a final bounded claim rather than a vague "it worked on my machine." That is rare, and it is worth studying.

![System overview](benchmark/diagrams/system-overview.png)

## The paper's core idea

The NSDI paper starts from a real production observation: in large multi-tenant systems, balancing CPU evenly across replicas is not the same thing as minimizing latency. A backend can look "lightly loaded" according to a smoothed resource metric and still be a bad place to send the next request because it is on a noisy host, has a growing queue, or has just crossed into a regime where service time gets ugly.

Prequal's answer is to use two signals:

- `RIF`: requests in flight
- `latency`: a backend-reported estimate of recent service latency

And then to sample those signals by probing backends asynchronously.

The selection rule from the paper is the part worth remembering. Prequal does not combine latency and RIF into one score by default. It uses a lexicographic rule:

1. Split candidates into "cold" and "hot" using an RIF quantile threshold.
2. If any cold candidates exist, pick the one with the lowest latency.
3. If every candidate is hot, pick the one with the lowest RIF.

That rule matters because it captures something simple and useful:

- latency is the best tie-breaker among backends that are not yet visibly congested
- once everything is congested, queue depth wins and you should pick the least loaded one

The paper calls this the hot-cold lexicographic rule, or HCL. In this repo, that is the heart of the algorithm.

The other big paper idea is async probing. Synchronous probing would put an extra network hop in the critical path of every request. Prequal instead probes off the request path, stores recent probe observations in a bounded pool, and reuses them enough to be cheap without letting them go stale.

That is exactly the design this repository implements.

## What this repo actually builds

At runtime this project is one Go binary with two jobs:

- a Kubernetes controller that watches `Ingress` and `EndpointSlice`
- an HTTP reverse proxy that receives requests and selects backends

Around that, the repo includes:

- a Rust backend used for controlled benchmarks
- benchmark manifests for uniform and heterogeneous workloads
- `k6` scripts for open-loop, ramp, burst, overload, multi-route, and long-duration tests
- Prometheus and Grafana assets
- frozen benchmark reports and investigation logs

The top-level structure is clean and maps well to the architecture:

```text
controller/      Kubernetes reconciliation and route state
server/          Reverse proxy and request-path selection
loadbalancer/    Prequal, least-connections, round-robin, probe logic, RIF, latency, pools
backend/         Rust benchmark backend exposing /work and /prequal/probe
observability/   Prometheus metrics
tree/            Host/path trie for ingress routing
benchmark/       Manifests, k6 scripts, dashboards, reports, investigations, raw results
```

The entrypoint in [`main.go`](main.go) wires all of that together:

```go
store := controller.NewBackendIPStore()
ctrl := controller.NewController(factory, store, queue)

tracker := &loadbalancer.RIFTracker{}
latencyTracker := loadbalancer.NewLatencyTracker()

cfg := loadbalancer.DefaultProbeConfig()
cfg.ApplyEnv()

pools := pool.NewRoutePools(pool.PoolConfig{
    MaxSize:     cfg.PoolMaxSize,
    MaxAge:      cfg.PoolMaxAge,
    ReuseLimit:  cfg.PoolReuseLimit,
    QRIF:        cfg.QRIF,
    MaxProbeAge: cfg.MaxProbeAge,
}, cfg.PoolMaintenanceInterval)

prober := loadbalancer.NewProber(pools, store, cfg, stop)
proxyServer := server.NewProxyServerWithConfig(
    ctrl.GetRouter(), store, selectors, tracker, latencyTracker, pools, prober, serverCfg,
)
```

That composition is a good summary of the design:

- the controller owns route and endpoint discovery
- the proxy owns request forwarding
- the load balancer owns route-local state and selection policy

## Control plane: translating Kubernetes into route state

The control plane lives in `controller/`. It uses shared informers to watch `Ingress` and `EndpointSlice`, then builds two in-memory structures:

- a host/path router
- a route-key to endpoint list store

The important point is that the controller does not directly configure nginx or write files. It builds local state for the in-process proxy.

### Watching ingress and endpoints

`controller.NewController` registers event handlers for both resource types:

- ingress add, update, delete
- endpointslice add, update, delete

On an ingress event, the controller queues the ingress key for reconciliation.
On an endpoint event, it finds which ingresses depend on the service and requeues those.

That dependency mapping is stored in `serviceToIngress`, which is what lets endpoint churn trigger only the routes that care about it.

The ingress reconciliation path in [`controller/controller.go`](controller/controller.go) does four things:

1. filters to this controller's class
2. parses rules into route specs
3. updates the router
4. refreshes the endpoint store for each referenced service/port

The filtering logic is pragmatic rather than doctrinaire. It accepts:

- `spec.ingressClassName == "prequal"`
- legacy `kubernetes.io/ingress.class: prequal`
- a fallback label `ingress.class=prequal`

That is a small detail, but it says a lot about the repo. This is implementation meant to survive real manifests, not just ideal ones.

### Route matching with a trie

The router in [`controller/router.go`](controller/router.go) delegates path matching to `tree/`, which implements a segment trie. Each host gets a `HostConfig` with a list of paths plus a trie built from those paths.

`tree.Match` supports:

- exact paths
- prefix paths
- longest-prefix semantics
- default-host fallback when a specific host is missing

That means route resolution is:

1. match host
2. walk the trie by URL segments
3. prefer exact matches, otherwise keep the best prefix match

It is a simple design, but the separation is clean: Kubernetes objects are converted once into `RouteSpec`, and the request path never has to understand Kubernetes types.

### Endpoint storage is route-local

The controller stores endpoints in `BackendIPStore`, keyed by a route key derived from namespace, service, and port:

- `namespace/service`
- `namespace/service:port`
- `namespace/service:portName`

That matters because the load-balancing state is also route-local. If two ingress routes point at different services, their probe history does not mix. If two routes point at the same service but different ports, their state stays separate too.

That isolation is one of the repo's strongest engineering decisions. It shows up in the code, the tests, and the benchmark design.

## Dataplane: the reverse proxy request path

The dataplane lives in [`server/server.go`](server/server.go). This is where a request enters the proxy, gets matched to a route, resolves candidate backends, triggers async probes, selects one backend, and is forwarded with `httputil.ReverseProxy`.

The flow is:

1. normalize host
2. match route from the trie
3. fetch candidate backends from the endpoint store
4. trigger async probes for that route
5. pick a backend using the requested algorithm
6. increment RIF counters
7. proxy the request
8. record observed latency locally

That is all in one request handler, which makes the architecture easy to follow.

The selection branch is especially important:

```go
func (p *ProxyServer) selectBackend(routeKey, algo string, backends []*controller.Endpoint) (*controller.Endpoint, error) {
    switch algo {
    case "prequal", "":
        entry, err := p.pools.Select(routeKey, backends)
        if err != nil {
            return nil, err
        }
        p.pools.IncrementRIF(routeKey, entry.Backend)
        return entry.Endpoint, nil
    default:
        sel, exists := p.selectors[algo]
        if !exists {
            return p.selectBackend(routeKey, "prequal", backends)
        }
        return sel.Select(backends)
    }
}
```

A few design choices here are worth calling out.

First, Prequal is the default. If the ingress annotation `lb/algo` is empty, the proxy uses Prequal.

Second, other algorithms are genuinely pluggable. The repo includes:

- round-robin
- least-connections
- prequal

That is what makes the benchmark harness clean. The same controller, proxy, transport, and backend stack can be benchmarked with different selection rules by patching one ingress annotation.

Third, the proxy increments both a global RIF tracker and the selected pool entry's RIF view. That keeps the request path's immediate state and the pool's sampled state reasonably aligned.

## The actual Prequal implementation

The implementation is split across:

- `loadbalancer/prober.go`
- `loadbalancer/pool/pool.go`
- `loadbalancer/pool/pools.go`
- `loadbalancer/rif.go`
- `loadbalancer/latency.go`
- `loadbalancer/config.go`

This is the core of the repo.

### Route-local probe pools

The paper's async probing design only works if sampled state is bounded and per-route. That is what `RoutePools` does: it owns one `ProbePool` per route key.

Each `ProbeEntry` holds:

- backend address
- endpoint pointer
- RIF
- latency
- probe timestamp
- `UsesLeft`

`UsesLeft` is the local implementation of probe reuse. A probe can be selected a limited number of times before it is evicted.

The pool is bounded by both size and age:

- `MaxSize`
- `MaxAge`
- `MaxProbeAge`

That gives the repo three protection mechanisms against stale decisions:

- cap how many probe samples are retained
- remove entries that are too old in wall-clock terms
- remove entries once they have been reused enough

### HCL in code

The HCL selection rule is implemented directly in [`loadbalancer/pool/pool.go`](loadbalancer/pool/pool.go). This is the most important code in the project.

The key selection logic looks like this:

```go
threshold := rifs[idx]

var bestCold *ProbeEntry
var bestHot *ProbeEntry
allHot := true

for i, e := range pool.entries {
    if e.RIF <= threshold {
        allHot = false
        if bestCold == nil || e.Latency < bestCold.Latency {
            bestCold = e
            bestColdIndex = i
        }
        continue
    }
    if bestHot == nil || e.RIF < bestHot.RIF {
        bestHot = e
        bestHotIndex = i
    }
}

selected := bestCold
if allHot {
    selected = bestHot
}
```

That is a faithful and readable expression of the paper's idea:

- compute the RIF quantile threshold
- treat `RIF <= threshold` as cold
- among cold entries, minimize latency
- if nothing is cold, minimize RIF

It is not trying to be clever. That is a good thing. Algorithm code gets dangerous when it becomes hard to explain. This one stays direct.

The fallback behavior is also worth noting. If the pool has fewer than two entries, the code falls back to a random backend from the full backend list. That is how the system behaves before warmup or after starvation:

```go
if len(pool.entries) < 2 {
    ep := allBackends[rand.Intn(len(allBackends))]
    return &ProbeEntry{Backend: ep.String(), Endpoint: ep}, len(pool.entries), nil
}
```

The benchmark campaign explicitly tracks how often that fallback happens. In the decisive Prequal runs, it is zero, which matters because otherwise a "Prequal win" might secretly be a random-selection run.

### Async probing

`loadbalancer.Prober` is the other half of the design. It is responsible for:

- sending HTTP probes to `/prequal/probe`
- decoding backend-reported RIF and latency
- rejecting stale probe responses
- feeding entries into the route-local pool
- keeping pools warm in the background

The request path never blocks on probe completion. Instead, `TriggerProbes(routeKey)` enqueues work:

```go
func (pr *Prober) TriggerProbes(routeKey string) {
    if routeKey == "" {
        return
    }
    n := pr.probesForQuery()
    for i := 0; i < n; i++ {
        pr.enqueueProbe(routeKey)
    }
}
```

Workers consume those route keys, sample a backend for the route, call the probe endpoint, and add a `ProbeEntry` to the right pool.

The configuration lives in [`loadbalancer/config.go`](loadbalancer/config.go), and the defaults are important because they define the repo's behavior:

- pool size: `16`
- pool max age: `1s`
- reuse limit: `3`
- `QRIF`: `0.75`
- probes per query: `1.0`
- probe workers: `8`
- background interval: `100ms`
- probe timeout: `100ms`
- max probe age: `2s`

Those values are not arbitrary, but they are also not identical to the paper's defaults. That matters later.

### RIF and latency tracking

Besides backend-reported probe data, the proxy maintains its own local trackers:

- `RIFTracker` uses `sync.Map` and `atomic.Int64`
- `LatencyTracker` keeps a per-backend circular buffer and reports a local median

These are used for two things:

- supporting least-connections
- optionally seeding the probe pool when the async prober is disabled

That second point is a nice engineering touch. The pool can be bootstrap-seeded from local observations if the async prober is absent, but once the prober exists, backend probes become the authoritative source.

## The benchmark backend is part of the algorithm story

The Rust backend in [`backend/src/main.rs`](backend/src/main.rs) is not just a load target. It is part of the implementation model.

It exposes three endpoints:

- `POST /work`
- `GET /health`
- `GET /prequal/probe`

`/work` simulates the backend's actual service time.
`/prequal/probe` exposes the two signals Prequal needs.

This is a smart design because it gives you a backend whose internal state is visible in exactly the way the algorithm expects. That makes controlled experiments possible.

### Work mode: CPU-bound or I/O-bound

The backend has two modes:

- CPU-bound SHA256 loop
- I/O-bound sleep mode

That switch is controlled by `IO_BOUND_MODE`.

In CPU-bound mode, each request burns CPU with repeated hashing.
In I/O-bound mode, each request sleeps for `iterations * IO_BOUND_BASE_US`.

That one switch ends up being central to the benchmark story. On small CPU-bound fleets, Prequal loses. In the paper-aligned I/O-bound skewed regime, it wins decisively.

### Probe responses are RIF-conditioned

The backend tracks current RIF with an atomic counter and stores recent latency samples in five buckets:

- `0`
- `1`
- `2..3`
- `4..7`
- `8+`

The probe handler looks at the current RIF, chooses the matching bucket, and returns the median latency from that bucket or the nearest non-empty bucket.

That means the probe latency signal is not a raw median across all recent requests. It is conditioned on queue depth. That is a meaningful design choice because it makes the latency signal more sensitive to the backend's current regime.

The probe response shape is simple:

```json
{
  "rif": 3,
  "latency_median_ms": 12.5,
  "timestamp_ms": 1710000000000
}
```

The Go prober then turns that into a `ProbeEntry`, rejects it if the timestamp is too stale, and inserts it into the right pool.

### Fault injection is built in

The backend also supports probe-fault modes:

- timeout
- HTTP 500
- malformed JSON
- stale timestamp

That makes it possible to test whether the prober correctly records failures, drops bad data, and avoids poisoning the pool with stale observations.

This is one of those details that separates a serious benchmark backend from a throwaway fake service.

## The observability path is first-class

This repo instruments the controller and proxy heavily enough that you can explain a benchmark result rather than just report it.

The Prometheus metrics in [`observability/metrics.go`](observability/metrics.go) cover:

- total requests and request latency
- backend selections by route and algorithm
- no-route and no-backend events
- reconciliation counts and durations
- probes sent, succeeded, failed, dropped
- probe queue depth
- pool occupancy
- selection algorithm usage
- active backend count

And the debug server in [`debug.go`](debug.go) exposes:

- `/metrics`
- `/routes`
- `/healthz`
- `/readyz`
- `pprof` endpoints

That instrumentation is what makes the benchmark investigation credible. The repo can answer questions like:

- Did Prequal actually avoid the slow replicas?
- Was the pool starving?
- Were requests falling back to random?
- Was the controller CPU-bound?
- Was mutex contention the problem?

In this project, observability is not decoration. It is how the results were debugged.

## Benchmarking was not an afterthought

The benchmark harness in `benchmark/` is extensive enough that it deserves to be treated as part of the software, not just support files.

There are several traffic models:

- `steady_state.js`
- `open_loop.js`
- `rate_ramp.js`
- `burst.js`
- `overload.js`
- `long_duration.js`
- `multi_route.js`

And there are matching manifests for:

- uniform workloads
- heterogeneous workloads
- multi-route workloads
- route-scale
- long-duration
- fault injection

The most important design choice in the benchmark harness is that comparisons are made by holding everything constant except the algorithm annotation on the ingress.

That gives a fair comparison:

- same controller binary
- same route matching
- same transport
- same backend images
- same cluster
- same load script
- same observability stack

Only the backend selection rule changes.

### The controlled protocol matters

The repo's investigation logs show something important: methodology was part of the result.

An early heterogeneous run made Prequal look dramatically worse than the baselines. The tempting conclusion would have been that the algorithm or implementation was wrong.

It turned out the main problem was protocol:

- algorithms were run sequentially instead of interleaved
- controller state leaked across runs
- probe pools were not reset cleanly between repetitions

The fix became the canonical benchmark protocol:

- interleaved algorithm order
- controller rollout restart before every run
- warmup period before measurement
- full metadata capture per run

That protocol is encoded in [`benchmark/scripts/run_interleaved_campaign.sh`](benchmark/scripts/run_interleaved_campaign.sh) and [`benchmark/scripts/run_campaign.sh`](benchmark/scripts/run_campaign.sh).

This is the kind of thing most benchmark writeups bury or ignore. Here it is preserved and scriptable. That is excellent engineering.

### Why open-loop matters

The decisive C2 benchmark uses `k6` constant-arrival-rate mode:

```js
export const options = {
  scenarios: {
    open_loop: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
}
```

That is a better choice than closed-loop when the point is to expose queueing behavior. Closed-loop traffic self-throttles when latency rises. Open-loop keeps pushing at the target rate and makes tail failures visible.

The ramp benchmark does the complementary thing: it increases arrival rate in stages until the system crosses its comfortable regime.

Together, those two tests are enough to answer the important question: does Prequal help under skew and near saturation, which is exactly where the paper says it should?

## What the benchmark results say

The repo's final claim is deliberately bounded, and I think it is the right one.

### Small CPU-bound fleet: Prequal loses

In the small-fleet CPU-bound regime, this implementation does not win.

The report's C1 numbers are:

| algorithm | throughput rps | p99 ms |
|---|---:|---:|
| prequal | 4162 | 29.86 |
| round-robin | 5366 | 20.22 |
| least-connections | 4965 | 23.48 |

That is roughly a 25% throughput deficit versus round-robin.

The repo did not hide that result. It profiled it.

The overhead investigation concludes that:

- there is no hot path dominating controller CPU
- mutex contention is negligible
- heap use is tiny
- the measurable cost is mostly diffuse probe/network competition in a regime where the algorithm does not have enough diversity or skew to pay for itself

That is exactly the sort of negative result worth publishing.

### Paper-aligned skewed I/O-bound regime: Prequal wins hard

The story changes once the benchmark is moved closer to the paper's assumptions:

- 16 backends instead of 4
- 14 fast, 2 slow
- 16x service-time skew
- I/O-bound backend mode
- open-loop and ramp traffic

In the E-B heterogeneous open-loop campaign, the median `p99` numbers are:

| algorithm | p99 ms | p99.9 ms |
|---|---:|---:|
| prequal | 94.20 | 272.38 |
| round-robin | 807.32 | 887.90 |
| least-connections | 802.84 | 1006.78 |

That is an 8.6x `p99` improvement versus the best baseline.

In the E-B ramp campaign:

| algorithm | p99 ms | p99.9 ms |
|---|---:|---:|
| prequal | 123.39 | 824.08 |
| round-robin | 831.58 | 1250.41 |
| least-connections | 867.93 | 1596.51 |

That is still a 6.8x `p99` improvement.

The selection-rate data explains why. Prequal pushes traffic almost entirely to the fast backends and drives the two slow replicas down to nearly zero selections per second. Round-robin, by definition, keeps giving the slow pair their fair share. Least-connections improves the `p95`, but still reacts too slowly to avoid queueing at the slow replicas, so the tail remains pinned near their service time.

This is the strongest part of the repo's evidence: the mechanism lines up with the result.

### The win is in the tail, not the center

One subtle but important point from the data is that `p50` is basically the same across algorithms in the winning regime. The advantage is almost entirely in `p99` and `p99.9`.

That is exactly what you would expect if the algorithm is avoiding pathological queueing rather than making the median request faster.

It is also why Prequal is interesting. If your median is already fine, the only remaining reason to build a more sophisticated load balancer is to keep a minority of requests from getting stuck behind bad backend choices.

![Backend selection](benchmark/diagrams/backend-selection.png)

## Where this repo diverges from the paper

This repo is faithful to the paper's central ideas, but it is not a line-by-line reproduction. The frozen report documents the differences clearly, and they matter.

The most important divergences are:

### `QRIF` default is `0.75`, not the paper's `~0.84`

The paper's baseline uses `Q_RIF = 2^(-0.25) ≈ 0.84`.
This repo defaults to `0.75`.

That is within the paper's recommended band and probably a minor difference, but it is still a difference.

### Probes per query is `1.0`, not `3` or `5`

The paper uses `3` probes per query in testbed experiments and mentions `5` in YouTube production.
This repo defaults to `1.0`.

That choice was motivated by keeping probe overhead reasonable on a small local cluster, but it is a substantial departure.

### Probe reuse is a fixed constant

The paper derives reuse behavior from a formula involving pool size, fleet size, probe rate, and removal rate.
This repo hardcodes `PoolReuseLimit = 3`.

That is a reasonable engineering choice for a local implementation, but it means the repo is approximating one part of the paper's mechanics rather than reproducing it exactly.

### Probe removal is maintenance-driven

The paper frames removal as a per-query process.
This repo performs cleanup and "remove worst" behavior on a maintenance tick plus reuse depletion.

That keeps work off the request hot path, which is sensible for Go code in a proxy, but it is another behavioral difference.

### Backend probing is not yet sampling without replacement

The report points out a latent issue: `ProbeRandom` picks one backend at a time with `rand.Intn`, so if probes-per-query were raised above `1`, the implementation would not yet match the paper's "sample without replacement" requirement.

That is a great example of the repo being honest with itself. The implementation works for the current defaults, but the report still documents where fidelity would break under a different sweep.

### The benchmark regime is a proxy, not the paper's exact environment

The paper's real story is about large multi-tenant services, CPU allocation, and antagonist load. This repo's decisive benchmark result comes from static service-time skew on an I/O-bound synthetic backend.

That does not invalidate the result. It just narrows the claim:

- this repo validates the mechanism under a paper-aligned regime
- it does not claim to reproduce Google's exact production environment

That is the right level of honesty.

## Code quality and test coverage

This repo does not just benchmark behavior. It also has a decent unit-test surface.

The Go test suite covers:

- route matching
- ingress reconciliation
- endpoint synchronization
- RIF tracking
- latency tracking
- least-connections behavior
- probe decoding and staleness handling
- HCL selection behavior
- request forwarding semantics

The Rust backend test suite covers:

- fault mode parsing
- median calculation
- I/O-bound duration calculation

I ran both suites while writing this post:

- `go test ./...` passed
- `cargo test` in `backend/` passed with 11 tests

That does not prove the benchmark claims, but it does mean the repo has a real correctness floor under its core components.

## What I think this repo gets right

There are a few engineering decisions here that I think are especially good.

### 1. Route-local state isolation

Keeping one probe pool per route key is the right design. It prevents cross-route contamination and makes multi-route benchmarks meaningful.

### 2. Benchmark evidence is preserved, not curated

The repo keeps:

- negative runs
- methodology failures
- pivot decisions
- overhead investigations
- frozen report snapshots

That makes the final claim much more believable than a repo that only keeps the winning screenshots.

### 3. The implementation is readable

The controller, proxy, pool, and prober are not tangled together. You can read each subsystem in isolation and understand its job.

### 4. The benchmark backend is purpose-built

The Rust backend is not a generic echo server. It is shaped around the algorithm: RIF tracking, latency buckets, probe responses, and fault injection are first-class.

### 5. The repo distinguishes algorithm correctness from regime validity

That is a subtle but important distinction. The small-fleet CPU-bound result does not mean the implementation is broken. It means the regime does not reward the mechanism enough. The repo eventually makes that distinction explicitly.

## What I would improve next

If I were continuing this project, the next engineering tasks I would prioritize are:

### Sample probes without replacement

That would make higher probe rates faithful to the paper and remove a latent correctness gap.

### Make probe reuse policy formula-driven

A dynamic `b_reuse` derived from fleet and pool settings would make the implementation closer to the paper and reduce hand-tuned behavior.

### Add an automatic small-fleet fallback

The overhead investigation strongly suggests Prequal should not be the default when the fleet is tiny. A route with four backends simply does not give HCL enough room to shine. Falling back automatically below a threshold would make the implementation more practical.

### Sweep more paper-aligned parameters

The report already points at obvious sweeps:

- probes per query
- `QRIF`
- reuse limit
- max probe age

The repo has enough automation that these would be straightforward.

### Reproduce on independent infrastructure

The current E-A and E-B environments are two topologies on the same host. That is good testbed discipline, but it is still the same Docker Desktop VM underneath. The next credibility jump would be running the same campaigns on an independent cloud or bare-metal cluster.

## The main lesson

If I had to reduce this project to one engineering lesson, it would be this:

The hard part of a systems project like this is not implementing the algorithm. It is building enough surrounding machinery to know whether the algorithm actually worked, why it worked, and when it stopped working.

The Go code for Prequal itself is not huge. The real work is in everything around it:

- Kubernetes reconciliation
- route-local state management
- probe freshness rules
- observability
- benchmark control
- metadata capture
- investigation discipline

That is why this repo is worth reading even if you never deploy this exact controller. It is a good case study in turning a systems paper into an implementation that can survive contact with reality.

![Benchmarking journey](benchmark/diagrams/benchmarking.png)

## Closing

The paper argues that balancing CPU is often the wrong objective, and this repo is a solid demonstration of what it takes to test that claim in code.

It implements the core Prequal ideas faithfully enough to matter:

- async probing
- route-local probe pools
- HCL backend selection
- RIF plus latency as the decision signal

And then it does something more valuable than most reimplementations: it shows the messy path from first result to believable result.

The final conclusion is stronger because it is narrower.

This implementation of Prequal is not universally better than round-robin or least-connections.
It loses on a small CPU-bound fleet.
It wins decisively in a paper-aligned high-skew regime.
The controller overhead is real but small.
The benchmark methodology itself can make or break the apparent outcome.

That is a much better result than a generic "Prequal is faster."
It is the kind of conclusion you can actually build on.

## References

- Wydrowski, Kleinberg, Rumble, Archer. [*Load is not what you should balance: Introducing Prequal*](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf), NSDI 2024.
- Repo benchmark report: [`benchmark/REPORT.md`](benchmark/REPORT.md)
- Tail-spike investigation: [`benchmark/investigations/2026-04-19-c2-tail-spike.md`](benchmark/investigations/2026-04-19-c2-tail-spike.md)
- Regime pivot investigation: [`benchmark/investigations/2026-04-20-regime-pivot.md`](benchmark/investigations/2026-04-20-regime-pivot.md)
- Overhead profiling investigation: [`benchmark/investigations/2026-04-20-prequal-overhead-profiling.md`](benchmark/investigations/2026-04-20-prequal-overhead-profiling.md)

## Suggested New Excalidraw Diagrams

If I were adding more diagrams in the same style as the existing repo visuals, I would add these three.

### 1. Request-path sequence diagram

Purpose: show the exact control flow for one proxied request.

Canvas layout:

- left to right lifelines: `Client`, `Prequal Proxy`, `Router`, `Route Pool`, `Prober Queue`, `Backend`, `Metrics`
- top to bottom time flow

Flow to draw:

1. `Client -> Prequal Proxy`: `POST /work Host: bench.local`
2. `Prequal Proxy -> Router`: `Match(host, path)`
3. `Router -> Prequal Proxy`: `routeKey=prequal-benchmark/bench-heterogeneous:8080`
4. `Prequal Proxy -> Prober Queue`: `TriggerProbes(routeKey)` as a dashed async arrow
5. `Prequal Proxy -> Route Pool`: `Select(routeKey)`
6. Inside Route Pool, show a small inset box:
   `cold = entries with RIF <= quantile(QRIF)`
   `pick min latency among cold`
   `else pick min RIF`
7. `Route Pool -> Prequal Proxy`: `selected backend`
8. `Prequal Proxy -> Backend`: proxy request
9. `Backend -> Prequal Proxy`: response
10. `Prequal Proxy -> Route Pool`: `IncrementRIF(routeKey, backend)` and later local latency update
11. `Prequal Proxy -> Metrics`: request duration, backend selection, algorithm counters
12. `Backend -> Prober Queue`: separate dashed note showing probe worker independently calls `/prequal/probe`

Visual notes:

- solid arrows for request-path work
- dashed arrows for async probe work
- highlight the critical path in one color and background probing in another

### 2. Probe-pool lifecycle diagram

Purpose: explain why the pool is bounded and how a probe enters, gets reused, and is evicted.

Canvas layout:

- left side: "probe entry arrives"
- center: bounded pool box with 6 to 8 sample entries
- right side: eviction rules
- bottom: selection path

Elements to draw:

- a box titled `ProbeEntry`
  fields: `backend`, `RIF`, `latency`, `timestamp`, `usesLeft`
- arrow into a `ProbePool(routeKey)` container
- pool container annotated with:
  `MaxSize = 16`
  `MaxAge = 1s`
  `MaxProbeAge = 2s`
  `ReuseLimit = 3`
- inside the pool, visually separate "cold" and "hot" entries by color
- show one selected entry having `usesLeft` decremented
- show three eviction reasons:
  - oldest removed when pool is full
  - stale removed by age
  - selected entry removed when `usesLeft == 0`
- include the alternating maintenance removal:
  `RemoveWorst(): oldest / highest-load alternation`

Visual notes:

- use small badges like `cold`, `hot`, `stale`, `evict`
- this diagram should feel like a state machine plus data-structure view combined

### 3. Benchmark evolution timeline

Purpose: summarize the experimental journey from wrong result to bounded claim.

Canvas layout:

- one horizontal timeline with four large phases
- each phase gets a labeled card above or below the line

Phases to draw:

1. `Initial C2 run`
   note: `Prequal looked 10x worse`
   icon: red warning triangle
2. `Methodology investigation`
   notes:
   `Sequential runs`
   `Pool-state leakage`
   `No reset between reps`
   icon: magnifying glass
3. `Controlled protocol`
   notes:
   `Interleaved order`
   `Controller restart per run`
   `Warmup`
   `Metadata capture`
   icon: wrench
4. `Regime pivot`
   notes:
   `16 backends`
   `14 fast + 2 slow`
   `IO-bound mode`
   `16x skew`
   icon: upward arrow
5. `Final bounded conclusion`
   notes:
   `Prequal loses on small CPU-bound fleet`
   `Prequal wins 6.8x-8.6x on p99 in paper-aligned regime`
   icon: checkmark

Add two thin parallel lanes under the timeline:

- `Code changes`
- `Benchmark protocol changes`

Under `Code changes`, show that the biggest final result shift was not from HCL rewrites but from backend mode and environment shape.
Under `Benchmark protocol changes`, show that methodology fixes alone removed the false negative.

Visual notes:

- this should read like an engineering postmortem timeline, not a marketing roadmap
- use neutral colors for protocol changes and stronger colors only for final validated outcomes
