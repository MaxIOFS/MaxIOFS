export interface TimeRange {
  label: string;
  hours: number;
}

export const TIME_RANGES: TimeRange[] = [
  { label: 'Real-time', hours: 5 / 60 }, // 5 minutes for true real-time (10s intervals)
  { label: '1H', hours: 1 },
  { label: '6H', hours: 6 },
  { label: '24H', hours: 24 },
  { label: '7D', hours: 24 * 7 },
  { label: '30D', hours: 24 * 30 },
  { label: '1Y', hours: 24 * 365 },
];

// The steps a time axis ticks by, in seconds, from the finest.
const TICK_STEPS = [
  10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200,
  86400, 2 * 86400, 7 * 86400, 14 * 86400, 30 * 86400, 60 * 86400, 90 * 86400,
];

const MAX_TICKS = 8;

/**
 * The ticks of a time axis from start to end (Unix seconds): the finest round
 * step that gives at most eight, on multiples of it from local midnight; a
 * step of days ticks on local midnights.
 */
export function timeTicks(start: number, end: number): { ticks: number[]; step: number } {
  const span = Math.max(end - start, 1);
  const step = TICK_STEPS.find((s) => span / s < MAX_TICKS) ?? TICK_STEPS[TICK_STEPS.length - 1];
  const day = new Date(start * 1000);
  day.setHours(0, 0, 0, 0);
  const ticks: number[] = [];
  if (step >= 86400) {
    while (day.getTime() / 1000 < start) day.setDate(day.getDate() + 1);
    for (; day.getTime() / 1000 <= end; day.setDate(day.getDate() + step / 86400)) {
      ticks.push(day.getTime() / 1000);
    }
    return { ticks, step };
  }
  const midnight = day.getTime() / 1000;
  for (let tick = midnight + Math.ceil((start - midnight) / step) * step; tick <= end; tick += step) {
    ticks.push(tick);
  }
  return { ticks, step };
}

/** The label of a tick (Unix seconds) on a time axis that ticks by step. */
export function timeTickLabel(tick: number, step: number, locale: string): string {
  const date = new Date(tick * 1000);
  const midnight = date.getHours() === 0 && date.getMinutes() === 0 && date.getSeconds() === 0;
  if (step >= 86400 || (midnight && step >= 3600)) {
    return date.toLocaleDateString(locale, { day: 'numeric', month: 'short' });
  }
  return date.toLocaleTimeString(locale, step < 60
    ? { hour: '2-digit', minute: '2-digit', second: '2-digit' }
    : { hour: '2-digit', minute: '2-digit' });
}
