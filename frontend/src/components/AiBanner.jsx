import { AnimatePresence, motion } from "framer-motion";
import { AlertTriangle, X } from "lucide-react";
import { useEffect } from "react";

/** A notice that floats over the chat and closes itself after 5 seconds. */
export default function AIBanner({ open, title, message, onClose }) {
  useEffect(() => {
    if (!open) return;
    const timer = setTimeout(() => onClose(), 5000);
    return () => clearTimeout(timer);
  }, [open, message, onClose]);

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          role="alert"
          initial={{ y: -16, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -16, opacity: 0 }}
          transition={{ duration: 0.18 }}
          className="fixed top-4 left-1/2 -translate-x-1/2 z-[999] w-[calc(100%-32px)] max-w-md"
        >
          <div className="flex items-start gap-3 rounded-2xl bg-raised p-4">
            <AlertTriangle size={16} strokeWidth={1.8} className="text-warning shrink-0 mt-0.5" />
            <div className="flex-1 min-w-0">
              <p className="text-[14px] font-medium">{title}</p>
              <p className="mt-0.5 text-[14px] leading-[20px] text-muted">{message}</p>
            </div>
            <button
              onClick={onClose}
              aria-label="Dismiss"
              className="-m-1.5 text-faint hover:text-ink cursor-pointer w-9 h-9 pointer-coarse:w-11 pointer-coarse:h-11 flex items-center justify-center rounded-full"
            >
              <X size={16} />
            </button>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
