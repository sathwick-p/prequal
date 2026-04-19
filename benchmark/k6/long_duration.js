import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:31080/work';
const hostHeader = __ENV.HOST_HEADER || 'bench.local';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');
const rate = Number(__ENV.RATE || '200');
const duration = __ENV.DURATION || '3600s';
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || '64');
const maxVUs = Number(__ENV.MAX_VUS || '512');
const markerIntervalSec = Number(__ENV.MARKER_INTERVAL_SEC || '300');

export const options = {
  scenarios: {
    long_duration: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};

export function setup() {
  const startUtc = new Date().toISOString();
  console.log(`[long_duration] run started at ${startUtc}`);
  return { startUtc, lastMarkerSec: 0 };
}

export default function (data) {
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

  // Emit a periodic marker line so operators can correlate with Prometheus.
  const elapsedSec = Math.floor((Date.now() - Date.parse(data.startUtc)) / 1000);
  const markerIndex = Math.floor(elapsedSec / markerIntervalSec);
  if (markerIndex > data.lastMarkerSec) {
    data.lastMarkerSec = markerIndex;
    console.log(`[long_duration] marker at ~${markerIndex * markerIntervalSec}s elapsed`);
  }
}

export function handleSummary(data) {
  const path = __ENV.SUMMARY_PATH || './summary-long_duration.json';
  return {
    [path]: JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: false }),
  };
}
