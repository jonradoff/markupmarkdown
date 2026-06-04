import { describe, expect, it } from "vitest";
import { baseURLForDoc, makeUrlTransform } from "./baseUrl";

describe("baseURLForDoc", () => {
  it("returns undefined for an empty input", () => {
    expect(baseURLForDoc(undefined)).toBeUndefined();
  });

  it("rewrites github.com/blob URLs to the raw.githubusercontent equivalent", () => {
    expect(
      baseURLForDoc(
        "https://github.com/foo/bar/blob/main/docs/README.md",
      ),
    ).toBe("https://raw.githubusercontent.com/foo/bar/main/docs/README.md");
  });

  it("leaves non-github URLs unchanged", () => {
    expect(baseURLForDoc("https://example.com/foo.md")).toBe(
      "https://example.com/foo.md",
    );
  });

  it("leaves a raw.githubusercontent URL alone", () => {
    const raw =
      "https://raw.githubusercontent.com/foo/bar/main/README.md";
    expect(baseURLForDoc(raw)).toBe(raw);
  });
});

describe("makeUrlTransform", () => {
  it("blocks javascript: URLs", () => {
    const tx = makeUrlTransform();
    expect(tx("javascript:alert(1)")).toBe("");
  });

  it("blocks vbscript: URLs", () => {
    const tx = makeUrlTransform();
    expect(tx("vbscript:msgbox('x')")).toBe("");
  });

  it("allows data:image URLs", () => {
    const tx = makeUrlTransform();
    expect(tx("data:image/png;base64,abc")).toBe(
      "data:image/png;base64,abc",
    );
  });

  it("blocks non-image data: URLs", () => {
    const tx = makeUrlTransform();
    expect(tx("data:text/html,<script>")).toBe("");
  });

  it("preserves http(s) URLs unchanged", () => {
    const tx = makeUrlTransform();
    expect(tx("https://example.com/x")).toBe("https://example.com/x");
  });

  it("resolves relative paths against the base", () => {
    const tx = makeUrlTransform(
      "https://raw.githubusercontent.com/foo/bar/main/docs/README.md",
    );
    expect(tx("../assets/logo.png")).toBe(
      "https://raw.githubusercontent.com/foo/bar/main/assets/logo.png",
    );
  });

  it("returns the input unchanged when no base is provided", () => {
    const tx = makeUrlTransform();
    expect(tx("./foo.png")).toBe("./foo.png");
  });
});
