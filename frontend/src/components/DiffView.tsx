import { useCallback, useMemo, useRef, useState } from "react";
import MarkdownRender from "./MarkdownRender";
import { computeDiff, type DiffHunk, type DiffLine, type InlineSegment } from "../utils/diff";

interface Props {
  original: string;
  revised: string;
  baseUrl?: string;
}

type View = "diff" | "rendered";

export default function DiffView({ original, revised, baseUrl }: Props) {
  const [view, setView] = useState<View>("diff");
  const { hunks, stats } = useMemo(
    () => computeDiff(original, revised),
    [original, revised]
  );
  // Refs to each hunk's outer div so Prev/Next can scroll the right
  // one into view inside the diff scroll container. Cleared on every
  // render so removed hunks don't leak stale node refs.
  const hunkRefs = useRef<(HTMLDivElement | null)[]>([]);
  hunkRefs.current = [];
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  // Track which hunk is "active" so Prev/Next have a known starting
  // point. Defaults to the first hunk; bumped by either button.
  const [activeHunk, setActiveHunk] = useState(0);

  const scrollToHunk = useCallback((i: number) => {
    const el = hunkRefs.current[i];
    const scroller = scrollerRef.current;
    if (!el || !scroller) return;
    // Manual computation rather than scrollIntoView so the hunk lands
    // below the sticky toolbar (which is INSIDE the scroller, so the
    // hunk's "top" must clear it). 44px ≈ toolbar height.
    const elRect = el.getBoundingClientRect();
    const sRect = scroller.getBoundingClientRect();
    const offsetTop = elRect.top - sRect.top + scroller.scrollTop;
    scroller.scrollTo({ top: Math.max(0, offsetTop - 44), behavior: "smooth" });
    setActiveHunk(i);
  }, []);

  const nextHunk = useCallback(() => {
    if (stats.hunks === 0) return;
    scrollToHunk((activeHunk + 1) % stats.hunks);
  }, [activeHunk, stats.hunks, scrollToHunk]);
  const prevHunk = useCallback(() => {
    if (stats.hunks === 0) return;
    scrollToHunk((activeHunk - 1 + stats.hunks) % stats.hunks);
  }, [activeHunk, stats.hunks, scrollToHunk]);

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="flex items-center justify-between px-4 py-2 border-b border-rule shrink-0">
        <div className="text-xs text-muted flex items-center gap-4 tabular-nums">
          <span>
            <span className="text-success font-semibold">+{stats.added + stats.changed}</span>
            {"  "}
            <span className="text-danger font-semibold">−{stats.removed + stats.changed}</span>
          </span>
          <span>
            {stats.hunks} {stats.hunks === 1 ? "change" : "changes"}
          </span>
          {view === "diff" && stats.hunks > 0 && (
            <span className="flex items-center gap-1">
              <button
                onClick={prevHunk}
                className="px-2 py-0.5 rounded border border-rule text-muted hover:text-ink hover:bg-soft disabled:opacity-40"
                title="Previous change (jump to the prior diff hunk)"
              >
                ‹ Prev
              </button>
              <button
                onClick={nextHunk}
                className="px-2 py-0.5 rounded border border-rule text-muted hover:text-ink hover:bg-soft disabled:opacity-40"
                title="Next change (jump to the next diff hunk)"
              >
                Next ›
              </button>
              <span className="text-faint">
                {activeHunk + 1} / {stats.hunks}
              </span>
            </span>
          )}
        </div>
        <div className="flex items-center gap-1 text-xs">
          <ViewTab active={view === "diff"} onClick={() => setView("diff")}>
            Diff
          </ViewTab>
          <ViewTab active={view === "rendered"} onClick={() => setView("rendered")}>
            Rendered
          </ViewTab>
        </div>
      </div>

      <div ref={scrollerRef} className="flex-1 min-h-0 overflow-auto">
        {view === "diff" ? (
          stats.hunks === 0 ? (
            <div className="p-10 text-center text-muted text-sm">
              No changes. Claude returned the document unchanged based on the
              resolved comments.
            </div>
          ) : (
            <UnifiedDiff hunks={hunks} hunkRefs={hunkRefs} activeHunk={activeHunk} />
          )
        ) : (
          <div className="p-6 max-w-3xl mx-auto">
            <MarkdownRender content={revised} baseUrl={baseUrl} />
          </div>
        )}
      </div>
    </div>
  );
}

