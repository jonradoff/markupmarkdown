import { useCallback, useEffect, useState } from "react";

// Per-tab session state: comment IDs the user has already activated on a
// given doc. Backed by sessionStorage so navigating away and back to the
// same doc doesn't make recently-read comments show up as unread again.
//
// Keyed by docId. Storage value is a JSON array of comment IDs.

const STORAGE_PREFIX = "mm:read:";

function load(docId: string): Set<string> {
  try {
    const raw = sessionStorage.getItem(STORAGE_PREFIX + docId);
    if (!raw) return new Set();
    const arr = JSON.parse(raw);
    return new Set(Array.isArray(arr) ? arr : []);
  } catch {
    return new Set();
  }
}

function save(docId: string, set: Set<string>): void {
  try {
    sessionStorage.setItem(STORAGE_PREFIX + docId, JSON.stringify([...set]));
  } catch {
    // Quota or storage disabled — silently drop. Worst case: unread badge
    // is briefly wrong after a tab swap.
  }
}

export function useSessionReadIds(docId: string | undefined) {
  const [ids, setIds] = useState<Set<string>>(() =>
    docId ? load(docId) : new Set()
  );

  // Re-hydrate when the docId changes (navigating between docs).
  useEffect(() => {
    setIds(docId ? load(docId) : new Set());
  }, [docId]);

  const markRead = useCallback(
    (commentId: string) => {
      if (!docId || !commentId) return;
      setIds((prev) => {
        if (prev.has(commentId)) return prev;
        const next = new Set(prev);
        next.add(commentId);
        save(docId, next);
        return next;
      });
    },
    [docId]
  );

  return { ids, markRead };
}
