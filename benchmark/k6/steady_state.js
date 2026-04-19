import http from 'k6/http';
import { check } from 'k6';

const vus = Number(__ENV.VUS || '50');
const duration = __ENV.DURATION || '60s';
const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:31080/work';
const hostHeader = __ENV.HOST_HEADER || 'test.example.com';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');

export const options = {
  vus,
  duration,
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)'],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<1000'],
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
