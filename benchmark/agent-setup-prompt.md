# Prequal: AI Agent Setup Prompt

## Purpose

Use this prompt when assigning benchmark, load-test, observability, or reporting work to another AI agent.

It is designed to keep agents aligned with:

- the current repo state
- the current benchmark assets
- the public-claim evidence standard
- the requirement to produce reproducible outputs

---

## Master Prompt

```text
You are working in the `prequal` repository.

Your job is to extend the benchmark, load-testing, observability, and evidence-collection stack for this project without breaking existing code or duplicating work that is already present.

Before making any changes, read these files in this order:

1. `benchmarking.md`
2. `benchmark/README.md`
3. `benchmark/public-claim-playbook.md`
4. `benchmark/evidence-asset-spec.md`
5. `benchmark/agent-setup-prompt.md`

Then read the implementation files that define the current benchmark-related behavior:

6. `loadbalancer/config.go`
7. `loadbalancer/prober.go`
8. `loadbalancer/pool/pool.go`
9. `loadbalancer/pool/pools.go`
10. `observability/metrics.go`
11. `server/server.go`
12. `controller/controller.go`
13. `main.go`

Then inspect the currently existing benchmark assets:

14. `benchmark/manifests/controller-benchmark.yaml`
15. `benchmark/manifests/workload-uniform.yaml`
16. `benchmark/manifests/workload-heterogeneous.yaml`
17. `benchmark/manifests/workload-multiroute.yaml`
18. `benchmark/k6/steady_state.js`
19. `benchmark/k6/open_loop.js`
20. `benchmark/k6/multi_route.js`
21. `benchmark/scripts/churn.sh`

Your working rules:

- Treat `benchmark/evidence-asset-spec.md` as the source of truth for missing assets.
- Do not remove or weaken existing benchmark coverage.
- Do not change the load-balancing algorithm semantics unless the assigned task explicitly requires it.
- Keep changes reproducible and scriptable.
- Prefer adding assets under `benchmark/` rather than scattering benchmark code elsewhere.
- If you add dashboards, also add provisioning so they auto-load.
- If you add scripts, they must accept environment variables and produce machine-readable outputs where appropriate.
- If you add manifests, keep them benchmark-focused and clearly named.
- If you add new metrics requirements, verify whether the metrics already exist before changing code.
- If you add code instrumentation, keep metric labels low-cardinality.
- If you add result collection or report generation, make the output directory structure deterministic.
- Do not claim publication-grade evidence exists unless the required assets and runs are actually present.
- Commit your work when you finish a coherent unit of work.
- Prefer small, scoped commits over one large mixed commit.
- Use commit messages that clearly describe the asset or capability added.
- If you changed docs and code together for the same feature, they may be committed together.
- If your task is still incomplete or unverified, say that explicitly before committing.

Your first task is:

1. Identify what already exists for your assigned area.
2. Identify what is still missing.
3. Implement the missing assets for your assigned area only.
4. Verify the assets locally as far as possible.
5. Update the relevant markdown docs if behavior or setup instructions changed.

When you finish, report:

- what you created
- what you verified
- what remains unverified
- any follow-up assets another agent should own
- the commit hash or hashes you created, if you committed

Success standard:

- another engineer can run the new asset without guessing hidden assumptions
- outputs are reproducible
- docs match the repo state
- changes support stronger benchmark evidence rather than one-off manual testing
```

---

## How To Split Work Across Agents

Use disjoint ownership. Do not assign overlapping write scopes unless necessary.

Recommended split:

- Agent 1: `benchmark/observability/`
  Owns Prometheus, Grafana, provisioning, dashboard JSONs.

- Agent 2: `benchmark/k6/`
  Owns missing load scripts such as `rate_ramp.js`, `burst.js`, `long_duration.js`, `overload.js`.

- Agent 3: `benchmark/manifests/`
  Owns route-scale, long-duration, and fault-injection manifests.

- Agent 4: `benchmark/scripts/` and `benchmark/report/`
  Owns campaign runner, result collector, archiver, and report rendering helpers.

- Agent 5: code instrumentation only if required
  Owns narrowly scoped changes in `observability/metrics.go`, `controller/`, `server/`, or backend code if a missing metric or fault-injection hook is blocking evidence collection.

---

## What Each Agent Must Evaluate

