import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

// Lightweight, non-modal toast queue. Surfaces background-action errors and
// transient success ("Saved") flashes without stealing focus.
//
// Use:
//   const toast = useToast();
//   toast.error("Couldn't save: …");
//   toast.success("Saved");

type ToastKind = "error" | "success" | "info";

interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
  // Optional: at most one action button per toast.
  action?: { label: string; onClick: () => void };
}

interface ToastAPI {
  error(message: string, action?: { label: string; onClick: () => void }): void;
  success(message: string): void;
  info(message: string): void;
}

const ToastContext = createContext<ToastAPI | null>(null);

const DURATION_MS: Record<ToastKind, number> = {
  error: 6000,
  success: 2200,
  info: 3500,
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const idRef = useRef(0);

  const dismiss = useCallback((id: number) => {
    setItems((list) => list.filter((t) => t.id !== id));
  }, []);

  const push = useCallback(
    (
      kind: ToastKind,
      message: string,
      action?: { label: string; onClick: () => void }
    ) => {
      const id = ++idRef.current;
      setItems((list) => [...list, { id, kind, message, action }]);
      window.setTimeout(() => dismiss(id), DURATION_MS[kind]);
    },
    [dismiss]
  );

  const api = useMemo<ToastAPI>(
    () => ({
      error: (msg, action) => push("error", msg, action),
      success: (msg) => push("success", msg),
      info: (msg) => push("info", msg),
    }),
    [push]
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="fixed z-[100] bottom-4 left-1/2 -translate-x-1/2 flex flex-col gap-2 pointer-events-none">
        {items.map((t) => (
          <ToastItemView key={t.id} item={t} onDismiss={() => dismiss(t.id)} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastAPI {
  const v = useContext(ToastContext);
  if (!v) throw new Error("useToast must be used inside ToastProvider");
  return v;
}

function ToastItemView({
  item,
  onDismiss,
}: {
  item: ToastItem;
  onDismiss: () => void;
}) {
  // Slide in on mount.
  const [show, setShow] = useState(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => setShow(true));
    return () => cancelAnimationFrame(id);
  }, []);

  const colorClasses =
    item.kind === "error"
      ? "bg-danger text-white"
      : item.kind === "success"
      ? "bg-success text-white"
      : "bg-ink text-card";

  return (
    <div
      role={item.kind === "error" ? "alert" : "status"}
      className={[
        "pointer-events-auto max-w-md shadow-lg rounded-lg px-4 py-2.5 text-sm flex items-start gap-3 transition-all duration-200",
        colorClasses,
        show ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2",
      ].join(" ")}
    >
      <div className="flex-1 min-w-0 break-words">{item.message}</div>
      {item.action && (
        <button
          onClick={() => {
            item.action!.onClick();
            onDismiss();
          }}
          className="text-xs font-medium underline shrink-0"
        >
          {item.action.label}
        </button>
      )}
      <button
        onClick={onDismiss}
        aria-label="Dismiss"
        className="opacity-70 hover:opacity-100 shrink-0 -mr-1 -mt-0.5"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>
  );
}

// Convenience: convert an unknown error (Error / APIError / string) into a
// user-readable message.
export function toastMessageFor(err: unknown): string {
  if (!err) return "Something went wrong.";
  if (typeof err === "string") return err;
  if (err instanceof Error) return err.message;
  return String(err);
}
