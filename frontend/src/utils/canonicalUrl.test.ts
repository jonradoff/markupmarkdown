import { describe, it, expect } from "vitest";
import { canonicalDocPath, isReservedTopPath, gistURLFor } from "./canonicalUrl";
import type { MdDocument } from "../types";

// Minimal MdDocument helper — tests only touch the fields canonicalDocPath
// reads, so cast through unknown to keep the spread terse.
function doc(over: Partial<MdDocument>): MdDocument {
  return {
    id: "doc-id",
    title: "Test",
    origin: "url",
    content: "",
    ...over,
  } as unknown as MdDocument;
}

describe("canonicalDocPath", () => {
  it("returns the /owner/repo/blob/ref/path shape for github blob docs", () => {
    const got = canonicalDocPath(
      doc({
        sourceKind: "github_blob",
        githubOwner: "anthropics",
        githubRepo: "claude-code",
        githubRef: "main",
        githubPath: "README.md",
      }),
    );
    expect(got).toBe("/anthropics/claude-code/blob/main/README.md");
  });

  it("returns /gist/<owner>/<id> for gist docs", () => {
    const got = canonicalDocPath(
      doc({
        sourceKind: "gist",
        gistOwner: "cdhanna",
        gistId: "f64c136",
      }),
    );
    expect(got).toBe("/gist/cdhanna/f64c136");
  });

  it("returns null for gist docs missing owner or id", () => {
    expect(canonicalDocPath(doc({ sourceKind: "gist", gistOwner: "" }))).toBeNull();
    expect(canonicalDocPath(doc({ sourceKind: "gist", gistId: undefined }))).toBeNull();
  });

  it("returns null for upload / url docs", () => {
    expect(canonicalDocPath(doc({ sourceKind: "upload" }))).toBeNull();
    expect(canonicalDocPath(doc({ sourceKind: "url" }))).toBeNull();
  });

  it("returns null when github fields are missing (pre-migration safety)", () => {
    expect(canonicalDocPath(doc({}))).toBeNull();
  });

  it("falls back to main when githubRef is empty", () => {
    const got = canonicalDocPath(
      doc({
        sourceKind: "github_blob",
        githubOwner: "owner",
        githubRepo: "repo",
        githubRef: "",
        githubPath: "x.md",
      }),
    );
    expect(got).toBe("/owner/repo/blob/main/x.md");
  });

  it("refuses reserved top-level paths (collision protection)", () => {
    expect(
      canonicalDocPath(
        doc({
          sourceKind: "github_blob",
          githubOwner: "api",
          githubRepo: "repo",
          githubPath: "x.md",
        }),
      ),
    ).toBeNull();
    expect(
      canonicalDocPath(
        doc({
          sourceKind: "github_blob",
          githubOwner: "gist",
          githubRepo: "repo",
          githubPath: "x.md",
        }),
      ),
    ).toBeNull();
  });

  it("URL-encodes path segments", () => {
    const got = canonicalDocPath(
      doc({
        sourceKind: "github_blob",
        githubOwner: "owner",
        githubRepo: "repo",
        githubRef: "main",
        githubPath: "docs/Spaces In Name.md",
      }),
    );
    expect(got).toBe("/owner/repo/blob/main/docs/Spaces%20In%20Name.md");
  });
});

describe("isReservedTopPath", () => {
  it("recognizes the literal /gist/ prefix", () => {
    expect(isReservedTopPath("gist")).toBe(true);
  });

  it("does not flag arbitrary names", () => {
    expect(isReservedTopPath("cdhanna")).toBe(false);
    expect(isReservedTopPath("anthropics")).toBe(false);
  });
});

describe("gistURLFor", () => {
  it("constructs the landing-page URL", () => {
    expect(gistURLFor("cdhanna", "f64c136")).toBe("https://gist.github.com/cdhanna/f64c136");
  });
});
