// Pure helpers that operate on a (text, selectionStart, selectionEnd)
// triple and return a new one. Used by the editor's formatting
// toolbar so the textarea state stays single-source-of-truth — the
// component sets value/selection from the returned values.

export interface EditState {
  text: string;
  selectionStart: number;
  selectionEnd: number;
}

// applyWrap wraps the selection with `marker` on each side. With no
// selection, inserts the pair and places the cursor between them so
// the user can start typing inside. With a selection, toggles —
// removing the markers if they already surround the selection.
export function applyWrap(s: EditState, marker: string): EditState {
  const { text, selectionStart, selectionEnd } = s;
  const before = text.slice(0, selectionStart);
  const inside = text.slice(selectionStart, selectionEnd);
  const after = text.slice(selectionEnd);

  // Toggle off: selection already surrounded by markers, strip them.
  if (
    before.endsWith(marker) &&
    after.startsWith(marker)
  ) {
    const newText = before.slice(0, -marker.length) + inside + after.slice(marker.length);
    return {
      text: newText,
      selectionStart: selectionStart - marker.length,
      selectionEnd: selectionEnd - marker.length,
    };
  }

  // Inserting the wrap.
  if (inside.length === 0) {
    const newText = before + marker + marker + after;
    const caret = selectionStart + marker.length;
    return {
      text: newText,
      selectionStart: caret,
      selectionEnd: caret,
    };
  }
  const newText = before + marker + inside + marker + after;
  return {
    text: newText,
    selectionStart: selectionStart + marker.length,
    selectionEnd: selectionEnd + marker.length,
  };
}

// applyLinePrefix toggles a per-line prefix across every line touched
// by the selection. Each line gets prefix prepended if absent; if all
// touched lines already start with prefix, it's removed.
export function applyLinePrefix(s: EditState, prefix: string): EditState {
  const { text, selectionStart, selectionEnd } = s;
  const startLine = text.lastIndexOf("\n", selectionStart - 1) + 1;
  let endLine = text.indexOf("\n", selectionEnd);
  if (endLine === -1) endLine = text.length;
  const before = text.slice(0, startLine);
  const block = text.slice(startLine, endLine);
  const after = text.slice(endLine);

  const lines = block.split("\n");
  const allHave = lines.every((l) => l.startsWith(prefix));
  const newLines = allHave
    ? lines.map((l) => l.slice(prefix.length))
    : lines.map((l) => prefix + l);
  const newBlock = newLines.join("\n");
  const delta = newBlock.length - block.length;
  return {
    text: before + newBlock + after,
    selectionStart: selectionStart + (allHave ? -prefix.length : prefix.length),
    selectionEnd: selectionEnd + delta,
  };
}

// applyHeading replaces any existing leading `#`s on the first
// touched line with `level` `#`s. Toggles off if the same level was
// already set.
export function applyHeading(s: EditState, level: 1 | 2 | 3): EditState {
  const { text, selectionStart, selectionEnd } = s;
  const lineStart = text.lastIndexOf("\n", selectionStart - 1) + 1;
  const lineEnd = (() => {
    const i = text.indexOf("\n", lineStart);
    return i === -1 ? text.length : i;
  })();
  const line = text.slice(lineStart, lineEnd);
  const stripped = line.replace(/^#{1,6}\s*/, "");
  const want = "#".repeat(level) + " ";
  const newLine = line === want + stripped ? stripped : want + stripped;
  const delta = newLine.length - line.length;
  return {
    text: text.slice(0, lineStart) + newLine + text.slice(lineEnd),
    selectionStart: Math.max(lineStart, selectionStart + delta),
    selectionEnd: selectionEnd + delta,
  };
}

// applyLink inserts a markdown link. With selection, it becomes the
// link text; cursor lands inside the URL placeholder. Without
// selection, inserts `[](url)` with cursor on the link text.
export function applyLink(s: EditState): EditState {
  const { text, selectionStart, selectionEnd } = s;
  const inside = text.slice(selectionStart, selectionEnd);
  if (inside) {
    const insert = "[" + inside + "](url)";
    const newText = text.slice(0, selectionStart) + insert + text.slice(selectionEnd);
    const urlStart = selectionStart + inside.length + 3; // [text](
    return {
      text: newText,
      selectionStart: urlStart,
      selectionEnd: urlStart + 3, // "url"
    };
  }
  const insert = "[](url)";
  const newText = text.slice(0, selectionStart) + insert + text.slice(selectionEnd);
  return {
    text: newText,
    selectionStart: selectionStart + 1, // inside []
    selectionEnd: selectionStart + 1,
  };
}

// applyCodeBlock wraps selection with triple-backtick fences on their
// own lines. Selection is preserved between the fences.
export function applyCodeBlock(s: EditState): EditState {
  const { text, selectionStart, selectionEnd } = s;
  const inside = text.slice(selectionStart, selectionEnd) || "";
  // Ensure the opening fence starts on its own line.
  const before = text.slice(0, selectionStart);
  const after = text.slice(selectionEnd);
  const leadingNL = before.endsWith("\n") || before === "" ? "" : "\n";
  const trailingNL = after.startsWith("\n") || after === "" ? "" : "\n";
  const insert = leadingNL + "```\n" + inside + (inside.endsWith("\n") ? "" : "\n") + "```" + trailingNL;
  const newText = before + insert + after;
  const innerStart = selectionStart + leadingNL.length + 4; // "```\n"
  return {
    text: newText,
    selectionStart: innerStart,
    selectionEnd: innerStart + inside.length,
  };
}

// applyHR inserts a horizontal rule on its own line.
export function applyHR(s: EditState): EditState {
  const { text, selectionStart, selectionEnd } = s;
  const before = text.slice(0, selectionStart);
  const after = text.slice(selectionEnd);
  const leadingNL = before.endsWith("\n") || before === "" ? "" : "\n";
  const trailingNL = after.startsWith("\n") || after === "" ? "" : "\n";
  const insert = leadingNL + "---" + trailingNL;
  const newText = before + insert + after;
  const caret = selectionStart + insert.length;
  return {
    text: newText,
    selectionStart: caret,
    selectionEnd: caret,
  };
}
