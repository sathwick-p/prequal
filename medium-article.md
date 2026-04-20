# I built a custom load balancer in Go. The hardest part wasn't the code.

If you've ever stared at a tail-latency graph wondering why your service is slow at the p99 even though every backend looks healthy, you've probably wondered whether your load balancer is part of the problem.

Most of them are. Not because they're broken, but because round-robin and least-connections are blind to the thing you actually care about: which backend is about to be slow.

I spent the last few weeks building a probe-driven load balancer in Go, putting it in front of a Kubernetes workload, and benchmarking it against the standard policies. This is what I learned. The code parts are easy. The benchmarking part is where almost everyone gets fooled.

## Why default policies are blind

Round-robin is the default for a reason: it's simple, it's stateless, it's hard to misconfigure. It also doesn't know that the third backend in the rotation is currently CPU-pinned because some other workload is sharing the node, or that the seventh backend is in the middle of a slow GC pause.

Least-connections is one step better: it tracks how many requests it has in flight to each backend and prefers the less loaded ones. But it's still inferring backend state from its own bookkeeping. If a backend stops dequeuing requests but the connections look open from the load balancer's perspective, least-connections will keep sending traffic at it for a while.

The class of failure that hurts you in production usually isn't "a backend is dead" — health checks catch that. It's "a backend is alive but materially slower than its peers, and the load balancer can't see the difference." When that happens, the symptom shows up in your p99, not your median.

## The probe-driven idea

Here's the general shape: instead of inferring backend state, you ask each backend directly. The backend exposes a small endpoint that returns its current load (requests-in-flight, recent latency, queue depth, whatever signal you can get). Your load balancer pings these endpoints continuously in the background, keeps a small per-route pool of recent observations, and at request time picks the backend that looks healthiest.

The selection step matters. The naive choice is "pick the lowest latency," but that breaks down when an idle backend reports low latency right before its queue fills up. A better approach is hot-cold lexicographic ordering: sort backends by their requests-in-flight, partition them into a "cold" set (low load) and a "hot" set, and within the cold set pick the one with the lowest recent latency. If everyone is hot, fall back to the least loaded.

