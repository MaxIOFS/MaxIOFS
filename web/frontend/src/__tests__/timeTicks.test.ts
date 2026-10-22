import { describe, it, expect } from 'vitest';
import { timeTickLabel, timeTicks, TIME_RANGES } from '@/components/charts/timeRanges';

const at = (iso: string) => Math.floor(new Date(iso).getTime() / 1000);
const window = (end: number, hours: number) => ({ start: end - Math.round(hours * 3600), end });

describe('time axis ticks', () => {
  it('fall on round times inside the range, eight at most', () => {
    const end = at('2026-10-22T12:47:13');
    for (const range of TIME_RANGES) {
      const { start } = window(end, range.hours);
      const { ticks, step } = timeTicks(start, end);
      expect(ticks.length, range.label).toBeGreaterThanOrEqual(2);
      expect(ticks.length, range.label).toBeLessThanOrEqual(8);
      for (const tick of ticks) {
        expect(tick, range.label).toBeGreaterThanOrEqual(start);
        expect(tick, range.label).toBeLessThanOrEqual(end);
      }
      if (step < 86400) {
        for (let i = 1; i < ticks.length; i++) {
          expect(ticks[i] - ticks[i - 1], range.label).toBe(step);
        }
      }
    }
  });

  it('are a minute apart in real time, on the minute, and move with the window', () => {
    const end = at('2026-10-22T12:47:13');
    const now = timeTicks(end - 300, end);
    expect(now.step).toBe(60);
    for (const tick of now.ticks) {
      expect(new Date(tick * 1000).getSeconds()).toBe(0);
    }
    const later = timeTicks(end - 300 + 60, end + 60);
    expect(later.ticks.at(-1)).toBe(now.ticks.at(-1)! + 60);
  });

  it('are local hours over a day and local midnights over a week', () => {
    const end = at('2026-10-22T12:47:13');
    const day = timeTicks(end - 86400, end);
    expect(day.step % 3600).toBe(0);
    for (const tick of day.ticks) {
      const d = new Date(tick * 1000);
      expect(d.getMinutes()).toBe(0);
      expect(d.getHours() % (day.step / 3600)).toBe(0);
    }
    const week = timeTicks(end - 7 * 86400, end);
    expect(week.step).toBe(86400);
    for (const tick of week.ticks) {
      const d = new Date(tick * 1000);
      expect([d.getHours(), d.getMinutes()]).toEqual([0, 0]);
    }
  });

  it('name the day where an hour does not, and the second where a minute does not', () => {
    const midnight = at('2026-10-22T00:00:00');
    expect(timeTickLabel(midnight, 21600, 'en-GB')).not.toContain(':');
    expect(timeTickLabel(midnight + 21600, 21600, 'en-GB')).toBe('06:00');
    expect(timeTickLabel(midnight, 86400, 'en-GB')).toContain('22');
    expect(timeTickLabel(midnight + 30, 15, 'en-GB')).toBe('00:00:30');
  });
});
