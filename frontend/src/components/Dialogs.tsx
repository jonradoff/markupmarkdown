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
import Modal from "./Modal";

// Styled replacements for window.alert / window.confirm / window.prompt.
// Wrap the app in <DialogProvider> and call `const dialog = useDialog()`.

interface AlertOpts {
  title?: string;
  body?: ReactNode;
  confirmLabel?: string;
}
interface ConfirmOpts {
  title?: string;
  body?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}
interface PromptOpts {
  title?: string;
  body?: ReactNode;
  defaultValue?: string;
  placeholder?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

interface DialogAPI {
  alert(opts: AlertOpts): Promise<void>;
  confirm(opts: ConfirmOpts): Promise<boolean>;
  prompt(opts: PromptOpts): Promise<string | null>;
}

const DialogContext = createContext<DialogAPI | null>(null);

type Pending =
  | { kind: "alert"; opts: AlertOpts; resolve: () => void }
  | { kind: "confirm"; opts: ConfirmOpts; resolve: (v: boolean) => void }
  | { kind: "prompt"; opts: PromptOpts; resolve: (v: string | null) => void };

export function DialogProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);

  const api = useMemo<DialogAPI>(
    () => ({
      alert: (opts) =>
        new Promise<void>((resolve) => {
          setPending({ kind: "alert", opts, resolve });
        }),
      confirm: (opts) =>
        new Promise<boolean>((resolve) => {
          setPending({ kind: "confirm", opts, resolve });
        }),
      prompt: (opts) =>
        new Promise<string | null>((resolve) => {
          setPending({ kind: "prompt", opts, resolve });
        }),
    }),
    []
  );

  function close() {
    setPending(null);
  }

  return (
    <DialogContext.Provider value={api}>
      {children}
      {pending?.kind === "alert" && (
        <AlertDialog
          opts={pending.opts}
          onDone={() => {
            const r = pending.resolve;
            close();
            r();
          }}
        />
      )}
      {pending?.kind === "confirm" && (
        <ConfirmDialog
          opts={pending.opts}
          onDone={(v) => {
            const r = pending.resolve;
            close();
            r(v);
          }}
        />
      )}
      {pending?.kind === "prompt" && (
        <PromptDialog
          opts={pending.opts}
          onDone={(v) => {
            const r = pending.resolve;
            close();
            r(v);
          }}
        />
      )}
    </DialogContext.Provider>
  );
}

export function useDialog(): DialogAPI {
  const v = useContext(DialogContext);
  if (!v) throw new Error("useDialog must be used inside DialogProvider");
  return v;
}

function AlertDialog({ opts, onDone }: { opts: AlertOpts; onDone: () => void }) {
  return (
    <Modal title={opts.title} onClose={onDone}>
      <div className="text-sm text-ink whitespace-pre-wrap mb-4">
        {opts.body}
      </div>
      <div className="flex justify-end">
        <button
          autoFocus
          onClick={onDone}
          className="text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90"
        >
          {opts.confirmLabel ?? "OK"}
        </button>
      </div>
    </Modal>
  );
}

function ConfirmDialog({
  opts,
  onDone,
}: {
  opts: ConfirmOpts;
  onDone: (v: boolean) => void;
}) {
  const confirmRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    confirmRef.current?.focus();
  }, []);
  return (
    <Modal title={opts.title} onClose={() => onDone(false)}>
      <div className="text-sm text-ink whitespace-pre-wrap mb-4">
        {opts.body}
      </div>
      <div className="flex justify-end gap-2">
        <button
          onClick={() => onDone(false)}
          className="text-sm px-3 py-2 text-muted hover:text-ink"
        >
          {opts.cancelLabel ?? "Cancel"}
        </button>
        <button
          ref={confirmRef}
          onClick={() => onDone(true)}
          className={[
            "text-sm px-4 py-2 rounded font-medium hover:opacity-90",
            opts.danger
              ? "bg-danger text-white"
              : "bg-accent text-accent-fg",
          ].join(" ")}
        >
          {opts.confirmLabel ?? "Confirm"}
        </button>
      </div>
    </Modal>
  );
}

function PromptDialog({
  opts,
  onDone,
}: {
  opts: PromptOpts;
  onDone: (v: string | null) => void;
}) {
  const [value, setValue] = useState(opts.defaultValue ?? "");
  const submit = useCallback(() => onDone(value), [value, onDone]);
  return (
    <Modal title={opts.title} onClose={() => onDone(null)}>
      {opts.body && (
        <div className="text-sm text-muted mb-3 whitespace-pre-wrap">
          {opts.body}
        </div>
      )}
      <input
        autoFocus
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit();
          if (e.key === "Escape") onDone(null);
        }}
        placeholder={opts.placeholder}
        className="w-full text-sm border border-rule rounded px-3 py-2 focus:outline-none focus:border-accent bg-card text-ink"
      />
      <div className="flex justify-end gap-2 mt-3">
        <button
          onClick={() => onDone(null)}
          className="text-sm px-3 py-2 text-muted hover:text-ink"
        >
          {opts.cancelLabel ?? "Cancel"}
        </button>
        <button
          onClick={submit}
          disabled={!value.trim()}
          className="text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
        >
          {opts.confirmLabel ?? "OK"}
        </button>
      </div>
    </Modal>
  );
}
