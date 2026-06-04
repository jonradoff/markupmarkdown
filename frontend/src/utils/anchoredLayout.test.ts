import { describe, expect, it } from "vitest";
import { relaxAnchors } from "./anchoredLayout";

describe("relaxAnchors", () => {
  it("returns an empty object for no items", () => {
    expect(relaxAnchors([])).toEqual({});
  });

  it("leaves non-overlapping items at their desired tops", () => {
    const out = relaxAnchors(
      [
        { id: "a", desiredTop: 0, height: 100 },
        { id: "b", desiredTop: 500, height: 100 },
      ],
      10,
    );
    expect(out).toEqual({ a: 0, b: 500 });
  });

  it("pushes overlapping items down to clear the previous one + gap", () => {
    const out = relaxAnchors(
      [
        { id: "a", desiredTop: 0, height: 100 },
        { id: "b", desiredTop: 50, height: 100 }, // would overlap a
      ],
      10,
    );
    expect(out.a).toBe(0);
    expect(out.b).toBe(110); // a.bottom (100) + gap (10)
  });

  it("only pushes down, never up — keeps items anchored to their highlights", () => {
    const out = relaxAnchors(
      [
        { id: "a", desiredTop: 200, height: 100 },
        { id: "b", desiredTop: 1000, height: 100 }, // far below, unaffected
      ],
      10,
    );
    expect(out.b).toBe(1000);
  });

  it("cascades pushes when many items pile up", () => {
    const out = relaxAnchors(
      [
        { id: "a", desiredTop: 0, height: 100 },
        { id: "b", desiredTop: 10, height: 100 },
        { id: "c", desiredTop: 20, height: 100 },
      ],
      10,
    );
    expect(out.a).toBe(0);
    expect(out.b).toBe(110);
    expect(out.c).toBe(220);
  });

  it("honors a custom gap", () => {
    const out = relaxAnchors(
      [
        { id: "a", desiredTop: 0, height: 100 },
        { id: "b", desiredTop: 0, height: 100 },
      ],
      30,
    );
    expect(out.b).toBe(130);
  });
});
