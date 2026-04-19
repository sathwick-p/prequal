import http from 'k6/http';
import { check } from 'k6';

const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:31080/work';
const hostHeader = __ENV.HOST_HEADER || 'bench.local';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');
const rate = Number(__ENV.RATE || '200');
const duration = __ENV.DURATION || '60s';
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || '64');
const maxVUs = Number(__ENV.MAX_VUS || '256');

export const options = {
  scenarios: {
    open_loop: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)'],
  thresholds: {
    http_req_failed: ['rate<0.01'],
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
