import { describe, expect, it, beforeEach } from "vitest";
import { getAuthor, setAuthor } from "./author";

describe("author storage", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("returns empty string when nothing is stored", () => {
    expect(getAuthor()).toBe("");
  });

  it("round-trips through localStorage", () => {
    setAuthor("Alice");
    expect(getAuthor()).toBe("Alice");
  });

  it("trims surrounding whitespace before storing", () => {
    setAuthor("  Bob  ");
    expect(getAuthor()).toBe("Bob");
  });

  it("clears the stored value when given empty input", () => {
    setAuthor("Bob");
    setAuthor("");
    expect(getAuthor()).toBe("");
  });

  it("clears the stored value when given whitespace-only input", () => {
    setAuthor("Bob");
    setAuthor("   ");
    expect(getAuthor()).toBe("");
  });
});
