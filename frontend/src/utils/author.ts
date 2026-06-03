const KEY = "markupmarkdown:author";

export function getAuthor(): string {
  return localStorage.getItem(KEY) || "";
}

export function setAuthor(name: string) {
  if (name.trim()) {
    localStorage.setItem(KEY, name.trim());
  } else {
    localStorage.removeItem(KEY);
  }
}

