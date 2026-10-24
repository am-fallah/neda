import { describe, expect, it } from "vitest";
import {
  formatElapsed,
  formatJalaliDate,
  formatJalaliNumeric,
  formatTehranTimestamp,
  gregorianToJalali,
  isIsoDate,
  isPastTehranDate,
  jalaliMonthDays,
  jalaliToGregorian,
  todayInTehran,
} from "./dates";

describe("Jalali/Gregorian conversion", () => {
  it("converts the documented provisional fixture", () => {
    expect(gregorianToJalali("2026-10-20")).toEqual({
      jy: 1405,
      jm: 7,
      jd: 28,
    });
    expect(formatJalaliDate("2026-10-20")).toBe("28 Mehr 1405");
    expect(formatJalaliNumeric("2026-10-20")).toBe("1405-07-28");
    expect(jalaliToGregorian(1405, 7, 28)).toBe("2026-10-20");
  });

  it("handles leap and non-leap Esfand lengths", () => {
    expect(jalaliMonthDays(1403, 12)).toBe(30);
    expect(jalaliMonthDays(1404, 12)).toBe(29);
    expect(jalaliMonthDays(1405, 12)).toBe(29);
  });

  it("handles Jalaali month and year boundaries", () => {
    expect(jalaliToGregorian(1405, 12, 29)).toBe("2027-03-20");
    expect(jalaliToGregorian(1406, 1, 1)).toBe("2027-03-21");
    expect(jalaliToGregorian(1403, 12, 30)).toBe("2025-03-20");
    expect(gregorianToJalali("2025-03-21")).toEqual({
      jy: 1404,
      jm: 1,
      jd: 1,
    });
  });

  it("rejects invalid dates rather than inventing values", () => {
    expect(isIsoDate("2026-02-30")).toBe(false);
    expect(isIsoDate("2026-10-20")).toBe(true);
    expect(gregorianToJalali("not-a-date")).toBeNull();
    expect(jalaliToGregorian(1405, 13, 1)).toBeNull();
    expect(formatJalaliDate("not-a-date")).toBe("not-a-date");
  });
});

describe("Tehran civil dates", () => {
  it("computes today in Asia/Tehran, not the browser timezone", () => {
    expect(todayInTehran(new Date("2026-10-20T20:29:00Z"))).toBe("2026-10-20");
    expect(todayInTehran(new Date("2026-10-20T21:30:00Z"))).toBe("2026-10-21");
    expect(todayInTehran(new Date("2026-10-20T00:30:00Z"))).toBe("2026-10-20");
  });

  it("compares dates against today in Tehran", () => {
    const now = new Date("2026-10-20T21:30:00Z");
    expect(isPastTehranDate("2026-10-20", now)).toBe(true);
    expect(isPastTehranDate("2026-10-21", now)).toBe(false);
    expect(isPastTehranDate("2026-10-22", now)).toBe(false);
  });

  it("formats timestamps in Tehran with an explicit label", () => {
    const formatted = formatTehranTimestamp("2026-10-20T21:30:00Z");
    expect(formatted).toContain("Tehran");
    expect(formatted).toContain("21 Oct 2026");
  });

  it("describes elapsed time without claiming freshness", () => {
    const now = new Date("2026-10-24T12:00:00Z");
    expect(formatElapsed("2026-10-24T11:59:30Z", now)).toBe("just now");
    expect(formatElapsed("2026-10-24T11:30:00Z", now)).toBe("30 min ago");
    expect(formatElapsed("2026-10-24T09:00:00Z", now)).toBe("3 h ago");
    expect(formatElapsed("2026-10-21T12:00:00Z", now)).toBe("3 d ago");
    expect(formatElapsed(null, now)).toBe("never");
  });
});
