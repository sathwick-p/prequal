# Prequal Ingress Controller - Development Plan

## Project Overview

This project implements a custom Kubernetes Ingress Controller with the **Prequal load balancing algorithm** from Google's NSDI'24 paper: "Load is not what you should balance: Introducing Prequal".

**Key Innovation**: Instead of balancing CPU load (which fails under antagonist load), Prequal balances based on:
- **RIF (Requests-In-Flight)**: Leading indicator of load
- **Estimated Latency**: Direct measure of what users experience

---

# COMPLETED WORK

## Phase 1: Control Plane (Kubernetes Watcher) ✅ COMPLETE

### Part 1: The Control Plane (Kubernetes Watcher)

Your goal here is to build a Go program that can "watch" Kubernetes resources and maintain a real-time list of backend IPs in memory.

#### 1. What to Learn / Read
*   **Kubernetes Client-Go:** This is the official library. You need to understand:
    *   **Clientset:** How to connect to the cluster (`rest.InClusterConfig`, `kubernetes.NewForConfig`).
    *   **Informers / SharedInformers:** The event-based mechanism to get updates (Add/Update/Delete) without polling. This is the "magic" of efficient controllers.
    *   **Listers:** How to retrieve objects from the local cache instead of hitting the API server every time.
    *   **Workqueues:** (Advanced but recommended) How to handle events safely without blocking the watcher.

#### 2. How to Go About It (Implementation Steps)
Don't worry about gRPC or proxying yet. Just print IP addresses to the console.

1.  **Project Setup:**
    *   Initialize a Go module.
    *   Import `k8s.io/client-go`.

2.  **Define Your Data Structure:**
    Create a thread-safe store.
    ```go
    type BackendStore struct {
        mu sync.RWMutex
        // ServiceName -> []IPs
        Endpoints map[string][]string
    }
    ```

3.  **Write the Informer:**
    *   Create a `SharedInformerFactory`.
    *   Ask for an informer for `discovery/v1.EndpointSlices` (Avoid `Core/v1.Endpoints` as it's older/slower).
    *   Add Event Handlers (`AddFunc`, `UpdateFunc`, `DeleteFunc`).

4.  **Handle the Logic:**
    *   Inside `UpdateFunc`:
        *   Read the `EndpointSlice` object.
        *   Loop through `slice.Endpoints`.
        *   Check `conditions.Ready == true`.
        *   Extract the IP.
        *   Update your `BackendStore`.
        *   `fmt.Printf("Updated IPs for service %s: %v\n", serviceName, ips)`

5.  **Test It:**
    *   Run this locally using `~/.kube/config`.
    *   Scale a deployment in your cluster (`kubectl scale deploy/my-app --replicas=5`).
    *   Watch your console logs print the new IPs instantly.

---

### Part 2: Preparing for the Data Plane (gRPC)

Before you write the proxy, you need to understand specific gRPC concepts that differ from standard REST.

#### 1. Core Concepts to Learn
*   **Protocol Buffers (Protobuf):** Understand how data is serialized.
*   **Services & Methods:** How gRPC structures APIs (e.g., `package.Service/Method`).
*   **Metadata:** The "Headers" of gRPC. This is where you will find authentication tokens or custom keys (like `user-id`) for your load balancing algo.
*   **Interceptors:** Middleware. You can attach code to run *before* a request is handled. This is often where simple logging or auth happens, though for a *proxy*, we go deeper.

#### 2. The Advanced Stuff (Crucial for Proxying)
*   **`grpc.UnknownServiceHandler`:**
    *   Normally, gRPC servers reject calls to methods they don't know.
    *   You need to learn how to catch *everything* so you can forward it without knowing the schema.
*   **`grpc.Codec` (Custom Codec):**
    *   By default, gRPC tries to decode the Protobuf message.
    *   For a transparent proxy, you *don't* want to decode it (you don't have the `.proto` file!). You want to pass the raw bytes. You need to learn how to use a "Proxy Codec" that just copies bytes.
*   **Context (`context.Context`):**
    *   This carries deadlines (timeouts) and cancellations.
    *   If the client cancels the request, your proxy must cancel the request to the backend.

#### 3. Recommended Reading / Resources
*   **Official gRPC Go Guide:** Start with the "Basics" and "Interceptors" sections.
*   **`mwitkow/grpc-proxy`:** This is an open-source library that implements a transparent gRPC proxy. **Read the source code.** It is the gold standard for this pattern. You will see exactly how they handle the `StreamHandler` and `Director` functions.
*   **"gRPC Internals" Blog Posts:** Look for articles explaining HTTP/2 framing and how gRPC multiplexes requests.

### Summary Plan
1.  **This Week:** Build the **Control Plane** (Watcher). Get it to print IPs when you scale pods.
2.  **Next:** Study `grpc.UnknownServiceHandler` and `StreamDirector`.
3.  **Then:** Combine them -> The Watcher feeds IPs to the Director.



1. Currently skipping the process of deleting routes/paths inside the ingress, the full ingress deletion is handeled only path and route deletion is not.



