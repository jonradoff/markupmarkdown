import { useEffect } from "react";

interface Props {
  title?: string;
  onClose: () => void;
  children: React.ReactNode;
}

export default function Modal({ title, onClose, children }: Props) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div
        className="absolute inset-0 bg-black/40"
        onClick={onClose}
      />
      <div className="relative bg-card border border-rule rounded-lg shadow-xl w-[420px] max-w-[92vw] p-5">
        {title && (
          <h2 className="text-lg font-semibold tracking-tight mb-3">{title}</h2>
        )}
        {children}
      </div>
    </div>
  );
}
