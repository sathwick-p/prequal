import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:31080/work';
const hostHeader = __ENV.HOST_HEADER || 'bench.local';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');
const startRate = Number(__ENV.START_RATE || '50');
const stepRate = Number(__ENV.STEP_RATE || '50');
const stepDuration = __ENV.STEP_DURATION || '60s';
const steps = Number(__ENV.STEPS || '6');
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || '64');
const maxVUs = Number(__ENV.MAX_VUS || '512');

// Build stages: short warmup ramp to startRate, then STEPS stages each ramping
// to startRate + stepRate*(i) over stepDuration.
const stages = [{ target: startRate, duration: '10s' }];
for (let i = 0; i < steps; i += 1) {
  stages.push({ target: startRate + stepRate * i, duration: stepDuration });
}

export const options = {
  scenarios: {
    rate_ramp: {
      executor: 'ramping-arrival-rate',
      startRate,
      timeUnit: '1s',
      stages,
      preAllocatedVUs,
      maxVUs,
    },
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)'],
  thresholds: {
    // Very relaxed — ramp intentionally goes past saturation where errors
    // are expected. Gate at 50% to catch only catastrophic breakdowns.
    http_req_failed: ['rate<0.5'],
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
  const path = __ENV.SUMMARY_PATH || './summary-rate_ramp.json';
  return {
    [path]: JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: false }),
  };
}
