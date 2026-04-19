import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:30080/work';
const hostHeader = __ENV.HOST_HEADER || 'bench.local';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');
const rate = Number(__ENV.RATE || '1000');
const overloadMultiplier = Number(__ENV.OVERLOAD_MULTIPLIER || '2.0');
const duration = __ENV.DURATION || '300s';
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || '256');
const maxVUs = Number(__ENV.MAX_VUS || '2048');

// Effective rate is RATE * OVERLOAD_MULTIPLIER, intentionally above saturation.
const effectiveRate = Math.round(rate * overloadMultiplier);

export const options = {
  scenarios: {
    overload: {
      executor: 'constant-arrival-rate',
      rate: effectiveRate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  thresholds: {
    // No http_req_failed threshold: overload deliberately expects high error rates.
    // Iterations threshold is a no-op sentinel to keep the thresholds block non-empty.
    iterations: ['count>=0'],
  },
};

export default function () {
  const response = http.post(
    targetURL,
    JSON.stringify({ iterations }),
    {
      headers: {
        'Content-Type': 'application/json',
        Host: hostHeader,
      },
    }
  );

  check(response, {
    'status is 200': (r) => r.status === 200,
  });
}

export function handleSummary(data) {
  const path = __ENV.SUMMARY_PATH || './summary-overload.json';
  return {
    [path]: JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: false }),
  };
}
