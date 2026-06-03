// Compute the URL to use as the base for resolving relative image/link refs
// inside a markdown document.
//
// We auto-rewrite github.com/{owner}/{repo}/blob/{branch}/{path} → the matching
// raw.githubusercontent.com URL, so relative paths like `.github/logo.svg`
// resolve to actual image files instead of localhost.
export function baseURLForDoc(sourceUrl?: string): string | undefined {
  if (!sourceUrl) return undefined;
  const m = sourceUrl.match(
    /^https:\/\/github\.com\/([^/]+)\/([^/]+)\/blob\/([^/]+)\/(.+)$/
  );
  if (m) {
    return `https://raw.githubusercontent.com/${m[1]}/${m[2]}/${m[3]}/${m[4]}`;
  }
  return sourceUrl;
}

const UNSAFE = /^(javascript|vbscript|data:(?!image\/))/i;

export function makeUrlTransform(baseUrl?: string) {
  return (url: string) => {
    if (!url) return url;
    if (UNSAFE.test(url)) return "";
    if (/^(https?:|mailto:|#)/.test(url)) return url;
    if (url.startsWith("data:")) return url;
    if (!baseUrl) return url;
    try {
      return new URL(url, baseUrl).toString();
    } catch {
      return url;
    }
  };
}
