import { describe, expect, it } from 'vitest';
import { withChangeBetweenPoints } from '@/pages/metrics/history';

const point = (timestamp: number, totalRequests: number, avgLatency: number) => ({
  timestamp,
  totalRequests,
  avgLatency,
  requestsPerSec: 999,
});

describe('withChangeBetweenPoints', () => {
  it('gives each point what happened since the one before it', () => {
    const [, second] = withChangeBetweenPoints([point(100, 1000, 10), point(110, 1500, 20)]);

    expect(second.requestsPerSec).toBe(50);
    expect(second.avgLatency).toBe(40); // 500 requests took 20,000 ms of the 30,000
  });

  it('has no value for the first point', () => {
    const [first] = withChangeBetweenPoints([point(100, 1000, 10)]);

    expect(first.requestsPerSec).toBeNull();
    expect(first.avgLatency).toBeNull();
  });

  it('shows an idle moment as no requests and no latency', () => {
    const [, idle] = withChangeBetweenPoints([point(100, 1000, 10), point(110, 1000, 10)]);

    expect(idle.requestsPerSec).toBe(0);
    expect(idle.avgLatency).toBeNull();
  });

  it('does not draw a fall of the counters as negative traffic', () => {
    const [, reset] = withChangeBetweenPoints([point(100, 1000, 10), point(110, 5, 10)]);

    expect(reset.requestsPerSec).toBeNull();
    expect(reset.avgLatency).toBeNull();
  });
});
