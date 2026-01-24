
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