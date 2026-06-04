import type { ReactNode } from "react";

// Small presentational primitives used by the comment sidebar's filter
// row. Kept in their own file so DocumentPage doesn't have to host them.

export function FilterButton({
  active,
  onClick,
  children,
  highlight,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
  highlight?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      className={[
        "px-2 py-1 rounded font-medium",
        active
          ? "bg-accent text-accent-fg"
          : highlight
            ? "text-accent hover:bg-accent-soft"
            : "text-muted hover:bg-soft",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

export function Count({ n, pulse }: { n: number; pulse?: boolean }) {
  return (
    <span
      className={[
        "ml-1 text-[10px] tabular-nums",
        pulse
          ? "inline-flex items-center justify-center min-w-[1.1rem] px-1 rounded-full bg-danger text-white font-semibold"
          : "opacity-70",
      ].join(" ")}
    >
      {n}
    </span>
  );
}
