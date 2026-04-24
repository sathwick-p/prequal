# Promotion Pack for `prequal`

This file is a working promo kit for the repo and the blog post.

Primary angle:

- this is an open Go implementation of the load-balancing idea Google says it uses across 20+ services, including YouTube's serving stack
- the repo keeps the full benchmark and investigation trail, not just the algorithm
- honest framing: production-proven idea, independent implementation

Do not say:

- "this is YouTube's load balancer"
- "this is Google's production code"
- "I rebuilt YouTube"

Preferred phrasing:

- "the load-balancing idea Google says it uses for YouTube"
- "a Go reimplementation of the Prequal algorithm from the NSDI paper"
- "an open implementation of a Google/YouTube-proven load-balancing approach"

Use these placeholders when posting:

- `REPO_LINK` = GitHub repo URL
- `BLOG_LINK` = blog article URL
- `PAPER_LINK` = https://www.usenix.org/system/files/nsdi24-wydrowski.pdf

## Best Reddit Communities

Checked and selected on April 24, 2026. These are the five best bets for this project based on fit and current subreddit norms.

### 1. `r/golang`

Why it fits:

- the repo is directly Go-related
- the implementation is readable and non-trivial
- the benchmark + systems angle is a good fit for Go developers

Posting note:

- keep the post clearly about the Go implementation, architecture, and code
- avoid leading with "check out my blog"

Suggested title:

`I rebuilt the load-balancing idea Google uses for YouTube in Go`

### 2. `r/kubernetes`

Why it fits:

- this is a Kubernetes ingress controller
- there is an actual controller/reconciliation story here, not a thin wrapper
- the benchmark and route-local balancing state are useful discussion points

Posting note:

- `r/kubernetes` currently has explicit anti-spam / anti-low-effort self-promo rules
- the post needs to provide value independent of the link
- summarize the actual implementation and findings in the post body

Suggested title:

`Built a Kubernetes ingress controller around the load-balancing idea Google uses for YouTube`

### 3. `r/devops` via the weekly self-promotion thread

Why it fits:

- the project includes infra, observability, profiling, benchmark automation, and reproducibility
- the repo is more interesting to DevOps people as a system plus benchmark harness than as an algorithm alone

Posting note:

- safest path is the weekly self-promotion thread, not a standalone post
- keep it short, practical, and benchmark-focused

Suggested thread opener:

`Built an open-source Go ingress controller based on the Google/YouTube Prequal paper + a full benchmark harness`

### 4. `r/programming`

Why it fits:

- broad audience for systems-paper-to-code stories
- the methodology and failed-benchmark angle carries the post by itself
- the post can be framed as "building and validating a load balancer from a production paper"

Posting note:

- focus on the technical journey and lessons, not promotion
- link the article, not just the repo, unless the repo is the stronger artifact

Suggested title:

`I rebuilt the load-balancing idea Google uses for YouTube and benchmarked when it actually wins`

### 5. `r/networking`

Why it fits:

- the core question is a load-balancing question
- the key insight is networking-adjacent: stop balancing CPU, start minimizing queueing/tail latency
- the article has enough detail on signals, probing, and backend selection to be interesting there

Posting note:

- write for people who care about load balancing, not Kubernetes or Go
- lead with the idea: latency + requests-in-flight beat naive load balancing in skewed fleets

Suggested title:

`Why Google's YouTube paper says CPU is the wrong thing to balance`

## Reddit Strategy

Use different framing for each community. Do not paste the same post everywhere.

General rules:

- disclose that it is your project
- put the useful technical summary in the Reddit post itself
- use one link max in the body if possible
- if a subreddit is strict, post the article link and include the repo at the bottom
- if a subreddit is code-first, post the repo link and include the article at the bottom

Recommended order:

1. `r/golang`
2. `r/kubernetes`
3. `r/devops` weekly self-promo thread
4. `r/programming`
5. `r/networking`

## Reddit Posts

These are written to sound human and subreddit-specific.

### `r/golang` post

Title:

`I rebuilt the load-balancing idea Google uses for YouTube in Go`

Body:

I built a Go reimplementation of the Prequal load-balancing algorithm from the NSDI'24 paper "Load is not what you should balance."

What pulled me into it was the paper's claim that Google deploys this approach across 20+ services, including YouTube's serving stack. I wanted to understand what that idea looks like in normal code, not just in a paper or inside Google's internal RPC stack.

So I built it as a Kubernetes ingress controller in Go:

