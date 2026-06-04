import { describe, expect, it, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSessionReadIds } from "./sessionReadIds";

describe("useSessionReadIds", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it("starts empty for a new doc", () => {
    const { result } = renderHook(() => useSessionReadIds("doc-a"));
    expect(Array.from(result.current.ids)).toEqual([]);
  });

  it("persists read IDs to sessionStorage", () => {
    const { result } = renderHook(() => useSessionReadIds("doc-a"));
    act(() => result.current.markRead("c1"));
    expect(Array.from(result.current.ids)).toEqual(["c1"]);
    expect(sessionStorage.getItem("mm:read:doc-a")).toBe('["c1"]');
  });

  it("scopes reads per docId", () => {
    const { result: a } = renderHook(() => useSessionReadIds("doc-a"));
    act(() => a.current.markRead("c1"));
    const { result: b } = renderHook(() => useSessionReadIds("doc-b"));
    expect(Array.from(b.current.ids)).toEqual([]);
  });

  it("hydrates from sessionStorage on first mount", () => {
    sessionStorage.setItem("mm:read:doc-x", '["c1","c2"]');
    const { result } = renderHook(() => useSessionReadIds("doc-x"));
    expect(Array.from(result.current.ids).sort()).toEqual(["c1", "c2"]);
  });

  it("ignores duplicate markRead calls", () => {
    const { result } = renderHook(() => useSessionReadIds("doc-y"));
    act(() => result.current.markRead("c1"));
    act(() => result.current.markRead("c1"));
    expect(Array.from(result.current.ids)).toEqual(["c1"]);
  });
});
