# Marketing notes for prequal

File is gitignored. Drafts only, rewrite in your own voice before posting.

Repo URL to use everywhere:  
`https://github.com/sathwick-p/prequal`

Pre-staged images (already in the repo, link directly to raw GitHub URLs):

- Hero (C2-eb request-overview, the bimodal tail):  
  `https://raw.githubusercontent.com/sathwick-p/prequal/main/benchmark/results/screenshots/2026-04-20-C2-eb/request-overview.png`
- Algorithm-behavior (mechanism evidence):  
  `https://raw.githubusercontent.com/sathwick-p/prequal/main/benchmark/results/screenshots/2026-04-20-C2-eb/algorithm-behavior.png`
- Honesty shot (C1, where prequal loses):  
  `https://raw.githubusercontent.com/sathwick-p/prequal/main/benchmark/results/screenshots/2026-04-19-C1-controlled/request-overview.png`

---

## One-liner (positioning, reusable)

> A Kubernetes ingress controller that implements the Prequal paper (NSDI '24). In the paper-aligned regime it cuts p99 tail latency by about 10× vs round-robin. In a small-fleet CPU-bound regime it loses. Both results, plus the full investigation trail of a first benchmark that was wrong by an order of magnitude, are in the repo.

---

## Short tweets / X posts

Each is standalone. Pick whichever one fits the moment.

### A. The headline
> I implemented the Prequal load-balancing paper (NSDI '24) as a Kubernetes ingress controller.
>
> In the paper-aligned regime, p99 tail latency drops about 10× vs round-robin and least-connections.
>
> In a small-fleet CPU-bound regime, it loses by 25%. Both results are in the repo.
>
> https://github.com/sathwick-p/prequal

### B. The investigation hook (probably the strongest)
> My first benchmark of a Prequal implementation showed the algorithm losing by 10×, in the wrong direction.
>
> Turned out to be pool-state leakage between sequential runs. The algorithm was fine, my protocol wasn't.
>
> Full investigation trail and both results are in the repo:
> https://github.com/sathwick-p/prequal

### C. The honest-results take
> I built a Prequal load-balancing ingress controller for Kubernetes and wrote down every place I was wrong.
>
> - first benchmark showed the algorithm losing 10×, I hadn't fixed methodology
> - even after fixing, it doesn't win in small-fleet CPU-bound workloads
> - in the paper-aligned regime it wins by 10× on p99
>
> All three sit in the repo together.
>
> https://github.com/sathwick-p/prequal

### D. Show-don't-tell (image tweet)
> 15 alternating runs. Five each of prequal, round-robin, least-connections.
> Same 16-backend workload. Same rate. Same duration.
>
> p50 is the same flat line. Only the tail separates.
>
> [attach C2-eb request-overview.png]
>
> https://github.com/sathwick-p/prequal

### E. The methodology one
> Benchmark methodology lesson I paid for.
>
> Sequential algorithm runs in a load-balancing benchmark leak pool state between them. My first pass showed my implementation losing by 10×. Interleaved runs with a controller reset between each dropped p99 by 56×, no code change.
>
> https://github.com/sathwick-p/prequal

### F. Understated / short
> Kubernetes ingress controller implementing the NSDI '24 Prequal algorithm. 10× p99 win in the paper's regime, honest negative result in the CPU-bound one, full benchmark trail in the repo.
>
> https://github.com/sathwick-p/prequal

---

## Twitter thread (the story arc, ~10 tweets)

Post as a thread. Attach the hero image to tweet 1, the honesty image to tweet 7.

**1/**
> I built a Kubernetes ingress controller that implements the Prequal load-balancing paper from NSDI '24.
>
> The short version: in the paper-aligned regime, p99 tail latency drops about 10× vs round-robin and least-connections.
>
> Long version has two plot twists. 🧵
> [attach hero: C2-eb request-overview.png]

**2/**
> Prequal's idea: active probes to backends report requests-in-flight and latency, and selection uses hot-cold lexicographic (HCL) ordering across recently probed backends.
>
> Blind policies like round-robin can't see which backend is about to be slow. Prequal tries to.

**3/**
> Wrote the controller in Go. Backend serving the probe endpoint is in Rust. Kubernetes Ingress and EndpointSlice reconciliation, route-scoped probe pools, bounded async prober, Prometheus metrics, Grafana dashboards.
>
> Standard stuff, except the route-scoping matters more than it looks.

**4/**
> First benchmark run: heterogeneous backends, 4 replicas (3 fast, 1 slow), 500 rps, 3 reps per algorithm, run sequentially.
>
> prequal p99 = 854 ms
> round-robin p99 = 284 ms
> least-connections p99 = 165 ms
>
> My implementation was losing by an order of magnitude.

**5/**
> I almost went hunting for a bug in HCL. What stopped me was per-run variance. One prequal rep had p95 of 17 ms, another had 196 ms. An algorithm bug shouldn't produce that much swing between identical runs.
>
> So I wrote down every competing hypothesis before touching code.

**6/**
> Cause turned out to be methodology, not algorithm. Sequential runs leaked pool state between them, and prequal's reuse behavior caught the worst of it.
>
> Interleaved runs plus controller rollout-restart before each run plus env capture produced a 56× p99 reduction. No code change.

**7/**
> That fix resolved the disaster but surfaced a second plot twist: in a small-fleet CPU-bound regime, prequal still doesn't win. Throughput about 25% lower than round-robin, tail slightly worse.
>
> pprof found no hot path, no mutex contention, no memory pressure. So where was the cost?
> [attach honesty image: C1 controlled request-overview.png]

**8/**
> Probe traffic was competing with user traffic for the same backend CPU budget. Cheap on the controller side, non-trivial on the backend side.
>
> Prediction: I/O-bound backends should make the overhead vanish and the regime advantage appear. So I pivoted.

**9/**
> Pivot: backend gets an IO_BOUND_MODE=1 flag (tokio sleep instead of SHA256). 16 backends instead of 4. Capacity skew of 16× instead of 4×.
>
> Result: prequal p99 = 80 ms. Round-robin p99 = 807 ms. Least-connections p99 = 802 ms.
>
> That's 10×. The paper's claim reproduces.

**10/**
> Advantage held across two cluster topologies (single-host kind, multi-node kind with controller isolated). Shifted from 10.0× to 8.6×, within each environment's own variance.
>
> Caveat: both environments share a Docker host. Strong testbed evidence, not cross-infrastructure proof.

**11/ (final)**
> Full writeup is in the repo. Every run committed, every dashboard rendered, every investigation log public. The negative result sits next to the positive one, and the catastrophic first wrong one hasn't been deleted either.
>
> https://github.com/sathwick-p/prequal

---

## Reddit: r/golang

**Suggested title**

> I implemented the NSDI '24 Prequal load-balancing algorithm as a Kubernetes ingress controller in Go. 10× p99 win in the paper's regime, 25% overhead in a small-fleet CPU-bound one, full investigation trail in the repo.

**Body**

Hey r/golang,

I spent the last while building a Kubernetes ingress controller that reimplements the Prequal load-balancing algorithm from the NSDI '24 Prequal paper. The point was to find out whether the paper's tail-latency claim survives being rewritten from scratch in Go on a smaller cluster.

Three things happened:

- In the paper-aligned regime (16 backends, 16× service-time skew, I/O-bound service times), the implementation cuts p99 by 8.6× on a steady-state open-loop benchmark and 6.8× under a rate ramp vs `round-robin` and `least-connections`.
- In a small-fleet CPU-bound regime (4 backends, closed-loop, SHA256 workload), prequal loses by about 25% on throughput and doesn't improve the tail. I kept this result in the repo on purpose.
- The algorithm's advantage reproduced across two cluster topologies on the same host.

Parts that might be specifically interesting to this sub:

The controller is one Go binary doing both reconciliation and proxying. It watches `Ingress` and `EndpointSlice` via client-go informers, builds an in-memory route trie with atomic replacement, and serves the data plane through `httputil.ReverseProxy`. The hot-cold lexicographic selection happens in `loadbalancer/pool/pool.go`: bounded-size pool, age-based eviction, reuse-limit bookkeeping.

pprof cleared the controller of the overhead I was hunting. In the CPU-bound regime where prequal loses, profiling at 498 rps showed 29.6% of a single core total, no user-code function over 1% flat, about 1.1 µs of mutex contention per request, 3 MB heap. The gap wasn't in the Go code. It was probe traffic competing for backend CPU budget.

The methodology story is in the repo and ended up being the most interesting part. My first 3-rep sequential benchmark made prequal look like it was losing by an order of magnitude. Root cause was pool-state leakage between sequential runs. The fix (interleaved runs, `kubectl rollout restart` before each run, 15 s warmup) produced a 56× p99 reduction without any code change. More informative than anything I did in the selection algorithm itself.

If you want the short version: [README](https://github.com/sathwick-p/prequal/blob/main/README.md).  
If you want the narrative: [TECHNICAL_BLOG.md](https://github.com/sathwick-p/prequal/blob/main/TECHNICAL_BLOG.md).  
If you want the locked-down claim: [benchmark/REPORT.md](https://github.com/sathwick-p/prequal/blob/main/benchmark/REPORT.md).

Happy to answer questions about the Go side specifically. I've kept the repo honest about what it doesn't prove (same Docker host for both environments, backend is a simulator, positive regime is specific), so I'd rather not oversell it.

Repo: https://github.com/sathwick-p/prequal

---

## Reddit: r/kubernetes

**Suggested title**

> Custom ingress controller implementing the Prequal load-balancing algorithm: 10× p99 tail-latency win in the paper-aligned regime, 25% loss in a small-fleet CPU-bound one, full methodology and evidence trail

**Body**

Posting this for anyone interested in custom ingress controllers, load-balancing policies beyond round-robin, or just benchmark methodology war stories.

I built a Kubernetes ingress controller in Go that implements the Prequal load-balancing algorithm from NSDI '24. Active probes to backends for requests-in-flight and latency, hot-cold lexicographic selection across route-scoped probe pools. Three algorithms are supported per route via annotation (`prequal`, `round-robin`, `least-connections`) so they can be compared on the same workload.

Headline numbers (5 reps interleaved, controlled protocol, kind cluster):

- C2 heterogeneous open-loop, 500 rps, 16 backends (14 fast + 2 slow), 16× skew, I/O-bound:
  - prequal p99: 94 ms
  - round-robin p99: 807 ms
  - least-connections p99: 803 ms
- C3 rate ramp 100→1500 rps, same topology:
  - prequal p99: 123 ms
  - round-robin p99: 832 ms
  - least-connections p99: 868 ms

Per-backend selection confirms the mechanism: prequal drives traffic to the 2 slow replicas down to less than 0.1 sel/s each.

Parts that were actually hard in a Kubernetes context:

Route-scoped probe pools. An earlier version had a global pool and route B was picking backends that had only been observed under route A's load. Per-route isolation is a correctness prerequisite, not a feature request.

Atomic route replacement during reconciliation. If the proxy can see half-updated routing state under churn, you can't trust any benchmark that involves scaling events.

Controlled benchmark protocol. The first 3-rep sequential pass showed prequal losing by 10× because of pool-state leakage between sequential runs. Fixed by interleaving algorithms and issuing `kubectl rollout restart deploy/prequal-controller` plus 15 s warmup before every single run. 56× p99 reduction, no code change.

The negative result is also in the repo on purpose. On 4 CPU-bound SHA256 backends with a closed-loop 30-VU driver, prequal loses by about 25% throughput and slightly worse p99. pprof rules out any hot path in the controller. The cost is diffuse probe-vs-user traffic competition on the backends. On I/O-bound backends that competition goes away and the regime advantage appears.

Testbed notes:

- Multi-node kind cluster, controller isolated on the control-plane via toleration and nodeSelector
- Backends spread across workers via `topologySpreadConstraints`
- k6 for load generation, Prometheus and Grafana for dashboards
- `benchmark/scripts/run_interleaved_campaign.sh` and `benchmark/scripts/port_forward_scrape.sh` are the controlled-protocol entry points
- Full evidence matrix, per-run artifacts, rendered dashboard PNGs, investigation logs, all committed

Important caveat: E-A and E-B share the same Docker host. This is strong testbed evidence, not cross-infrastructure proof. A proper Claim-Level-B writeup would want reproduction on an independent cloud or bare-metal cluster.

Repo: https://github.com/sathwick-p/prequal  
Paper: https://www.usenix.org/system/files/nsdi24-wydrowski.pdf  
Hero dashboard (5 alternating runs per algorithm, tail separates by algorithm phase): https://raw.githubusercontent.com/sathwick-p/prequal/main/benchmark/results/screenshots/2026-04-20-C2-eb/request-overview.png

Happy to answer questions about the benchmark protocol, the controller design, or what I'd do differently if I was graduating this to a multi-host environment.

---

## Reddit: r/sre

This sub tends to like the methodology angle the most.

**Suggested title**

> How a 3-rep sequential benchmark made my load-balancing algorithm look 10× worse than it was: a methodology post-mortem with the fix

**Body**

Cross-posting from my Prequal ingress controller repo (Kubernetes, Go). The methodology story here is probably more useful than the algorithm result, so I'm extracting it specifically for this sub.

I was benchmarking three load-balancing algorithms (prequal, round-robin, least-connections) against each other under a heterogeneous backend workload. First pass: 3 reps per algorithm, run sequentially. All three prequal first, then all three round-robin, then all three least-connections. Standard shape.

Result made no sense.

- prequal p99 = 854 ms
- round-robin p99 = 284 ms
- least-connections p99 = 165 ms

prequal was supposed to reduce tail latency. It was making it 5× worse instead.

I was about to go hunting for a bug in the selection algorithm. What stopped me was per-run variance. One prequal rep had p95 = 17 ms, another had p95 = 196 ms. Same code, same rate, same duration, same backends. An algorithm bug shouldn't produce that much swing between identical runs.

So I wrote down the hypothesis tree before touching code:

1. Algorithm configuration is wrong (tuning problem)
2. Backend's latency reporting hides the slow replica (algorithm-fidelity problem)
3. Pool reuse amplifies stale observations
4. Probe sampling rate is too low for arrival rate
5. Sequential algorithm ordering leaks pool state between runs (methodology problem)
6. Some weird resonance at 500 rps
7. Pool is starving and falling back to random

I couldn't distinguish 5 from the algorithm-fidelity hypotheses without an interleaved pass. And I couldn't distinguish 7 from anything without capturing `random_fallback_rate` per run.

Fix: rewrote the campaign runner.

- Interleaved the algorithm order instead of running them sequentially
- kubectl rollout restart on the controller before every single run
- 15 s warmup after each restart so pools could repopulate cold
- Captured controller_env and backend_env per run so every result was reproducible
- Added per-backend selection rate and random_fallback_rate as Prometheus range queries collected per run

Reran the same workload. Same code. Same rate. Same duration. 5 reps now.

- prequal p99 = 15.22 ms (was 854)
- round-robin p99 = 14.40 ms (was 284)
- least-connections p99 = 15.30 ms (was 165)

56× reduction on prequal's p99. Zero lines of algorithm code changed.

The failure mode was pool-state leakage between sequential runs, interacting with one algorithm's pool reuse behavior in a way that caught that algorithm specifically. Undetectable from the result numbers alone because the only signal was between-rep variance, not absolute numbers.

Things I'll probably do every time from now on:

- Treat "sequential campaigns" as suspect by default. Interleave or prove you don't need to
- Reset the controller, server, or process under test before every run if there's any per-process state that could leak across reps
- Capture environment variables at run time into run metadata, don't assume they were the defaults
- Instrument the "we degraded to a fallback mode" path explicitly. Prequal's `random_fallback_rate` counter could have saved me days
- Write down hypotheses before touching code. Variance shape tells you whether you're looking at a bug or a noise pattern

Full investigation log (including the 7 hypotheses, which one survived, and the exact evidence that distinguished them): https://github.com/sathwick-p/prequal/blob/main/benchmark/investigations/2026-04-19-c2-tail-spike.md

Controlled campaign runner: https://github.com/sathwick-p/prequal/blob/main/benchmark/scripts/run_interleaved_campaign.sh

Repo: https://github.com/sathwick-p/prequal

---

## Reddit: r/rust (short, optional)

Only post if you want to emphasise the backend. It's a smaller part of the story, but axum + tokio is legit infrastructure.

**Suggested title**

> Small axum/tokio backend for a load-balancing benchmark: implements an NSDI '24 probe endpoint, RIF-bucketed latency reporting, and fault-injection modes

**Body**

Built a small Rust benchmark backend as part of a Kubernetes ingress controller project that implements the NSDI '24 Prequal load-balancing algorithm. The backend's job is to report probe signals (current requests-in-flight, a RIF-conditioned median-latency estimate, and a server-side timestamp) to the controller, plus serve a `/work` endpoint that simulates service time.

It's small (about 280 lines of Rust) but a few things might be interesting:

- axum 0.8 and tokio routing for the probe and work endpoints
- RIF-bucketed latency histogram using `VecDeque<f64>` guarded by per-bucket `tokio::sync::Mutex`es, so concurrent requests don't serialise on a single lock
- Two workload modes: a CPU-bound SHA256 loop (default) and an I/O-bound `tokio::time::sleep` mode (`IO_BOUND_MODE=1`). The toggle turned out to matter a lot for the benchmark. CPU-bound backends cause probe traffic to compete with user traffic for backend CPU, which poisons the probe signal
- Four fault-injection modes via `FAULT_PROBE_MODE`: timeout (sleeps past the controller's probe timeout), 500 (returns HTTP 500), malformed (returns invalid JSON), stale_timestamp (returns an old timestamp so the controller's staleness check fires)

Source: https://github.com/sathwick-p/prequal/blob/main/backend/src/main.rs

Full repo: https://github.com/sathwick-p/prequal

---

## Hacker News submission

**Suggested title (keep it understated, HN rewards that)**

> Show HN: Prequal as a Kubernetes ingress controller, the benchmark almost fooled me

**Body (HN's "show" text, 3-5 sentences max)**

I implemented the Prequal load-balancing algorithm (NSDI '24) as a Kubernetes ingress controller. In the paper-aligned regime it cuts p99 tail latency about 10× vs round-robin and least-connections; in a small-fleet CPU-bound regime it loses by 25%. My first benchmark made the algorithm look like it was losing by 10× until I found the methodology bug (pool-state leakage between sequential runs), and fixing that took one commit, not any algorithm changes. All three results (catastrophic-wrong-first-pass, tied-after-methodology-fix, 10×-win-after-regime-pivot) are in the repo with dashboards and investigation logs.

Repo: https://github.com/sathwick-p/prequal

---

## Angles not yet drafted but might work

- r/devops: benchmarking stack (Prometheus, Grafana, k6, kind, reproducible protocol). Can mostly copy the r/sre post.
- r/kubernetesEngineering: smaller, overlaps with r/kubernetes. Usually stricter about self-promotion rules, check first.
- lobste.rs: similar to HN but more technical. The investigation log is the strongest angle there.
- LinkedIn post: if you want one, keep it tight (4-6 sentences), lead with the "I was wrong by 10× and fixed it by changing my benchmark protocol" hook, link to the repo.
- dev.to or Substack cross-post of `TECHNICAL_BLOG.md`: the blog is already publication-ready, just change the relative links to absolute GitHub URLs.

---

## Practical checklist before posting

- Double-check the repo is set to public on GitHub
- Make sure the hero screenshot link resolves (GitHub raw URL)
- Use straight quotes everywhere. HN and Reddit render curly quotes differently
- Don't post to all four Reddit communities within the same hour, mods read each other
- Reply to questions in the first 2 hours. That's most of the engagement window
- If a question reveals a genuine weakness in the claim, edit the post with an update rather than arguing the weakness away
