import { describe, expect, it } from 'vitest';
import { formatDuration, perWeek, timeChange, volumeChange, weeks } from './results-format';

describe('results format', () => {
  it('writes durations in minutes, hours and days', () => {
    expect(formatDuration(41 * 60)).toBe('41 min');
    expect(formatDuration(3 * 3600 + 12 * 60)).toBe('3 h 12 min');
    expect(formatDuration(9 * 3600 + 5 * 60)).toBe('9 h 05 min');
    expect(formatDuration(19 * 3600)).toBe('19 h');
    expect(formatDuration(28 * 3600)).toBe('1 d 4 h');
    expect(formatDuration(48 * 3600)).toBe('2 d');
  });

  it('says how a time changed, and that less time is better', () => {
    expect(timeChange(3 * 3600 + 12 * 60, 41 * 60)).toEqual({ direction: 'better', headline: '2 h 31 min faster', detail: '79% less time' });
    expect(timeChange(3600, 5400)).toEqual({ direction: 'worse', headline: '30 min slower', detail: '50% more time' });
    expect(timeChange(0, 60)).toBeUndefined();
  });

  it('says how volume changed without calling it better or worse', () => {
    expect(volumeChange(98, 92)).toEqual({ headline: '6% fewer', detail: '6 fewer a week' });
    expect(volumeChange(50, 60)).toEqual({ headline: '20% more', detail: '10 more a week' });
  });

  it('writes weekly figures and weeks without needless decimals', () => {
    expect(perWeek(2.8)).toBe('2.8');
    expect(perWeek(98.4)).toBe('98');
    expect(perWeek(5)).toBe('5');
    expect(weeks(30)).toBe('4.3');
    expect(weeks(7)).toBe('1');
  });
});
