import { describe, expect, it } from 'vitest';

import { monthName, monthOf, previousMonth, slotLabel, toCsv } from '@/lib/usage/calendar';

// 2026-11-17 12:00 UTC is 26 Aban 1405 in Tehran and everywhere else that day.
const NOV_17 = Date.UTC(2026, 10, 17, 12) / 1000;

describe('usage calendar', () => {
  it('reads the Persian month of a day', () => {
    expect(monthOf(new Date(NOV_17 * 1000), 'jalalian')).toEqual({ year: 1405, month: 8 });
    expect(monthOf(new Date(NOV_17 * 1000), 'gregorian')).toEqual({ year: 2026, month: 11 });
  });

  it('labels a slot with Latin digits in the chosen calendar', () => {
    expect(slotLabel(NOV_17, 'jalalian', false)).toBe('1405-08-26');
    expect(slotLabel(NOV_17, 'gregorian', false)).toBe('2026-11-17');
  });

  it('steps back across a year', () => {
    expect(previousMonth({ year: 1405, month: 1 })).toEqual({ year: 1404, month: 12 });
  });

  it('names Persian months in Persian for a Persian interface', () => {
    expect(monthName({ year: 1405, month: 8 }, 'jalalian', 'fa-IR')).toBe('آبان 1405');
    expect(monthName({ year: 1405, month: 8 }, 'jalalian', 'en-US')).toBe('Aban 1405');
  });

  it('quotes what would break a CSV row and marks the file as UTF-8', () => {
    expect(
      toCsv([
        ['name', 'note'],
        ['a,b', 'say "hi"'],
      ]),
    ).toBe('﻿name,note\r\n"a,b","say ""hi"""\r\n');
  });
});
