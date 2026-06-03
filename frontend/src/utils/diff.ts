import { diffLines, diffWordsWithSpace, type Change } from "diff";

export interface DiffHunk {
  lines: DiffLine[];
}

export interface DiffLine {
  kind: "context" | "added" | "removed" | "changed-original" | "changed-revised";
  text: string;
  oldLineNumber?: number;
  newLineNumber?: number;
  // Word-level inline diff segments, present on changed-* lines.
  segments?: InlineSegment[];
}

export interface InlineSegment {
  text: string;
  kind: "same" | "added" | "removed";
}

export interface DiffStats {
  added: number;
  removed: number;
  changed: number;
  hunks: number;
}

const CONTEXT_LINES = 3;

export function computeDiff(
  original: string,
  revised: string
): { hunks: DiffHunk[]; stats: DiffStats } {
  const rawChanges: Change[] = diffLines(original, revised, {
    newlineIsToken: false,
  });

  // First, flatten into a per-line stream with stable line numbers,
  // pairing up matching removed/added lines so we can show inline word diffs.
  const flat: DiffLine[] = [];
  let oldLine = 1;
  let newLine = 1;

  const queue = rawChanges.slice();
  while (queue.length > 0) {
    const change = queue.shift()!;
    const lines = splitLines(change.value);

    if (!change.added && !change.removed) {
      for (const line of lines) {
        flat.push({
          kind: "context",
          text: line,
          oldLineNumber: oldLine++,
          newLineNumber: newLine++,
        });
      }
      continue;
    }

    if (change.removed) {
      const next = queue[0];
      if (next && next.added) {
        // Adjacent removed + added → render as paired "changed" lines with
        // word-level inline diff.
        const nextLines = splitLines(next.value);
        const pairs = Math.min(lines.length, nextLines.length);
        for (let i = 0; i < pairs; i++) {
          const o = lines[i];
          const n = nextLines[i];
          const { fromSegs, toSegs } = wordDiffLine(o, n);
          flat.push({
            kind: "changed-original",
            text: o,
            oldLineNumber: oldLine++,
            segments: fromSegs,
          });
          flat.push({
            kind: "changed-revised",
            text: n,
            newLineNumber: newLine++,
            segments: toSegs,
          });
        }
        for (let i = pairs; i < lines.length; i++) {
          flat.push({
            kind: "removed",
            text: lines[i],
            oldLineNumber: oldLine++,
          });
        }
        for (let i = pairs; i < nextLines.length; i++) {
          flat.push({
            kind: "added",
            text: nextLines[i],
            newLineNumber: newLine++,
          });
        }
        queue.shift();
        continue;
      }
      for (const line of lines) {
        flat.push({ kind: "removed", text: line, oldLineNumber: oldLine++ });
      }
      continue;
    }

    if (change.added) {
      for (const line of lines) {
        flat.push({ kind: "added", text: line, newLineNumber: newLine++ });
      }
    }
  }

  // Now group into hunks: stretches of changed lines with up to CONTEXT_LINES
  // of context on either side. Long stretches of context collapse.
  const hunks: DiffHunk[] = [];
  let currentHunk: DiffLine[] = [];
  let contextBuffer: DiffLine[] = [];

  const isChange = (l: DiffLine) => l.kind !== "context";

  for (const line of flat) {
    if (isChange(line)) {
      // Flush buffer (up to last CONTEXT_LINES) into the current hunk start.
      const lead = contextBuffer.slice(-CONTEXT_LINES);
      if (currentHunk.length === 0) {
        currentHunk.push(...lead);
      } else {
        // Continue same hunk: keep all buffered context between changes.
        currentHunk.push(...contextBuffer);
      }
      contextBuffer = [];
      currentHunk.push(line);
    } else {
      contextBuffer.push(line);
      if (currentHunk.length > 0 && contextBuffer.length > CONTEXT_LINES * 2) {
        // We've drifted far past the last change; close out this hunk with
        // CONTEXT_LINES of trailing context.
        currentHunk.push(...contextBuffer.slice(0, CONTEXT_LINES));
        hunks.push({ lines: currentHunk });
        currentHunk = [];
        contextBuffer = [];
      }
    }
  }
  if (currentHunk.length > 0) {
    currentHunk.push(...contextBuffer.slice(0, CONTEXT_LINES));
    hunks.push({ lines: currentHunk });
  }

  const stats: DiffStats = {
    added: flat.filter((l) => l.kind === "added").length,
    removed: flat.filter((l) => l.kind === "removed").length,
    changed: flat.filter((l) => l.kind === "changed-revised").length,
    hunks: hunks.length,
  };

  return { hunks, stats };
}

function splitLines(s: string): string[] {
  if (s === "") return [];
  const lines = s.split("\n");
  // diffLines includes the trailing newline in `value`; drop the empty tail.
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

function wordDiffLine(
  original: string,
  revised: string
): { fromSegs: InlineSegment[]; toSegs: InlineSegment[] } {
  const parts = diffWordsWithSpace(original, revised);
  const fromSegs: InlineSegment[] = [];
  const toSegs: InlineSegment[] = [];
  for (const part of parts) {
    if (part.added) {
      toSegs.push({ text: part.value, kind: "added" });
    } else if (part.removed) {
      fromSegs.push({ text: part.value, kind: "removed" });
    } else {
      fromSegs.push({ text: part.value, kind: "same" });
      toSegs.push({ text: part.value, kind: "same" });
    }
  }
  return { fromSegs, toSegs };
}
