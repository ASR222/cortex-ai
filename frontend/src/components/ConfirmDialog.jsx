import useDialog from "../hooks/useDialog";

/** An in-app confirmation for destructive actions; names the object and the consequence. */
export default function ConfirmDialog({ title, body, confirmLabel, busy, onConfirm, onCancel }) {
  const ref = useDialog(true, busy ? null : onCancel);
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 px-4" onClick={busy ? undefined : onCancel}>
      <div
        ref={ref}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        aria-describedby="confirm-body"
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-[400px] rounded-[20px] bg-surface p-6 outline-none"
      >
        <h2 id="confirm-title" className="text-[16px] font-medium">
          {title}
        </h2>
        <p id="confirm-body" className="mt-2 text-[14px] leading-[22px] text-muted">
          {body}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            onClick={onCancel}
            disabled={busy}
            className="h-9 pointer-coarse:h-11 px-4 rounded-full text-[14px] text-muted hover:text-ink hover:bg-hover cursor-pointer transition-colors disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={busy}
            className="h-9 pointer-coarse:h-11 px-4 rounded-full border border-negative text-[14px] text-negative hover:bg-negative-bg cursor-pointer transition-colors disabled:opacity-50 disabled:cursor-wait"
          >
            {busy ? "Deleting…" : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