--- PREQUAL PLAN ---


---

# Integrating Prequal with Your Ingress Controller

## Current Architecture Overview

Your codebase is a **Kubernetes Ingress Controller** with:

| Component | File | Role |
|-----------|------|------|
| **Controller** | `controller/controller.go` | Watches K8s Ingresses & EndpointSlices, maintains backend IP list |
| **Router** | `controller/router.go` | Path matching via radix tree, stores algorithm config per route |
| **ProxyServer** | `server/server.go` | HTTP reverse proxy, forwards requests to backends |
| **BackendIPStore** | `controller/controller.go` | Thread-safe map of `service -> []IP` |

**Current load balancing**: Essentially none - line 58 of `server/server.go` just picks `backends[0]`:

```go
backend := backends[0]
```

You already have the infrastructure for algorithm selection (`PathConfig.Algorithm` and the `lb/algo` annotation), but it's not wired up yet.

---

## Integration Strategy

Prequal fits naturally into your architecture as a **new load balancing strategy** that the `ProxyServer` can use when `Algorithm == "prequal"`.

### High-Level Component Mapping

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    YOUR EXISTING ARCHITECTURE                           │
│                                                                         │
│  ┌─────────────┐      ┌──────────────┐      ┌─────────────────────┐   │
│  │ Controller  │─────▶│ Router       │─────▶│ ProxyServer         │   │
│  │ (K8s watch) │      │ (path match) │      │ (reverse proxy)     │   │
│  └─────────────┘      └──────────────┘      └─────────────────────┘   │
│         │                                            │                 │
│         ▼                                            ▼                 │
│  ┌─────────────┐                            ┌─────────────────────┐   │
│  │BackendIPStore│                            │ CURRENT: backends[0]│   │
│  │ svc -> []IP │                            │ NEW: Prequal select │   │
│  └─────────────┘                            └─────────────────────┘   │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                    PREQUAL COMPONENTS TO ADD                            │
│                                                                         │
│  CLIENT SIDE (in your proxy):                                          │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────┐ │
│  │ ProbePool        │  │ ProbeManager     │  │ HCLSelector          │ │
│  │ (per service)    │  │ (async probing)  │  │ (replica selection)  │ │
│  └──────────────────┘  └──────────────────┘  └──────────────────────┘ │
│                                                                         │
│  SERVER SIDE (new lightweight endpoint on backends):                   │
│  ┌──────────────────┐  ┌──────────────────┐                           │
│  │ RIF Counter      │  │ Latency Estimator│                           │
│  └──────────────────┘  └──────────────────┘                           │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## New Data Structures Needed

### 1. Probe Response

This represents what a backend returns when probed:

```
ProbeResponse:
  - BackendIP: string        // Which backend responded
  - RIF: int                 // Current requests-in-flight
  - EstimatedLatency: time.Duration
  - ReceivedAt: time.Time    // When we got this response
  - UseCount: int            // How many times we've used this probe
```

### 2. Probe Pool (per service/backend-group)

Each service (e.g., `default/backend-svc`) needs its own probe pool:

```
ProbePool:
  - entries: []ProbeResponse  // Max 16 entries
  - maxSize: 16
  - timeout: 1s
  - rifDistribution: RollingQuantile  // To compute hot/cold threshold
  - mu: sync.RWMutex
```

### 3. Prequal Load Balancer

Wraps the pool and selection logic:

```
PrequalLB:
  - pools: map[string]*ProbePool  // service key -> pool
  - config: PrequalConfig
      - ProbesPerQuery: float64    // r_probe, default 2-3
      - PoolSize: int              // default 16
      - ProbeTimeout: time.Duration
      - QRif: float64              // hot/cold threshold quantile (0.6-0.9)
      - RemoveRate: float64        // r_remove
      - ReuseLimit: int            // b_reuse
```

---

## Where Each Component Lives

### 1. New Package: `balancer/prequal/`

Create a new package to encapsulate Prequal logic:

```
balancer/
  prequal/
    pool.go         // ProbePool implementation
    selector.go     // HCL selection logic
    prober.go       // Async probe sender
    latency.go      // RIF distribution tracker
    config.go       // Configuration struct
    prequal.go      // Main PrequalLB type
```

### 2. Modifications to Existing Files

**`server/server.go`** - Add balancer dispatch:

Currently at line 57-58 you have:
```go
// 4. Select backend (simple: first one for now)
backend := backends[0]
```

This becomes a dispatch based on `pathConfig.Algorithm`:

```
switch pathConfig.Algorithm:
  case "prequal":
    backend = prequal.Select(pathConfig.Key, backends)
  case "round-robin":
    backend = roundrobin.Select(...)
  default:
    backend = backends[0]
```

**`controller/controller.go`** - Initialize Prequal pools:

When `syncServiceEndpoints` updates the backend IPs, it should also notify the Prequal subsystem to update its pool for that service (add new backends, remove stale ones).

---

## The Probing Mechanism

