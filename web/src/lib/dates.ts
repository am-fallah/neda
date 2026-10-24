import {
  isValidJalaaliDate,
  jalaaliMonthLength,
  toGregorian,
  toJalaali,
} from "jalaali-js";

export const TEHRAN_TIME_ZONE = "Asia/Tehran";

const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

const JALALI_MONTHS = [
  "Farvardin",
  "Ordibehesht",
  "Khordad",
  "Tir",
  "Mordad",
  "Shahrivar",
  "Mehr",
  "Aban",
  "Azar",
  "Dey",
  "Bahman",
  "Esfand",
] as const;

export interface JalaliDate {
  jy: number;
  jm: number;
  jd: number;
}

export function jalaliMonthName(month: number): string {
  return JALALI_MONTHS[month - 1] ?? "";
}

export function isIsoDate(value: string): boolean {
  const match = ISO_DATE.exec(value);
  if (!match) return false;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(Date.UTC(year, month - 1, day));
  return (
    date.getUTCFullYear() === year &&
    date.getUTCMonth() === month - 1 &&
    date.getUTCDate() === day
  );
}

function pad(value: number, length = 2): string {
  return String(value).padStart(length, "0");
}

/** Today's civil date in Tehran, formatted as a Gregorian YYYY-MM-DD value. */
export function todayInTehran(now: Date = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: TEHRAN_TIME_ZONE,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const values: Record<string, string> = {};
  for (const part of parts) {
    if (part.type !== "literal") values[part.type] = part.value;
  }
  return `${values.year}-${values.month}-${values.day}`;
}

export function gregorianToJalali(value: string): JalaliDate | null {
  if (!isIsoDate(value)) return null;
  const [gy, gm, gd] = value.split("-").map(Number);
  try {
    return toJalaali(gy, gm, gd);
  } catch {
    return null;
  }
}

export function jalaliToGregorian(
  jy: number,
  jm: number,
  jd: number,
): string | null {
  if (!isValidJalaaliDate(jy, jm, jd)) return null;
  const { gy, gm, gd } = toGregorian(jy, jm, jd);
  return `${pad(gy, 4)}-${pad(gm)}-${pad(gd)}`;
}

export function jalaliMonthDays(jy: number, jm: number): number {
  if (jm < 1 || jm > 12) return 0;
  return jalaaliMonthLength(jy, jm);
}

/**
 * Formats a Gregorian ISO date as a Jalali date such as `28 Mehr 1405`.
 * Invalid values are returned unchanged so the UI never invents a date.
 */
export function formatJalaliDate(value: string): string {
  const jalali = gregorianToJalali(value);
  if (!jalali) return value;
  return `${jalali.jd} ${jalaliMonthName(jalali.jm)} ${jalali.jy}`;
}

/** Short numeric Jalali form such as `1405-07-28`. */
export function formatJalaliNumeric(value: string): string {
  const jalali = gregorianToJalali(value);
  if (!jalali) return value;
  return `${jalali.jy}-${pad(jalali.jm)}-${pad(jalali.jd)}`;
}

export function formatGregorianDate(value: string): string {
  return isIsoDate(value) ? value : value;
}

/** Formats a UTC timestamp for display in Tehran time. */
export function formatTehranTimestamp(
  value: string | null | undefined,
  fallback = "—",
): string {
  if (!value) return fallback;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return fallback;
  return `${new Intl.DateTimeFormat("en-GB", {
    timeZone: TEHRAN_TIME_ZONE,
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date)} Tehran`;
}

export function formatDurationMs(value: number | null | undefined): string {
  if (value === null || value === undefined) return "—";
  if (value < 1000) return `${value} ms`;
  return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)} s`;
}

/** Human-readable elapsed time without claiming freshness. */
export function formatElapsed(
  value: string | null | undefined,
  now: Date = new Date(),
): string {
  if (!value) return "never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "unknown";
  const deltaMs = now.getTime() - date.getTime();
  if (deltaMs < 0) return "just now";
  const minutes = Math.floor(deltaMs / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  const days = Math.floor(hours / 24);
  return `${days} d ago`;
}

export function isPastTehranDate(
  value: string,
  now: Date = new Date(),
): boolean {
  return isIsoDate(value) && value < todayInTehran(now);
}
