import http from 'k6/http';
import { check } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:30080/work';
const routeAHost = __ENV.ROUTE_A_HOST || 'route-a.bench.local';
const routeBHost = __ENV.ROUTE_B_HOST || 'route-b.bench.local';
const iterations = Number(__ENV.WORK_ITERATIONS || '1000');
const duration = __ENV.DURATION || '60s';
const rate = Number(__ENV.RATE || '200');
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || '64');
const maxVUs = Number(__ENV.MAX_VUS || '256');
const routeAWeight = Number(__ENV.ROUTE_A_WEIGHT || '8');
const routeBWeight = Number(__ENV.ROUTE_B_WEIGHT || '2');

const weightedHosts = [];
for (let i = 0; i < routeAWeight; i += 1) {
  weightedHosts.push(routeAHost);
}
for (let i = 0; i < routeBWeight; i += 1) {
  weightedHosts.push(routeBHost);
}

export const options = {
  scenarios: {
    multi_route: {
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

export default function () {
  const hostHeader = weightedHosts[Math.floor(Math.random() * weightedHosts.length)];
  const response = http.post(
    baseURL,
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