This is the core of a paper called Prequal (NSDI '24). I won't recap the whole thing here, but the idea generalizes: probe + RIF-aware selection beats blind distribution in regimes where backend variance matters.

## Skeleton in Go

Let's get the shape down. You need four things.

### 1. A way to know what your backends are

In Kubernetes that's an informer over `EndpointSlice`. The pattern is well-trodden:

```go
factory := informers.NewSharedInformerFactory(clientset, 60*time.Second)
sliceInformer := factory.Discovery().V1().EndpointSlices().Informer()

sliceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:    onSliceChange,
    UpdateFunc: func(_, obj interface{}) { onSliceChange(obj) },
    DeleteFunc: onSliceChange,
})
```

Build an in-memory map keyed by route, holding a slice of current backend addresses. Replace the slice atomically when the informer fires so the data plane never sees a half-updated set.

### 2. An async prober

The prober has one job: keep the per-route pool of probe observations fresh. Bound it. If you don't, the act of measuring the system starts to dominate the system.

```go
type Prober struct {
    pools         *RoutePools
    workCh        chan string
    workers       int
    httpClient    *http.Client
    interval      time.Duration
}

func (p *Prober) Run(stop <-chan struct{}) {
    for i := 0; i < p.workers; i++ {
        go p.worker(stop)
    }
    ticker := time.NewTicker(p.interval)
    for {
        select {
        case <-stop:
            return
        case <-ticker.C:
            for _, route := range p.pools.Routes() {
                select {
                case p.workCh <- route:
                default: // queue full, drop and increment a metric
                }
            }
        }
    }
}

func (p *Prober) worker(stop <-chan struct{}) {
    for {
        select {
        case <-stop:
            return
        case route := <-p.workCh:
            p.probeOne(route)
        }
    }
}
```

The "drop on full queue" path is important. Instrument it as a metric. You will need it later.

### 3. A selection function

This is the algorithm. The full HCL version is maybe 30 lines:

```go
func (pool *ProbePool) Select(allBackends []*Endpoint) (*ProbeEntry, error) {
    pool.mu.Lock()
    defer pool.mu.Unlock()

    if len(pool.entries) < 2 {
        // pool too small, fall back to random over all backends
        ep := allBackends[rand.Intn(len(allBackends))]
        return entryFromEndpoint(ep), nil
    }

    rifs := collectRIFs(pool.entries)
    sort.Slice(rifs, func(i, j int) bool { return rifs[i] < rifs[j] })
    threshold := rifs[int(pool.QRIF*float64(len(rifs)))]

    var bestCold, bestHot *ProbeEntry
    for _, e := range pool.entries {
        if e.RIF <= threshold {
            if bestCold == nil || e.Latency < bestCold.Latency {
                bestCold = e
            }
        } else {
            if bestHot == nil || e.RIF < bestHot.RIF {
                bestHot = e
            }
        }
    }
    if bestCold != nil {
        return bestCold, nil
    }
    return bestHot, nil
}
```

`QRIF` is a knob: how aggressively you partition cold vs hot. 0.75 is a reasonable starting point. Don't tune it until you've benchmarked.

### 4. Per-route state, not global state

This is the bug I almost didn't catch. If you have multiple routes (different services behind your load balancer), put the probe pool per-route. Don't share a global pool.

The reason is subtle. If route A is heavily loaded and you observe its backends as hot, those observations will flow into a shared pool. When a request to route B comes in, the pool says "everything is hot," and you make a routing decision based on a backend's behavior under a workload that has nothing to do with route B's traffic.

In a single-route benchmark you'll never catch this. In production with multiple ingresses, it'll quietly poison your selections.

```go
type RoutePools struct {
    mu    sync.RWMutex
    pools map[string]*ProbePool
    cfg   PoolConfig
}

func (rp *RoutePools) PoolFor(route string) *ProbePool {
    rp.mu.RLock()
    p, ok := rp.pools[route]
    rp.mu.RUnlock()
    if ok {
        return p
    }
    rp.mu.Lock()
    defer rp.mu.Unlock()
    if p, ok = rp.pools[route]; ok {
        return p
    }
    p = NewProbePool(rp.cfg)
    rp.pools[route] = p
    return p
}
```

That's the shape. About 800 lines of Go in total once you've added the proxy itself, the request handling, and the metrics. Manageable in an afternoon. None of it is the hard part.

## Now the actually hard part: benchmarking it

Here's how I almost convinced myself my implementation was broken.

I set up a heterogeneous workload: 3 fast backends, 1 slow backend (4× slower than the others), 500 requests per second for 5 minutes. Three reps per algorithm: prequal, round-robin, least-connections. Ran them sequentially. All three prequal reps first, then all three round-robin, then all three least-connections.

The result:

| algorithm | p99 ms | p99.9 ms |
|---|---:|---:|
| prequal (mine) | **854** | **1822** |
| round-robin | 284 | 1802 |
| least-connections | 165 | 562 |

My implementation was supposed to *reduce* tail latency. It was making it 5× worse instead.

If you've never been here before, your instinct will be to go hunting for a bug in the selection function. That's the wrong instinct. Before touching the algorithm code, check the variance shape. One of my prequal reps had p95 = 17 ms. Another had p95 = 196 ms. Same code, same workload, same rate, same backends. An algorithm bug doesn't produce that much swing between identical runs.

What does produce that kind of swing? Run-to-run state contamination.

## The benchmark trap

When you run three reps of prequal in a row, the second rep starts with a probe pool warmed up by the first rep. That pool has data about which backends were fast and slow during the previous run. Some of that data is still relevant; some of it isn't. The interaction with the algorithm's "reuse this probe up to N times" behavior is non-trivial.

Then when you switch to round-robin, you're starting fresh — round-robin has no probe pool to warm. You're not comparing algorithms on equal footing. You're comparing "algorithm with stale state" against "algorithm with no state."

The fix isn't a code change. It's a protocol change.

```bash
# wrong: sequential, no reset
for algo in prequal round-robin least-connections; do
  for rep in 1 2 3; do
    set_algorithm "$algo"
    run_benchmark
  done
done

# right: interleaved, full reset before every single run
for rep in 1 2 3 4 5; do
  for algo in prequal round-robin least-connections; do
    set_algorithm "$algo"
    kubectl rollout restart deploy/load-balancer
    kubectl rollout status deploy/load-balancer --timeout=60s
    sleep 15  # warmup
    run_benchmark
  done
done
```

When I reran the same heterogeneous workload with this protocol, no code changes:

| algorithm | p99 ms | p99.9 ms |
|---|---:|---:|
| prequal | 15.22 | 54.40 |
| round-robin | 14.40 | 49.88 |
| least-connections | 15.30 | 59.19 |

That's a 56× p99 reduction on prequal. The algorithm code was correct the entire time.

## What to measure besides p99

p99 alone will mislead you. A few other things you should be capturing per run:

**Per-backend selection rate.** If your "smart" load balancer is supposed to avoid a slow backend, prove that it actually is. Export a counter incremented on every selection, labeled by backend address. If the slow backend is still receiving 25% of traffic, your algorithm isn't doing what you think.

**Random-fallback rate.** Most probe-driven algorithms have a "pool is empty, just pick randomly" fallback path. If the pool is consistently empty (because your probe rate is too low, or your prober is dropping work, or your backends are returning errors), your algorithm has silently degenerated into random selection. You will not catch this from p99 alone. Instrument it.

**Throughput.** If your algorithm gets a great p99 but achieves it by serving fewer requests, you haven't won anything. Closed-loop benchmarks make this especially easy to miss because each VU paces itself based on response time.

**Run-to-run variance, not just medians.** If your worst rep is 5× your best rep, your median doesn't mean what you think. Always report the min and max across reps.

## The regime question

Here's the one that surprised me most. Even after fixing the methodology, my implementation didn't always win.

On a small fleet (4 backends) with CPU-bound work (SHA256 loops on the backends), the probe-driven version was about 25% slower on throughput than round-robin and slightly worse on p99. Profiling the load balancer found nothing — no hot path, no mutex contention, no allocation pressure. The cost was somewhere else.

The somewhere else turned out to be the backends themselves. Probe traffic competes with user traffic for backend CPU. Each probe is cheap on your side and non-trivial on the backend side. With 4 backends running a CPU-bound workload, the aggregate probe load was eating about 25% of the headroom, and your users felt it as latency.

When I swapped the backends to I/O-bound work (sleep instead of SHA256 — same response time, no CPU contention), the overhead disappeared entirely and the algorithm started winning. Same code, same configuration, different regime.

This is the part nobody puts in the README of their cool new load balancer. **Probe-driven load balancing is regime-specific.** It needs:

- Enough backends for probe-sampling to give you real information (probably 8+ minimum, more is better)
- Heterogeneity in the backends — if everyone is identical, there's nothing to discriminate
- Backends where probe overhead isn't competing with user traffic for the same scarce resource
- A workload where tail latency actually matters more than peak throughput

Get those right and you'll see large p99 wins. Get them wrong and you'll either see no benefit or pay overhead. The honest advice is to run your own benchmarks against your own workload, with the controlled protocol above, and let the data tell you whether the algorithm is right for your situation.

## Things I'd do differently if I started over

A few practical takeaways from going through this once.

Build the metrics before the algorithm, not after. The per-backend selection counter and the random-fallback counter would have saved me days of debugging. They cost nothing in the hot path (atomic counter increments) and they're the only way to confirm your algorithm is doing what you think.

Treat per-route isolation as a correctness requirement, not a feature. Sharing probe state across routes is a real bug that's invisible in single-route benchmarks. You'll only catch it after you've shipped.

Write down hypotheses before touching code when a benchmark surprises you. The catastrophic first result I got could have sent me on a multi-day code hunt. What stopped me was the variance shape — and I only noticed the variance because I had the discipline to look at min/max instead of just median.

Pair every positive result with the regime it lives in. "Algorithm X is better than algorithm Y" is a meaningless sentence. "Algorithm X is better than algorithm Y on heterogeneous fleets of 16+ backends with I/O-bound service times" is meaningful.

## Worked example

If you want to see all of this end-to-end with code, dashboards, and the full investigation log of the catastrophic first benchmark, I put the whole thing on GitHub: [sathwick-p/prequal](https://github.com/sathwick-p/prequal). It's the Kubernetes ingress controller version of the algorithm above, with the controlled benchmark protocol baked into a script, the negative result preserved next to the positive one, and the methodology investigation written up as its own document.

You don't need to use it as a library or a product. It's there as a worked example of "what does a properly benchmarked custom load balancer actually look like, including the parts that broke."

Building the algorithm is an afternoon. Knowing whether it works is the rest of the project.
