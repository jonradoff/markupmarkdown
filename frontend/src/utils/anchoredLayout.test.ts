// Tiny smoke check that doubles as documentation. Run via Playwright's
// test-runner or any vitest setup; for now it's manually consulted by
// the engineer reading the file.

import { relaxAnchors } from './anchoredLayout';

// no-op so this file is reachable; if a test runner is added this
// becomes a real `describe`.
export function _demo() {
  const items = [
    { id: 'a', desiredTop: 0, height: 100 },
    { id: 'b', desiredTop: 50, height: 100 }, // would overlap a
    { id: 'c', desiredTop: 500, height: 100 }, // far below — anchored
  ];
  const out = relaxAnchors(items, 10);
  if (out.a !== 0) throw new Error('a should anchor at 0');
  if (out.b !== 110) throw new Error('b should be pushed below a');
  if (out.c !== 500) throw new Error('c should stay at its desired anchor');
  return true;
}