### Challenge: Your Backends Don't Speak "Probe"

Prequal requires backends to respond to probe requests with RIF and latency data. Your current architecture just has a list of IPs - the backends are opaque.

### Two Approaches

#### Approach A: Sidecar/Agent Pattern (Recommended)

Deploy a lightweight **Prequal agent** as a sidecar in each backend pod:

```
Backend Pod:
  ┌─────────────────────────────────────────┐
  │ Container 1: Your App (port 8080)       │
  │ Container 2: Prequal Agent (port 9999)  │──▶ Exposes /probe endpoint
  │              - Tracks RIF               │
  │              - Estimates latency        │
  └─────────────────────────────────────────┘
```

The agent:
- Intercepts requests (or uses eBPF/kernel stats) to track RIF
- Maintains latency statistics bucketed by RIF
- Exposes a `/probe` or gRPC endpoint that returns `{rif: 5, latency_ms: 23}`

Your ingress controller probes `backend-ip:9999/probe` instead of the main app port.

#### Approach B: Application-Integrated

Require backend applications to expose a probe endpoint themselves. This is simpler but requires app changes:

```
GET /prequal/probe
Response: {"rif": 5, "estimated_latency_ms": 23}
```

#### Approach C: Passive Observation (Limited)

Track RIF and latency from the **proxy side only**:
- RIF: Count of in-flight requests per backend (you track this in the proxy)
- Latency: Observe actual response times

This is less accurate than server-side tracking but requires no backend changes. It's what NGINX and some other load balancers do.

---

## Integration Points in Detail

### Point 1: Probe Sending (Async Background Goroutines)

When to probe:
- Triggered by incoming requests (send `r_probe` probes per query)
- Minimum probe rate when idle (to keep pools fresh)

Where this happens:
- New goroutine pool managed by `PrequalLB`
- Started when the `ProxyServer` initializes

Flow:
```
Request arrives
    │
    ├──▶ [Async] Send r_probe probes to random backends
    │           └──▶ HTTP GET backend:9999/probe
    │           └──▶ Parse response
    │           └──▶ Add to ProbePool
    │
    └──▶ [Sync] Select backend from existing pool
              └──▶ Return selected backend IP
```

### Point 2: Probe Pool Management

The pool needs lifecycle management:

**On probe response received:**
1. If pool is full, evict oldest probe
2. Add new probe to pool
3. Update RIF distribution estimate

**On each request:**
1. If pool size < 2, fall back to random selection
2. Select using HCL rule
3. Mark probe as used (increment `UseCount`)
4. If `UseCount >= ReuseLimit`, remove probe
5. Periodically remove worst probe (rate: `r_remove`)

**Background cleanup:**
- Evict probes older than timeout (1 second)

### Point 3: The HCL Selection (in `selector.go`)

```
func (p *ProbePool) SelectBackend() string:
    1. Filter out expired probes
    2. If pool empty, return random backend
    3. Compute hot/cold threshold from RIF distribution
    4. Classify each probe as hot or cold
    5. If all probes are hot:
         return probe with lowest RIF
       Else:
         among cold probes, return one with lowest latency
    6. Increment selected probe's UseCount
    7. Increment selected probe's RIF (we're adding load)
```

### Point 4: Backend IP Updates

When the Controller detects endpoint changes:

```
syncServiceEndpoints() called
    │
    ├──▶ Update BackendIPStore (existing)
    │
    └──▶ Notify PrequalLB of backend change
              └──▶ Add new backends to pool candidates
              └──▶ Remove probes for deleted backends
```

### Point 5: Server-Side (If Using Sidecar)

The Prequal agent/sidecar needs:

**RIF Tracking:**
```
Atomic counter
  - Increment on request start
  - Decrement on request end
  - Probe reads current value
```

**Latency Estimation:**
```
Data structure: map[int]RingBuffer  // RIF bucket -> recent latencies

On request complete:
  - Record (arrival_rif, latency) pair
  - Store in ring buffer for that RIF bucket

On probe:
  - Current RIF = 5
  - Look up RIF bucket 5 (or nearby)
  - Return median of recent latencies
```

---

## Configuration via Annotations

Extend your annotation support to configure Prequal:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
  labels:
    ingress.class: prequal
  annotations:
    lb/algo: "prequal"
    prequal/probes-per-query: "3"
    prequal/pool-size: "16"
    prequal/q-rif: "0.84"
    prequal/probe-timeout: "1s"
spec:
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            backend:
              service:
                name: backend-svc
                port:
                  number: 8080
```

Parse these in `syncIngress()` and pass to the Prequal config.

---

## Request Flow After Integration

```
1. HTTP Request arrives at ProxyServer.ServeHTTP()
         │
2. Router.Match(host, path) → PathConfig
         │
3. Check PathConfig.Algorithm == "prequal"
         │
4. Get backends from BackendIPStore
         │
5. [ASYNC] PrequalLB.TriggerProbes(serviceKey, backends)
         │         └──▶ Sends probes to random backends
         │         └──▶ Updates probe pool when responses arrive
         │