- informer-driven control plane for `Ingress` + `EndpointSlice`
- host/path trie for routing
- route-local probe pools
- async backend probing
- HCL selection using latency + requests-in-flight
- Prometheus metrics and pprof hooks

I also built a benchmark harness around it with `k6`, Grafana, Prometheus, and a Rust backend that exposes `/prequal/probe`.

The interesting part is that the result is not "it always wins":

- on a small CPU-bound 4-backend setup, it loses to round-robin
- on a paper-aligned 16-backend high-skew setup, it cuts p99 by ~6.8x-8.6x

The repo keeps the failed benchmark interpretations too, not just the happy path, which ended up being the most educational part of the project.

Repo: `REPO_LINK`
Writeup: `BLOG_LINK`

### `r/kubernetes` post

Title:

`Built a Kubernetes ingress controller around the load-balancing idea Google uses for YouTube`

Body:

Built a side project that turned into a pretty deep systems exercise: a Go Kubernetes ingress controller based on the Prequal paper from NSDI'24.

The paper is interesting because Google says it uses this load-balancing approach across 20+ services, including YouTube's serving stack. The core idea is to stop balancing CPU and instead choose backends using two signals:

- requests-in-flight
- backend-reported latency

Implementation-wise, I built it as:

- a controller watching `Ingress` and `EndpointSlice`
- an in-process reverse proxy
- per-route probe pools so balancing state stays isolated
- async probing to keep decisions off the request path

I also put a benchmark harness around it so I could compare it against round-robin and least-connections under controlled workloads.

Most useful finding: the methodology mattered almost as much as the algorithm. A bad protocol made the algorithm look dramatically worse until I fixed state leakage between runs.

Final result was regime-specific:

- small CPU-bound fleet: worse than round-robin
- 16-backend skewed fleet: much better tail latency, with ~6.8x-8.6x better p99

If people are interested I can also break out a follow-up post just on the Kubernetes design and route-local state isolation.

Repo: `REPO_LINK`
Writeup: `BLOG_LINK`

### `r/devops` weekly self-promotion thread post

Short version:

Built an open-source Go ingress controller inspired by the Prequal NSDI paper, which Google says it uses across 20+ services including YouTube. The repo includes the controller, a Rust benchmark backend, `k6` load tests, Prometheus/Grafana dashboards, pprof hooks, and a full investigation trail showing where the algorithm wins and where it doesn't.

Most interesting finding:

- small CPU-bound setup: it loses
- paper-aligned skewed setup: it cuts p99 by ~6.8x-8.6x

Project: `REPO_LINK`
Writeup: `BLOG_LINK`

### `r/programming` post

Title:

`I rebuilt the load-balancing idea Google uses for YouTube and benchmarked when it actually wins`

Body:

I took the NSDI'24 Prequal paper and turned it into a working Go project: a Kubernetes ingress controller plus a benchmark harness.

The part that made it worth doing was not just the algorithm. It was the fact that Google says this load-balancing approach is used across 20+ services, including YouTube's serving stack. That made it a lot more interesting than a generic "I wrote a load balancer" project.

The core claim of the paper is that balancing CPU is often the wrong goal. Instead, the algorithm probes backends and chooses based on latency + requests-in-flight.

I implemented that, then benchmarked it against round-robin and least-connections.

The result was more nuanced than I expected:

- on a small CPU-bound setup, it was worse
- on a high-skew 16-backend setup, it cut tail latency dramatically
- a big chunk of the work was fixing the benchmark methodology so I could trust the result at all

I wrote up the architecture, the code, the benchmark protocol, and the failures along the way here:

`BLOG_LINK`

Repo:

`REPO_LINK`

### `r/networking` post

Title:

`Why Google's YouTube load-balancing paper says CPU is the wrong thing to balance`

Body:

I spent the last stretch building a Go implementation of the Prequal algorithm from the NSDI'24 paper that Google says it uses across 20+ services, including YouTube.

The reason I think networking people may find it interesting is that the paper's core argument goes against the default instinct:

don't try to balance CPU evenly

Instead:

- actively probe backends
- track requests-in-flight
- use latency as the tie-breaker among "cold" backends
- only fall back to least-loaded when everything is hot

I implemented it as a Kubernetes ingress controller, but the interesting part is the backend-selection logic and the benchmark results:

- in small CPU-bound setups, the extra complexity doesn't pay for itself
- in skewed fleets, avoiding the wrong backends matters a lot more than spreading load evenly