function ViewTab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={[
        "px-3 py-1 rounded font-medium",
        active ? "bg-accent text-accent-fg" : "text-muted hover:bg-soft",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

function UnifiedDiff({
  hunks,
  hunkRefs,
  activeHunk,
}: {
  hunks: DiffHunk[];
  hunkRefs: React.MutableRefObject<(HTMLDivElement | null)[]>;
  activeHunk: number;
}) {
  return (
    <div className="font-mono text-[12px] leading-[1.55]">
      {hunks.map((hunk, i) => {
        const head = hunk.lines.find((l) => l.kind !== "context");
        const headLineOld =
          head?.oldLineNumber ?? head?.newLineNumber ?? "?";
        const headLineNew =
          head?.newLineNumber ?? head?.oldLineNumber ?? "?";
        return (
          <div
            key={i}
            ref={(el) => {
              hunkRefs.current[i] = el;
            }}
            className="mb-4"
          >
            <div
              className={[
                "px-3 py-1 text-[11px] border-y border-rule sticky top-0 z-10",
                i === activeHunk
                  ? "bg-accent-soft text-accent font-medium"
                  : "bg-soft text-muted",
              ].join(" ")}
            >
              @@ −{headLineOld}, +{headLineNew} @@
              <span className="ml-2 text-faint font-normal">
                {i + 1} of {hunks.length}
              </span>
            </div>
            <div>
              {hunk.lines.map((line, j) => (
                <DiffLineRow key={j} line={line} />
              ))}
            </div>
          </div>
        );
      })}
      <div className="h-4" />
    </div>
  );
}

function DiffLineRow({ line }: { line: DiffLine }) {
  const bg = {
    context: "",
    added: "bg-success/10",
    removed: "bg-danger/10",
    "changed-original": "bg-danger/10",
    "changed-revised": "bg-success/10",
  }[line.kind];

  const marker = {
    context: " ",
    added: "+",
    removed: "−",
    "changed-original": "−",
    "changed-revised": "+",
  }[line.kind];

  const markerColor = {
    context: "text-faint",
    added: "text-success",
    removed: "text-danger",
    "changed-original": "text-danger",
    "changed-revised": "text-success",
  }[line.kind];

  const ln = (n?: number) =>
    n === undefined ? (
      <span className="text-faint">·</span>
    ) : (
      <span className="text-faint tabular-nums">{n}</span>
    );

  return (
    <div className={["grid grid-cols-[3rem_3rem_1.25rem_1fr]", bg].join(" ")}>
      <div className="px-2 text-right">{ln(line.oldLineNumber)}</div>
      <div className="px-2 text-right">{ln(line.newLineNumber)}</div>
      <div className={["text-center", markerColor].join(" ")}>{marker}</div>
      <div className="pr-3 whitespace-pre-wrap break-words">
        {line.segments ? (
          renderSegments(line.segments)
        ) : (
          <span>{line.text || "​"}</span>
        )}
      </div>
    </div>
  );
}

function renderSegments(segs: InlineSegment[]) {
  return segs.map((s, i) => {
    if (s.kind === "same") return <span key={i}>{s.text}</span>;
    if (s.kind === "added")
      return (
        <span key={i} className="bg-success/30 rounded-sm">
          {s.text}
        </span>
      );
    return (
      <span key={i} className="bg-danger/30 rounded-sm line-through decoration-danger/60">
        {s.text}
      </span>
    );
  });
}
