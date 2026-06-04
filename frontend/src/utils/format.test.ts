import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import { formatRelative, initials, colorFor } from "./format";

describe("formatRelative", () => {
  const NOW = new Date("2026-06-10T12:00:00Z").getTime();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders past times in human form", () => {
    expect(formatRelative(new Date(NOW - 30 * 1000).toISOString())).toBe("just now");
    expect(formatRelative(new Date(NOW - 5 * 60 * 1000).toISOString())).toBe("5m ago");
    expect(formatRelative(new Date(NOW - 3 * 60 * 60 * 1000).toISOString())).toBe("3h ago");
    expect(formatRelative(new Date(NOW - 2 * 86400 * 1000).toISOString())).toBe("2d ago");
  });

  it("renders future times with 'in' prefix", () => {
    expect(formatRelative(new Date(NOW + 30 * 1000).toISOString())).toBe("in a moment");
    expect(formatRelative(new Date(NOW + 20 * 60 * 1000).toISOString())).toBe("in 20m");
    expect(formatRelative(new Date(NOW + 5 * 60 * 60 * 1000).toISOString())).toBe("in 5h");
    expect(formatRelative(new Date(NOW + 7 * 86400 * 1000).toISOString())).toBe("in 7d");
  });

  it("falls back to a locale date for older-than-a-week timestamps", () => {
    const got = formatRelative(new Date(NOW - 10 * 86400 * 1000).toISOString());
    // Locale-dependent format, just check it's not the bucket strings.
    expect(got).not.toMatch(/ago|just now/);
  });
});

describe("initials", () => {
  it("picks the first letter of first + last name", () => {
    expect(initials("Alice Wonderland")).toBe("AW");
  });
  it("uses the first two characters when there's only one name", () => {
    expect(initials("alice")).toBe("AL");
  });
  it("handles empty / whitespace input", () => {
    expect(initials("")).toBe("?");
    expect(initials("   ")).toBe("?");
  });
});

describe("colorFor", () => {
  it("returns the same color for the same name", () => {
    expect(colorFor("alice")).toBe(colorFor("alice"));
  });
  it("returns a valid hex string", () => {
    expect(colorFor("alice")).toMatch(/^#[0-9a-f]{6}$/i);
  });
});
