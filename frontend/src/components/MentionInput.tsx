import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { api } from "../api";
import type { MentionCandidate } from "../types";

// Textarea wrapper with a lightweight @-autocomplete dropdown. The candidate
// list is lazy-loaded the first time the user types "@" — empty results just
// suppress the menu silently.

interface Props {
  documentId: string;
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  rows?: number;
  autoFocus?: boolean;
  onSubmit?: () => void; // ⌘/Ctrl+Enter
  onEscape?: () => void;
  className?: string;
}

export interface MentionInputHandle {
  focus: () => void;
}

const MentionInput = forwardRef<MentionInputHandle, Props>(function MentionInput(
  {
    documentId,
    value,
    onChange,
    placeholder,
    rows = 3,
    autoFocus,
    onSubmit,
    onEscape,
    className,
  },
  ref
) {
  const taRef = useRef<HTMLTextAreaElement>(null);
  const [candidates, setCandidates] = useState<MentionCandidate[] | null>(null);
  const [query, setQuery] = useState<string | null>(null);
  const [activeIdx, setActiveIdx] = useState(0);
  const [mentionStart, setMentionStart] = useState<number | null>(null);

  useImperativeHandle(ref, () => ({
    focus: () => taRef.current?.focus(),
  }));

  useEffect(() => {
    if (autoFocus) taRef.current?.focus();
  }, [autoFocus]);

  const loadCandidates = useCallback(async () => {
    if (candidates !== null) return;
    try {
      const list = await api.listMentionCandidates(documentId);
      setCandidates(list);
    } catch {
      setCandidates([]);
    }
  }, [candidates, documentId]);

  // Detect an in-progress @mention before the cursor.
  function updateMentionState(text: string, caret: number) {
    // Walk backwards from caret to find an unbroken @login fragment.
    let i = caret - 1;
    while (i >= 0) {
      const ch = text[i];
      if (ch === "@") {
        const prev = i > 0 ? text[i - 1] : "";
        // Only trigger when @ starts a token (prev char is whitespace or start).
        if (prev === "" || /\s|[^\w]/.test(prev)) {
          const frag = text.slice(i + 1, caret);
          if (/^[a-zA-Z0-9-]*$/.test(frag)) {
            setMentionStart(i);
            setQuery(frag.toLowerCase());
            setActiveIdx(0);
            loadCandidates();
            return;
          }
        }
        break;
      }
      if (/\s|[^a-zA-Z0-9-]/.test(ch)) break;
      i--;
    }
    setMentionStart(null);
    setQuery(null);
  }

  function handleChange(e: React.ChangeEvent<HTMLTextAreaElement>) {
    const next = e.target.value;
    onChange(next);
    updateMentionState(next, e.target.selectionStart);
  }

  function commitMention(candidate: MentionCandidate) {
    if (mentionStart == null) return;
    const ta = taRef.current;
    if (!ta) return;
    const before = value.slice(0, mentionStart);
    const after = value.slice(ta.selectionStart);
    const insert = `@${candidate.login} `;
    const next = before + insert + after;
    onChange(next);
    setMentionStart(null);
    setQuery(null);
    requestAnimationFrame(() => {
      ta.focus();
      const pos = (before + insert).length;
      ta.setSelectionRange(pos, pos);
    });
  }

  const filtered =
    query != null && candidates
      ? candidates
          .filter(
            (c) =>
              c.login.toLowerCase().startsWith(query) ||
              c.name.toLowerCase().includes(query)
          )
          .slice(0, 6)
      : [];
  const showMenu = mentionStart != null && filtered.length > 0;

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (showMenu) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIdx((i) => (i + 1) % filtered.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIdx((i) => (i - 1 + filtered.length) % filtered.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        commitMention(filtered[activeIdx]);
        return;
      }
      if (e.key === "Escape") {
        setMentionStart(null);
        setQuery(null);
        return;
      }
    }
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      onSubmit?.();
      return;
    }
    if (e.key === "Escape") {
      onEscape?.();
    }
  }

  return (
    <div className="relative">
      <textarea
        ref={taRef}
        value={value}
        onChange={handleChange}
        onKeyDown={onKeyDown}
        onBlur={() => {
          // Defer close so a candidate click registers first.
          setTimeout(() => {
            setMentionStart(null);
            setQuery(null);
          }, 120);
        }}
        rows={rows}
        placeholder={placeholder}
        className={
          className ??
          "w-full text-sm border border-rule rounded p-2 resize-none focus:outline-none focus:border-accent"
        }
      />
      {showMenu && (
        <div className="absolute left-0 right-0 mt-1 bg-card border border-rule rounded-lg shadow-lg z-30 overflow-hidden">
          {filtered.map((c, i) => (
            <button
              key={c.login}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault();
                commitMention(c);
              }}
              onMouseEnter={() => setActiveIdx(i)}
              className={[
                "w-full flex items-center gap-2 px-3 py-2 text-left text-sm",
                i === activeIdx ? "bg-soft" : "",
              ].join(" ")}
            >
              {c.avatarUrl ? (
                <img
                  src={c.avatarUrl}
                  alt=""
                  className="w-6 h-6 rounded-full"
                  loading="lazy"
                />
              ) : (
                <span className="w-6 h-6 rounded-full bg-soft" />
              )}
              <span className="font-medium">@{c.login}</span>
              {c.name && c.name !== c.login && (
                <span className="text-muted">· {c.name}</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
});

export default MentionInput;