Writeup: `BLOG_LINK`
Repo: `REPO_LINK`

## Optional Alternate Reddit Posts

Use these if you want a second round later without repeating the same angle.

### Alternate `r/golang`

Title:

`Go project idea: rebuilding a Google/YouTube load-balancing algorithm from a systems paper`

Body:

Wanted a non-trivial Go project, so I rebuilt Prequal from the NSDI paper that Google says powers load balancing in YouTube and other services.

It ended up becoming:

- controller + proxy
- async probing system
- route-local state management
- benchmark harness with `k6`, Prometheus, Grafana, and pprof

The repo is here if anyone wants to read the code or tear apart the benchmark assumptions:

`REPO_LINK`

### Alternate `r/kubernetes`

Title:

`Route-local load-balancing state in a custom Kubernetes ingress controller`

Body:

One design decision I liked in this project was keeping one probe pool per route key so balancing state never bleeds across ingress routes.

That came out of building a Go ingress controller around the Prequal paper Google says it uses for YouTube.

If there's interest, happy to write up just the route matching / endpoint reconciliation / per-route state side of it.

Repo: `REPO_LINK`
Writeup: `BLOG_LINK`

## X / Twitter Posts

These are short and lean on the Google/YouTube hook without saying anything false.

### Project-focused posts

1.

I rebuilt the load-balancing idea Google uses for YouTube in Go.

Kubernetes ingress controller.
Benchmarks.
pprof.
Prometheus.
The failed experiments too.

`REPO_LINK`

2.

Google says YouTube uses this load-balancing idea.

I built an open Go implementation of it and benchmarked when it actually wins.

`REPO_LINK`

3.

Most load balancers try to balance load.

This one tries to avoid queueing.

I rebuilt the Google/YouTube Prequal idea in Go:
`REPO_LINK`

4.

Built a Go ingress controller around the NSDI paper Google says it uses in YouTube.

The fun part:
- loses on small CPU-bound fleets
- wins hard on skewed fleets

`REPO_LINK`

5.

I wanted to understand the load-balancing idea behind YouTube.

So I rebuilt it in Go and turned it into a benchmark-heavy side project.

`REPO_LINK`

### Article-focused posts

1.

I wrote up how I rebuilt the load-balancing idea Google uses for YouTube in Go.

Paper -> controller -> benchmarks -> where it failed -> where it won.

`BLOG_LINK`

2.

The title of the paper is great:
"Load is not what you should balance."

I turned that idea into a Go project and wrote the whole thing up here:

`BLOG_LINK`

3.

I rebuilt a Google/YouTube-inspired load balancer in Go.

The writeup covers:
- how the algorithm works
- how the code is structured
- how I benchmarked it
- why the first result was wrong

`BLOG_LINK`

4.

If you like systems papers that turn into real code, this one was fun:

I rebuilt the load-balancing idea Google says it uses in YouTube and documented the whole process.

`BLOG_LINK`

5.

This started as "let me try a load balancer in Go"

It ended as:
- custom ingress controller
- benchmark harness
- methodology postmortem
- a much narrower claim than I expected

`BLOG_LINK`

### Even shorter variants

1.

I rebuilt the load-balancing idea behind YouTube in Go.
`BLOG_LINK`

2.

Google says YouTube uses this load-balancing approach.
I built an open Go version.
`REPO_LINK`

3.

Built a Go load balancer inspired by Google's YouTube paper.
Writeup here:
`BLOG_LINK`

4.

I turned a Google/YouTube load-balancing paper into a real Go project.
`REPO_LINK`

## Suggested Posting Order

Day 1:

- publish article
- post on `r/golang`
- post project-focused X post #1
- post article-focused X post #1

Day 2:

- post on `r/kubernetes`
- post on `r/devops` self-promo thread
- post X project-focused #4

Day 3:

- post on `r/programming`
- post X article-focused #3

Day 4:

- post on `r/networking`
- post one of the shorter X variants

## Best-performing Angles to Repeat

If one message starts working, repeat these angles in different wording:

- "the load-balancing idea Google uses for YouTube"
- "I rebuilt it in Go"
- "it does not always win"
- "the benchmark methodology changed the conclusion"
- "open implementation of a production-proven idea"

## Final Reminder

For Reddit especially, the best-performing version usually is not the most promotional one.

Lead with:

- what you built
- what you learned
- what surprised you

Then link out.
