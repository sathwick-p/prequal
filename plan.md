
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