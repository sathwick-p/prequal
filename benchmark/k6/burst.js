import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:30080/work';
const hostHeader = __ENV.HOST_HEADER || 'bench.local';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');
const idleRate = Number(__ENV.IDLE_RATE || '10');
const burstRate = Number(__ENV.BURST_RATE || '500');
const idleDuration = __ENV.IDLE_DURATION || '30s';
const burstDuration = __ENV.BURST_DURATION || '15s';
const cycles = Number(__ENV.CYCLES || '5');
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || '64');
const maxVUs = Number(__ENV.MAX_VUS || '512');

// Build alternating idle/burst stages across CYCLES cycles.
const stages = [];
for (let i = 0; i < cycles; i += 1) {
  stages.push({ target: idleRate, duration: idleDuration });
  stages.push({ target: burstRate, duration: burstDuration });
}

export const options = {
  scenarios: {
    burst: {
      executor: 'ramping-arrival-rate',
      startRate: idleRate,
      timeUnit: '1s',
      stages,
      preAllocatedVUs,
      maxVUs,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.1'],
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
  const path = __ENV.SUMMARY_PATH || './summary-burst.json';
  return {
    [path]: JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: false }),
  };
}
