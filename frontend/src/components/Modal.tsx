// In-app modal + a confirm() replacement that matches the app's theme.
//
// The native window.confirm() dialog pops up an OS-chrome window labelled
// "Сообщение с wails.localhost" and breaks the immersion of the rest of
// the UI. <Modal> renders an overlay portal: dimmed + blurred backdrop,
// centered card, ESC / backdrop-click to dismiss, Enter to confirm.
//
// Usage:
//
//   const ok = await confirmModal({
//     title: "Восстановить сейв?",
//     body: "Текущее состояние будет автоматически забэкаплено.",
//     confirmLabel: "Восстановить",
//     variant: "danger",
//   });

import { createPortal } from "react-dom";
import { createRoot, type Root } from "react-dom/client";
import { useEffect, useRef, type ReactNode } from "react";

export function Modal({
  open,
  title,
  onClose,
  children,
  footer,
}: {
  open: boolean;
  title?: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  // ESC closes; lock body scroll while open. Both are pure-DOM side effects
  // so they live in one effect tied to the open flag.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-[1000] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="card w-full max-w-md p-5 shadow-[0_20px_60px_rgba(0,0,0,0.5)]"
        onClick={(e) => e.stopPropagation()}
      >
        {title && (
          <div className="mb-3 text-base font-semibold text-gray-100">{title}</div>
        )}
        <div className="text-sm text-gray-300">{children}</div>
        {footer && <div className="mt-5 flex justify-end gap-2">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

// ── confirmModal: imperative API ───────────────────────────────────────
// Mounts a tiny self-contained React root, returns a Promise<boolean>,
// tears down when the user makes a choice. This lets non-component code
// (e.g. event handlers that previously called window.confirm()) await a
// styled dialog without each call site having to manage open-state.

type ConfirmOpts = {
  title?: string;
  body: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** "primary" → accent button. "danger" → red button. Default "primary". */
  variant?: "primary" | "danger";
};

export function confirmModal(opts: ConfirmOpts): Promise<boolean> {
  return new Promise((resolve) => {
    const mount = document.createElement("div");
    document.body.appendChild(mount);
    const root: Root = createRoot(mount);

    const cleanup = (answer: boolean) => {
      root.unmount();
      mount.remove();
      resolve(answer);
    };

    root.render(<ConfirmHost opts={opts} onAnswer={cleanup} />);
  });
}

function ConfirmHost({ opts, onAnswer }: { opts: ConfirmOpts; onAnswer: (b: boolean) => void }) {
  const okRef = useRef<HTMLButtonElement | null>(null);

  // Autofocus the confirm button so Enter immediately confirms, matching
  // native confirm() ergonomics.
  useEffect(() => { okRef.current?.focus(); }, []);

  const variantClass = opts.variant === "danger"
    ? "bg-red-600 text-white border-transparent hover:bg-red-500"
    : "";

  return (
    <Modal
      open
      title={opts.title}
      onClose={() => onAnswer(false)}
      footer={
        <>
          <button className="btn" onClick={() => onAnswer(false)}>
            {opts.cancelLabel ?? "Отмена"}
          </button>
          <button
            ref={okRef}
            className={`btn btn-primary ${variantClass}`}
            onClick={() => onAnswer(true)}
          >
            {opts.confirmLabel ?? "OK"}
          </button>
        </>
      }
    >
      {/* whitespace-pre-line so callers can keep "\n" in plain strings */}
      <div className="whitespace-pre-line leading-relaxed">{opts.body}</div>
    </Modal>
  );
}
