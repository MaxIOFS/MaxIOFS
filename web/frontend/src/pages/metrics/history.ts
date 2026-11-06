interface HistoryPoint {
  timestamp: number;
  totalRequests: number;
  avgLatency: number | null;
  requestsPerSec: number | null;
  [field: string]: string | number | null;
}

// The server keeps requestsPerSec and avgLatency as averages since its counters
// began, which hardly move: a chart needs what happened between two points. A
// point with nothing before it, or after the counters went back, has no value,
// and neither has the latency of a moment without requests.
export function withChangeBetweenPoints<T extends HistoryPoint>(points: T[]): T[] {
  return points.map((point, index) => {
    const previous = points[index - 1];
    const seconds = previous ? point.timestamp - previous.timestamp : 0;
    const requests = previous ? point.totalRequests - previous.totalRequests : -1;
    if (seconds <= 0 || requests < 0) {
      return { ...point, requestsPerSec: null, avgLatency: null };
    }
    if (requests === 0) {
      return { ...point, requestsPerSec: 0, avgLatency: null };
    }
    const latencyBefore = (previous.avgLatency ?? 0) * previous.totalRequests;
    const latencyNow = (point.avgLatency ?? 0) * point.totalRequests;
    return {
      ...point,
      requestsPerSec: requests / seconds,
      avgLatency: Math.max((latencyNow - latencyBefore) / requests, 0),
    };
  });
}
