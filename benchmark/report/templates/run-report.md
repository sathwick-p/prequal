# Run Report

## Metadata

| Field | Value |
|-------|-------|
| Run ID | ${RUN_ID} |
| Date (UTC) | ${DATE_UTC} |
| Git SHA | ${GIT_SHA} |
| Environment | ${ENVIRONMENT} |

## Scenario

| Field | Value |
|-------|-------|
| Scenario | ${SCENARIO} |
| Algorithm | ${ALGORITHM} |
| Load script | ${LOAD_SCRIPT} |
| Workload manifest | ${WORKLOAD_MANIFEST} |

## Load Profile

| Field | Value |
|-------|-------|
| Target rate (RPS) | ${TARGET_RATE_RPS} |
| Duration (seconds) | ${DURATION_SECONDS} |

## Key Metrics

| Metric | Value |
|--------|-------|
| Total requests | ${TOTAL_REQUESTS} |
| Success rate (%) | ${SUCCESS_RATE} |
| Error rate (%) | ${ERROR_RATE} |
| p50 latency (ms) | ${P50_MS} |
| p95 latency (ms) | ${P95_MS} |
| p99 latency (ms) | ${P99_MS} |

## Anomalies

${NOTES}

## Raw Artifacts

- [k6 summary](k6-summary.json)
- [kubectl top](kubectl-top.txt)
- [Prometheus export](prometheus-export/)
- [Controller logs](controller-logs.txt)
- [Backend logs](backend-logs.txt)
- [Routes snapshot](routes.json)
- [Collection notes](notes.md)
