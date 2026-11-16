export type CalendarKind = 'gregorian' | 'jalalian';

export interface YearMonth {
  year: number;
  month: number;
}

function parts(date: Date, calendar: CalendarKind, withHour: boolean) {
  const locale = calendar === 'jalalian' ? 'en-u-ca-persian-nu-latn' : 'en-u-ca-gregory-nu-latn';
  const fmt = new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    ...(withHour ? { hour: '2-digit', hourCycle: 'h23' as const } : {}),
  });
  const out: Record<string, string> = {};
  for (const p of fmt.formatToParts(date)) out[p.type] = p.value;
  return out;
}

/** The month the given moment falls in, in the panel's calendar. */
export function monthOf(date: Date, calendar: CalendarKind): YearMonth {
  const p = parts(date, calendar, false);
  return { year: Number.parseInt(p.year ?? '0', 10), month: Number.parseInt(p.month ?? '0', 10) };
}

/** The month before, keeping the year in step. */
export function previousMonth({ year, month }: YearMonth): YearMonth {
  return month === 1 ? { year: year - 1, month: 12 } : { year, month: month - 1 };
}

export function monthParam({ year, month }: YearMonth): string {
  return `${year}-${String(month).padStart(2, '0')}`;
}

/** A slot's start as YYYY-MM-DD (and HH:00 for an hour), Latin digits, in the calendar. */
export function slotLabel(unixSeconds: number, calendar: CalendarKind, hourly: boolean): string {
  const p = parts(new Date(unixSeconds * 1000), calendar, hourly);
  const day = `${(p.year ?? '').replace(/\D/g, '')}-${p.month}-${p.day}`;
  return hourly ? `${day} ${p.hour}:00` : day;
}

const PERSIAN_MONTHS = [
  'Farvardin',
  'Ordibehesht',
  'Khordad',
  'Tir',
  'Mordad',
  'Shahrivar',
  'Mehr',
  'Aban',
  'Azar',
  'Dey',
  'Bahman',
  'Esfand',
];
const PERSIAN_MONTHS_FA = [
  'فروردین',
  'اردیبهشت',
  'خرداد',
  'تیر',
  'مرداد',
  'شهریور',
  'مهر',
  'آبان',
  'آذر',
  'دی',
  'بهمن',
  'اسفند',
];

/** The month's name in the interface language. */
export function monthName(
  { year, month }: YearMonth,
  calendar: CalendarKind,
  language: string,
): string {
  if (calendar === 'jalalian') {
    const names = language.startsWith('fa') ? PERSIAN_MONTHS_FA : PERSIAN_MONTHS;
    return `${names[month - 1]} ${year}`;
  }
  return new Intl.DateTimeFormat(language, {
    year: 'numeric',
    month: 'long',
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(year, month - 1, 15)));
}

/** A CSV document Excel opens with its non-Latin text intact. */
export function toCsv(rows: (string | number)[][]): string {
  const cell = (v: string | number) => {
    const s = String(v);
    return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  return '﻿' + rows.map((r) => r.map(cell).join(',')).join('\r\n') + '\r\n';
}