6. [SYNC] PrequalLB.Select(serviceKey) → backend IP
         │         └──▶ Applies HCL rule to probe pool
         │         └──▶ Manages probe lifecycle
         │
7. Forward request to selected backend
         │
8. Track request completion (for passive latency observation)
```

---

## Key Implementation Decisions to Make

1. **Probe endpoint**: Sidecar agent vs. app-integrated vs. passive observation?

2. **Probe transport**: HTTP GET (simple) or gRPC (lower overhead)?

3. **Pool scope**: Per-service pool (simpler) or per-service-per-path (finer-grained)?

4. **Fallback behavior**: What happens when Prequal fails or pool is empty? (Random selection is typical)

5. **Metrics/Observability**: Expose probe pool stats, selection decisions, latency improvements via your debug server at `:8081`

---

## Suggested Implementation Order

1. **Start with passive observation**: Track RIF and latency from the proxy side only. This lets you test the pool and HCL logic without backend changes.

2. **Add the probe pool data structure**: Implement the pool with add/remove/select operations.

3. **Implement HCL selection**: Wire it into `ServeHTTP` for routes with `algo: prequal`.

4. **Add async probing**: Background goroutines that probe backends and feed the pool.

5. **Build the sidecar agent**: For accurate server-side RIF and latency tracking.

6. **Tune parameters**: Test with your workloads to find optimal `Q_RIF`, `r_probe`, etc.

---

Would you like me to dive deeper into any specific component, or discuss tradeoffs between the different approaches?


Great question! Let me clarify exactly how the probing mechanism works - it's often a point of confusion.

## Probes Are **Separate** From Actual Requests

Probes are **not** sent with every request to the backend application. They are **independent, lightweight status checks** sent to the sidecar on a separate port.

```
                                    ACTUAL REQUEST PATH
┌──────────────┐                    ┌─────────────────────────────────────┐
│ Your Ingress │───────────────────▶│ Backend Pod                         │
│ Controller   │   HTTP Request     │  ┌─────────────────────────────┐   │
│              │   to port 8080     │  │ Main App Container          │   │
│              │                    │  │ (port 8080)                 │   │
│              │                    │  │ - Processes actual requests │   │
│              │                    │  └─────────────────────────────┘   │
│              │                    │                                     │
│              │   PROBE PATH       │  ┌─────────────────────────────┐   │
│              │───────────────────▶│  │ Prequal Sidecar             │   │
│              │   GET /probe       │  │ (port 9999)                 │   │
│              │   to port 9999     │  │ - Returns {rif, latency}    │   │
│              │   (async,          │  │ - Never touches app traffic │   │
│              │    separate)       │  └─────────────────────────────┘   │
└──────────────┘                    └─────────────────────────────────────┘
```

## How The Flow Works

### Step 1: Probes Happen Asynchronously (Background)

```
Time ─────────────────────────────────────────────────────────────▶

Request 1 arrives
    │
    ├──▶ [ASYNC] Send ~3 probes to random backends (port 9999)
    │         These return immediately with {rif: 5, latency: 20ms}
    │         Results go into the probe pool
    │
    └──▶ [SYNC] Select backend from EXISTING pool entries
               (uses probes from previous requests)
               Forward request to selected backend (port 8080)

Request 2 arrives
    │
    ├──▶ [ASYNC] Send ~3 more probes to different random backends
    │         Pool gets fresher data
    │
    └──▶ [SYNC] Select from pool (now has probes from Request 1)
               Forward request
```

**Key insight**: The probes triggered by Request 1 aren't used to route Request 1. They're used for *future* requests. This keeps probing off the critical path.

### Step 2: Probe Frequency

Probes are sent at a rate proportional to your query rate:

| Config | Meaning |
|--------|---------|
| `r_probe = 3` | Send 3 probes per incoming request |
| `r_probe = 1` | Send 1 probe per incoming request |
| `r_probe = 0.5` | Send 1 probe every 2 requests |

You also have a **minimum probe rate** for idle periods so the pool doesn't go stale.

### Step 3: What The Sidecar Does

The sidecar has two jobs:

**Job 1: Track RIF and Latency**

The sidecar needs to observe traffic to the main app. Options:

```
Option A: Proxy Mode (sidecar intercepts all traffic)
┌─────────────────────────────────────────────────────────────┐
│ Pod                                                         │
│                                                             │
│   Ingress ──▶ Sidecar:8080 ──▶ App:8081                    │
│               (counts RIF,     (actual app)                 │
│                measures latency)                            │
└─────────────────────────────────────────────────────────────┘

Option B: Observer Mode (sidecar reads kernel/eBPF stats)
┌─────────────────────────────────────────────────────────────┐
│ Pod                                                         │
│                                                             │
│   Ingress ──────────────────▶ App:8080                     │
│                                  ▲                          │
│   Sidecar:9999 ◀── observes ────┘                          │
│   (reads connection stats from kernel)                      │
└─────────────────────────────────────────────────────────────┘