Before building anything, the agent should answer:

1. What already exists for this area?
2. What does `benchmark/evidence-asset-spec.md` say is still missing?
3. What exact files should be created?
4. What exact outputs should those files produce?
5. How will success be verified locally?
6. Does any doc need to be updated to reflect the new assets?

If the agent cannot answer these six questions from the repo, it has not read enough context yet.

---

## Area-Specific Prompt Add-Ons

Paste the master prompt above, then append one of these.

### Observability Agent

```text
Your assigned area is `benchmark/observability/`.

Create the local Prometheus and Grafana stack described in `benchmark/evidence-asset-spec.md`.

You must create:

- `benchmark/observability/docker-compose.yml`
- `benchmark/observability/prometheus/prometheus.yml`
- `benchmark/observability/prometheus/recording-rules.yml`
- `benchmark/observability/prometheus/alert-rules.yml`
- `benchmark/observability/grafana/provisioning/datasources/prometheus.yml`
- `benchmark/observability/grafana/provisioning/dashboards/dashboards.yml`
- the dashboard JSON files listed in the evidence spec

Requirements:

- scrape the controller metrics endpoint
- provision Grafana automatically
- make dashboards load without manual import
- use the metric names that already exist in `observability/metrics.go`
- document exactly how to start the stack and what URLs to open

Verification:

- config files should be internally consistent
- dashboards should reference the provisioned Prometheus datasource
- docs should explain how the benchmark manifests expose scrape targets
```

### Load Script Agent

```text
Your assigned area is `benchmark/k6/`.

Create the missing load scripts listed in `benchmark/evidence-asset-spec.md`:

- `rate_ramp.js`
- `burst.js`
- `long_duration.js`
- `overload.js`

Requirements:

- follow the style of the existing `k6` scripts
- accept env vars for rate, duration, host header, and URL
- prefer constant-arrival-rate or explicit staged execution where required
- produce summaries suitable for archived evidence
- keep scenario names and env vars self-explanatory

Verification:

- each script should be readable and runnable without editing the file
- `benchmark/README.md` should be updated with example commands if needed
```

### Manifest Agent

```text
Your assigned area is `benchmark/manifests/`.

Create the missing benchmark manifests listed in `benchmark/evidence-asset-spec.md`.

Requirements:

- keep manifests benchmark-specific
- name workloads and services clearly
- ensure ingress hosts and paths line up with the current `k6` scripts
- prefer explicit labels and stable naming
- include comments only where they reduce ambiguity

Verification:

- manifests should be internally coherent
- the workload shape should match the intended scenario
- docs should explain which script pairs with which manifest
```

### Reporting Agent

```text
Your assigned area is `benchmark/scripts/` and `benchmark/report/`.

Create the missing campaign runner, result collector, archive, and report assets.

Requirements:

- produce deterministic output directories under `benchmark/results/`
- save run metadata, command metadata, and summaries
- make it easy to bundle one run or a full campaign
- create report templates that can summarize one run and one campaign

Verification:

- scripts should document usage clearly
- outputs should match the archive shape defined in the evidence spec
```

### Instrumentation Agent

```text
Your assigned area is benchmark-blocking instrumentation only.

Before changing code, confirm the requested metric or hook is actually missing.

Only add narrowly scoped instrumentation needed for:

- benchmark observability
- fault injection
- reproducible evidence collection

Requirements:

- keep labels low-cardinality
- do not add hot-path logging
- update docs if new metrics or env vars are introduced
- add tests where practical

Verification:

- `go test ./...` should pass if code changes were made
```

---

## Minimum Acceptance Checklist

Every agent should leave behind:

- created files in the correct benchmark subdirectory
- updated docs where required
- a short summary of what was verified
- a clear statement of anything still blocked

Reject work that is:

- only partially wired
- not documented
- dependent on hidden manual steps
- inconsistent with the current metric names or benchmark manifests
- impossible to archive or reproduce later
- left uncommitted without a clear reason

---

## What “Improving The Testbed” Means Here

For this repo, improving the testbed means:

- better workload coverage
- better observability
- better fault injection
- better automation
- better result capture
- better report generation
- better reproducibility

It does not mean adding complexity with no evidence value.

If an asset does not make the benchmark campaign easier to trust, easier to reproduce, or easier to analyze, do not add it.
*** End Patch