Option C: App Reports to Sidecar (app sends metrics)
┌─────────────────────────────────────────────────────────────┐
│ Pod                                                         │
│                                                             │
│   Ingress ──▶ App:8080 ──reports──▶ Sidecar:9999           │
│               (calls sidecar on                             │
│                request start/end)                           │
└─────────────────────────────────────────────────────────────┘
```

**Job 2: Respond to Probes**

Simple HTTP endpoint:

```
GET http://backend-ip:9999/probe

Response (JSON, ~100 bytes):
{
  "rif": 5,
  "estimated_latency_ms": 23
}
```

This is **extremely lightweight** - just reading two values and returning JSON. Sub-millisecond response time.

---

## Concrete Example Timeline

Let's say you have 3 backend pods and `r_probe = 2`:

```
Time 0ms:   Request A arrives at ingress
            - Pool is empty (first request)
            - Fallback: pick random backend → Pod 1
            - [ASYNC] Send probes to Pod 2, Pod 3

Time 1ms:   Probe responses arrive
            - Pod 2: {rif: 3, latency: 15ms}
            - Pod 3: {rif: 1, latency: 12ms}
            - Pool now has 2 entries

Time 50ms:  Request B arrives
            - Pool has 2 entries (from Request A's probes)
            - HCL selection: Pod 3 is cold (rif=1), lowest latency → Pod 3
            - [ASYNC] Send probes to Pod 1, Pod 2

Time 51ms:  Probe responses arrive
            - Pod 1: {rif: 2, latency: 18ms}
            - Pod 2: {rif: 4, latency: 20ms}
            - Pool now has 4 entries

Time 100ms: Request C arrives
            - Pool has 4 entries
            - HCL selection: Pod 3 has lowest latency among cold → Pod 3
            - Pod 3's probe entry: rif incremented to 2 (we added load)
            - [ASYNC] Send probes to Pod 1, Pod 3
            
... and so on
```

---

## Sidecar Design: What It Needs To Track

### Data Structures in the Sidecar

```
Sidecar State:
  - currentRIF: atomic int64          // Increment on request start, decrement on end
  - latencyBuckets: map[int]RingBuffer // RIF bucket → recent latencies
```

### Tracking RIF

If using **Proxy Mode** (sidecar intercepts traffic):

```
On request received at sidecar:
    atomic.AddInt64(&currentRIF, 1)
    startTime = now()
    arrivalRIF = currentRIF
    
    forward request to app
    wait for response
    
    latency = now() - startTime
    atomic.AddInt64(&currentRIF, -1)
    
    // Store for latency estimation
    latencyBuckets[arrivalRIF].Add(latency)
```

### Estimating Latency

When a probe arrives:

```
GET /probe handler:
    rif = atomic.LoadInt64(&currentRIF)
    
    // Get recent latencies at similar RIF levels
    bucket = latencyBuckets[rif]
    if bucket.Empty():
        // Try nearby buckets
        bucket = findNearestBucket(rif)
    
    estimatedLatency = bucket.Median()
    
    return JSON{rif, estimatedLatency}
```

The latency is bucketed by RIF because **latency depends on how loaded the server was**. A server with 10 requests in flight will have higher latency than one with 1 request.

---

## Why Probes Are Cheap

| Aspect | Why It's Cheap |
|--------|----------------|
| **Frequency** | Only ~1-5 probes per actual request, not per-request overhead |
| **Size** | ~100 bytes request, ~100 bytes response |
| **Latency** | Sub-millisecond (just reading memory, no I/O) |
| **CPU** | Negligible - one atomic read + JSON marshal |
| **Connection** | Can use HTTP/1.1 keep-alive or HTTP/2 |

The paper measured probe overhead in production YouTube and found it was in the noise compared to the actual request processing.

---

## Sidecar Deployment Pattern

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-backend
spec:
  template:
    spec:
      containers:
      # Your actual application
      - name: app
        image: my-app:latest
        ports:
        - containerPort: 8080
        
      # Prequal sidecar
      - name: prequal-sidecar
        image: prequal-sidecar:latest
        ports:
        - containerPort: 9999   # Probe endpoint
        - containerPort: 8081   # Proxy port (if using proxy mode)
        env:
        - name: UPSTREAM_PORT
          value: "8080"         # Forward to app
        - name: PROXY_PORT
          value: "8081"         # Listen for traffic
        - name: PROBE_PORT
          value: "9999"         # Listen for probes
```

If using **Proxy Mode**, the Service would point to the sidecar's proxy port (8081), which forwards to the app (8080):

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-backend-svc
spec:
  ports:
  - name: http
    port: 80
    targetPort: 8081   # Sidecar proxy port, NOT app directly
  - name: probe
    port: 9999
    targetPort: 9999   # Probe endpoint
```

Your ingress controller would:
- Send actual requests to `backend-ip:80` (which hits sidecar:8081 → app:8080)
- Send probes to `backend-ip:9999`

---

## Summary

| Question | Answer |
|----------|--------|
| Does every request trigger a probe? | No - probes are decoupled from requests |
| How often are probes sent? | `r_probe` probes per incoming request (configurable, typically 1-5) |
| Do probes block requests? | No - probes are async, requests use existing pool |
| What port do probes go to? | Separate port (e.g., 9999), not the app port |
| What does the sidecar do? | 1) Track RIF/latency, 2) Respond to `/probe` |
| Is this expensive? | No - probes are ~100 bytes, sub-millisecond |

Would you like me to go deeper into the sidecar implementation details, or discuss how the sidecar would track RIF/latency in different modes?

---

# IMPLEMENTATION PROGRESS

## Phase 2: Prequal Sidecar (Observer Mode) ✅ COMPLETE

### What Was Built

A lightweight sidecar container that observes TCP connections via `/proc/net/tcp` and exposes RIF data through a probe endpoint.

### Files Created

```
probe/
├── probe.go       # Main sidecar implementation
└── Dockerfile     # Container build file
```

### Implementation Details

**probe/probe.go** - Core sidecar logic:
```go
// Key structures
type ProbeResponse struct {
    RIF       int    `json:"rif"`
    BackendIP string `json:"backend_ip"`
    Timestamp int64  `json:"timestamp_ms"`
}

type Observer struct {
    targetPort uint64
    procFS     procfs.FS
    mu         sync.RWMutex
    currentRIF int
    localIP    string
}
```

**Features implemented:**
1. **RIF Tracking via /proc/net/tcp**
   - Reads `/proc/net/tcp` and `/proc/net/tcp6` every 100ms
   - Counts ESTABLISHED connections on target port
   - Uses `github.com/prometheus/procfs` for parsing

2. **HTTP Probe Endpoint**
   - `GET /probe` → Returns `{rif, backend_ip, timestamp_ms}`
   - `GET /health` → Health check endpoint

3. **Configuration via Environment Variables**
   - `TARGET_PORT`: Port to observe (default: 80)
   - `PROBE_PORT`: Port to serve probe endpoint (default: 9999)

### Deployment Configuration

**test.yaml** - Added sidecar to api-deployment:
```yaml
containers:
- name: echo                    # Main app
  image: ealen/echo-server:latest
  ports:
  - containerPort: 80

- name: prequal-sidecar         # Sidecar
  image: prequal-sidecar:latest
  ports:
  - containerPort: 9999
  env:
  - name: TARGET_PORT
    value: "80"
  - name: PROBE_PORT
    value: "9999"
  resources:
    requests:
      cpu: 10m
      memory: 16Mi
    limits:
      cpu: 50m
      memory: 32Mi
```

**Service updated** to expose both ports:
```yaml
ports:
- name: http
  port: 80
  targetPort: 80
- name: probe
  port: 9999
  targetPort: 9999
```

### Makefile Targets Added

```makefile
build-sidecar          # Build sidecar binary locally
docker-build-sidecar   # Build sidecar Docker image
kind-load-sidecar      # Load sidecar into kind cluster
kind-load-all          # Build and load all images
test-probe             # Test probe endpoint on pods
port-forward-probe     # Port forward to probe endpoint
```

### Testing Results

**Verified working:**
- Sidecar starts and reads /proc/net/tcp correctly
- RIF reflects active connections under load
- Tested with `hey` load generator:
  - `hey -n 10000 -c 50 http://localhost:8080/` → RIF shows ~50
  - Variable concurrency (`-c 10`, `-c 100`) → RIF changes accordingly

**Test commands:**
```bash
# Port forward both app and probe
kubectl port-forward svc/api-service 8080:80 9999:9999

# Watch RIF in real-time
watch -n 0.2 'curl -s http://localhost:9999/probe'

# Generate variable load
for c in 10 30 70 20 90 15 50; do
  hey -c $c -z 3s http://localhost:8080/ > /dev/null 2>&1
done
```

---

# FUTURE WORK

## Phase 3: Add Latency Tracking to Sidecar

### Option A: TCP RTT (Simple)

Add TCP RTT measurement using `ss -ti` or netlink sockets:

```go
type ProbeResponse struct {
    RIF             int     `json:"rif"`
    BackendIP       string  `json:"backend_ip"`
    EstimatedLatency float64 `json:"estimated_latency_ms"`  // NEW
    LatencySource   string  `json:"latency_source"`         // "tcp_rtt" | "app_metrics"
    Timestamp       int64   `json:"timestamp_ms"`
}
```

**Implementation approach:**
1. Run `ss -ti 'sport = :80'` to get TCP_INFO
2. Parse RTT from output: `rtt:0.045/0.022`
3. Average across active connections

### Option B: eBPF (Advanced, More Accurate)

Replace /proc/net polling with eBPF for event-driven tracking:

**Benefits:**
- Zero polling overhead
- Precise connection duration (actual request latency)
- Captures every connection (no sampling gaps)

**Architecture:**
```
┌─────────────────────────────────────────────────────────────────┐
│                     LINUX KERNEL                                │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ eBPF Programs                                              │ │
│  │  - tracepoint/sock/inet_sock_set_state                    │ │
│  │  - Tracks ESTABLISHED → increment RIF                      │ │
│  │  - Tracks CLOSE → decrement RIF, compute latency          │ │
│  └───────────────────────────────────────────────────────────┘ │
│                          │                                      │
│                    eBPF Maps                                    │
│           ┌──────────────┼──────────────┐                      │
│           ▼              ▼              ▼                      │
│    [rif_counter]  [conn_start_times]  [latency_ringbuf]       │
└───────────────────────────────────────────────────────────────┘
                           ▲
                           │ bpf() syscall
                           │
┌──────────────────────────┴────────────────────────────────────┐
│                  USERSPACE (Go + cilium/ebpf)                 │
│  - Loads eBPF programs                                        │
│  - Reads maps for /probe endpoint                             │
│  - Computes latency histogram bucketed by RIF                 │
└───────────────────────────────────────────────────────────────┘
```

**Required changes:**
1. Add `bpf/` directory with eBPF C code
2. Use `cilium/ebpf` library with `bpf2go`
3. Update Dockerfile for privileged container
4. Add security context to deployment

---

## Phase 4: Implement Probe Pool in Ingress Controller

### New Package: `loadbalancer/prequal/`

```
loadbalancer/prequal/
├── pool.go        # ProbePool - stores probe responses
├── selector.go    # HCL selection algorithm
├── prober.go      # Async background probing
├── config.go      # Configuration struct
└── prequal.go     # Main PrequalLB type
```

### Data Structures

```go
// ProbeEntry represents a single probe response in the pool
type ProbeEntry struct {
    BackendIP   string
    RIF         int
    Latency     time.Duration
    ReceivedAt  time.Time
    UseCount    int
}

// ProbePool maintains probe responses for a service
type ProbePool struct {
    mu          sync.RWMutex
    entries     []*ProbeEntry
    maxSize     int           // Default: 16
    timeout     time.Duration // Default: 1s
    rifQuantile *RollingQuantile
}

// PrequalLB is the main load balancer
type PrequalLB struct {
    pools       map[string]*ProbePool  // serviceKey -> pool
    config      Config
    httpClient  *http.Client
    probeWG     sync.WaitGroup
}

// Config holds Prequal configuration
type Config struct {
    ProbesPerQuery float64       // r_probe, default: 2
    PoolSize       int           // default: 16
    ProbeTimeout   time.Duration // default: 1s
    QRif           float64       // hot/cold threshold, default: 0.84
    RemoveRate     float64       // r_remove, default: 0.5
    ReuseLimit     int           // b_reuse, computed from formula
    ProbePort      int           // default: 9999
}
```

### HCL Selection Algorithm

```go
func (p *ProbePool) Select() *ProbeEntry {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    // 1. Filter expired probes
    p.evictExpired()
    
    // 2. Fallback if pool is too small
    if len(p.entries) < 2 {
        return nil  // Caller should use random selection
    }
    
    // 3. Compute hot/cold threshold
    threshold := p.rifQuantile.Quantile(p.config.QRif)
    
    // 4. Classify probes
    var hot, cold []*ProbeEntry
    for _, e := range p.entries {
        if e.RIF > threshold {
            hot = append(hot, e)
        } else {
            cold = append(cold, e)
        }
    }
    
    // 5. HCL selection
    var selected *ProbeEntry
    if len(cold) == 0 {
        // All hot: pick lowest RIF
        selected = minByRIF(hot)
    } else {
        // Has cold: pick lowest latency among cold
        selected = minByLatency(cold)
    }
    
    // 6. Update selected probe
    selected.UseCount++
    selected.RIF++  // We're adding load
    
    // 7. Check reuse limit
    if selected.UseCount >= p.config.ReuseLimit {
        p.remove(selected)
    }
    
    return selected
}
```

### Integration with ProxyServer

**server/server.go** changes:

```go
type ProxyServer struct {
    router    *controller.Router
    ips       *controller.BackendIPStore
    Transport *http.Transport
    prequal   *prequal.PrequalLB  // NEW
}

func (p *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // ... existing route matching ...
    
    backends := p.ips.Get(pathConfig.Key)
    
    // Select backend based on algorithm
    var backend string
    switch pathConfig.Algorithm {
    case "prequal":
        backend = p.prequal.Select(pathConfig.Key, backends)
        // Trigger async probes
        go p.prequal.TriggerProbes(pathConfig.Key, backends)
    case "round-robin":
        backend = p.roundRobin(pathConfig.Key, backends)
    default:
        backend = backends[rand.Intn(len(backends))]
    }
    
    // ... forward request ...
}
```

---

## Phase 5: Async Probing System

### Prober Implementation

```go
// Prober handles async probing of backends
type Prober struct {
    pool       *ProbePool
    backends   []string
    probePort  int
    httpClient *http.Client
    rate       float64  // probes per request
    
    probeChan  chan string  // backends to probe
    stopChan   chan struct{}
}

func (p *Prober) Start() {
    go p.probeLoop()
}

func (p *Prober) probeLoop() {
    for {
        select {
        case backend := <-p.probeChan:
            p.probeBackend(backend)
        case <-p.stopChan:
            return
        }
    }
}

func (p *Prober) probeBackend(backend string) {
    url := fmt.Sprintf("http://%s:%d/probe", backend, p.probePort)
    
    resp, err := p.httpClient.Get(url)
    if err != nil {
        return  // Probe failed, skip
    }
    defer resp.Body.Close()
    
    var probeResp ProbeResponse
    json.NewDecoder(resp.Body).Decode(&probeResp)
    
    entry := &ProbeEntry{
        BackendIP:  backend,
        RIF:        probeResp.RIF,
        Latency:    time.Duration(probeResp.EstimatedLatency) * time.Millisecond,
        ReceivedAt: time.Now(),
        UseCount:   0,
    }
    
    p.pool.Add(entry)
}

func (p *Prober) TriggerProbes(backends []string) {
    // Select random backends to probe
    n := int(p.rate)
    if rand.Float64() < (p.rate - float64(n)) {
        n++  // Probabilistic rounding
    }
    
    // Shuffle and pick n backends
    perm := rand.Perm(len(backends))
    for i := 0; i < n && i < len(backends); i++ {
        select {
        case p.probeChan <- backends[perm[i]]:
        default:
            // Channel full, skip
        }
    }
}
```

---

## Phase 6: Configuration via Ingress Annotations

### Annotation Parsing

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
  labels:
    ingress.class: prequal
  annotations:
    lb/algo: "prequal"
    prequal/probes-per-query: "3"
    prequal/pool-size: "16"
    prequal/q-rif: "0.84"
    prequal/probe-timeout: "1s"
    prequal/probe-port: "9999"
```

### Controller Changes

```go
func (c *Controller) syncIngress(ingress *networkingv1.Ingress) {
    // ... existing logic ...
    
    // Parse Prequal config from annotations
    if algo == "prequal" {
        config := prequal.ParseConfig(ingress.Annotations)
        c.prequal.UpdateConfig(serviceKey, config)
    }
}
```

---

## Phase 7: Observability & Metrics

### Debug Endpoint Enhancements

Extend `/routes` endpoint to include Prequal stats:

```json
{
  "host": "test.example.com",
  "paths": [{
    "path": "/api",
    "algorithm": "prequal",
    "prequal_stats": {
      "pool_size": 12,
      "hot_count": 3,
      "cold_count": 9,
      "avg_rif": 15.2,
      "avg_latency_ms": 23.5,
      "probes_sent": 1523,
      "probes_failed": 12
    }
  }]
}
```

### Prometheus Metrics

```go
var (
    probePoolSize = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "prequal_probe_pool_size",
            Help: "Current size of probe pool",
        },
        []string{"service"},
    )
    
    probeLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "prequal_probe_latency_seconds",
            Help:    "Probe response latency",
            Buckets: []float64{.001, .005, .01, .025, .05, .1},
        },
        []string{"service", "backend"},
    )
    
    selectionDecisions = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "prequal_selection_total",
            Help: "Number of backend selection decisions",
        },
        []string{"service", "result"},  // result: "hot", "cold", "fallback"
    )
)
```

---

## Phase 8: Production Hardening

### Error Handling & Fallbacks

1. **Probe failures**: Skip failed probes, don't add to pool
2. **Empty pool**: Fall back to random selection
3. **All backends unhealthy**: Circuit breaker pattern
4. **Sidecar not deployed**: Detect missing probe port, fall back

### Performance Optimizations

1. **Connection pooling**: Reuse HTTP connections for probes
2. **Probe batching**: Send multiple probes in parallel
3. **Pool sharding**: Reduce lock contention for high-traffic services

### Testing

1. **Unit tests**: Pool operations, HCL selection
2. **Integration tests**: End-to-end with sidecar
3. **Load tests**: Compare Prequal vs round-robin under load
4. **Chaos tests**: Sidecar failures, network partitions

---

# IMPLEMENTATION TIMELINE

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Control Plane (K8s Watcher) | ✅ Complete |
| 2 | Sidecar (Observer Mode, RIF only) | ✅ Complete |
| 3 | Add Latency Tracking | 🔲 Pending |
| 4 | Probe Pool in Controller | 🔲 Pending |
| 5 | Async Probing System | 🔲 Pending |
| 6 | Ingress Annotation Config | 🔲 Pending |
| 7 | Observability & Metrics | 🔲 Pending |
| 8 | Production Hardening | 🔲 Pending |

---

# REFERENCES

- **Paper**: "Load is not what you should balance: Introducing Prequal" (NSDI'24)
  - https://www.usenix.org/conference/nsdi24/presentation/wydrowski
- **procfs library**: github.com/prometheus/procfs
- **eBPF library**: github.com/cilium/ebpf
- **Load testing**: github.com/rakyll/hey