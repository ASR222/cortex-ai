import { useEffect, useRef, useState } from "react";
import { useDispatch, useSelector } from "react-redux";
import { motion, AnimatePresence } from "framer-motion";
import MessageBubble from "./MessageBubble";
import { setDraft } from "../redux/message.slice";

function NeuralPulse() {
  return (
    <div className="relative w-9 h-9 flex items-center justify-center shrink-0">
      {[0, 0.45, 0.9].map((delay, i) => (
        <motion.span
          key={i}
          className="absolute inset-0 rounded-full border border-cyan-400/30"
          initial={{ scale: 0.3, opacity: 0.55 }}
          animate={{ scale: 1.7, opacity: 0 }}
          transition={{ duration: 1.8, repeat: Infinity, delay, ease: "easeOut" }}
        />
      ))}
      <motion.span
        className="w-2.5 h-2.5 rounded-full bg-gradient-to-br from-cyan-300 to-violet-400"
        style={{ boxShadow: "0 0 14px rgba(125,211,252,0.55)" }}
        animate={{ scale: [1, 1.25, 1] }}
        transition={{ duration: 1.6, repeat: Infinity, ease: "easeInOut" }}
      />
    </div>
  );
}

const THINKING_LABELS = ["Thinking", "Analyzing", "Reasoning", "Generating"];

/** Shows the agent's live status ("Searching the web") or a cycling label. */
function GeneratingIndicator({ status }) {
  const [labelIndex, setLabelIndex] = useState(0);

  useEffect(() => {
    if (status) return undefined;
    const interval = setInterval(() => setLabelIndex((p) => (p + 1) % THINKING_LABELS.length), 1800);
    return () => clearInterval(interval);
  }, [status]);

  const label = status || THINKING_LABELS[labelIndex];

  return (
    <div className="flex items-center gap-3 py-1">
      <NeuralPulse />
      <AnimatePresence mode="wait">
        <motion.span
          key={label}
          className="text-[13px] font-medium tracking-wide text-slate-400"
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -6 }}
          transition={{ duration: 0.25, ease: "easeOut" }}
        >
          {label}…
        </motion.span>
      </AnimatePresence>
    </div>
  );
}

const SUGGESTIONS = [
  "Build a landing page for a coffee shop",
  "What's new in AI this week?",
  "Create a presentation on climate tech",
  "Explain how Redis persistence works",
];

export default function MessageList() {
  const bottomRef = useRef(null);
  const dispatch = useDispatch();
  const { messages, isLoading, status } = useSelector((state) => state.message);
  const lastContentLength = messages[messages.length - 1]?.content?.length || 0;

  useEffect(() => {
    requestAnimationFrame(() => bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" }));
  }, [messages.length, isLoading, lastContentLength]);

  if (messages.length === 0 && !isLoading) {
    return (
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="h-full flex flex-col items-center justify-center gap-4 text-center">
          <div className="flex flex-col gap-1.5">
            <h1 className="text-[20px] font-semibold text-slate-200 tracking-tight">CortexAI</h1>
            <h3 className="text-[15px] font-semibold text-slate-400 tracking-tight">How can I help you?</h3>
            <p className="text-[13px] text-slate-600 max-w-[300px] leading-relaxed">
              Chat, search the web, write code, ask questions about a PDF, or generate documents, slides and images.
            </p>
          </div>
          <div className="flex flex-wrap justify-center gap-2 mt-1 max-w-[560px]">
            {SUGGESTIONS.map((s) => (
              <button
                key={s}
                onClick={() => dispatch(setDraft(s))}
                className="text-[12px] text-slate-400 bg-white/[0.04] border border-white/[0.07] px-3.5 py-1.5 rounded-lg hover:bg-white/[0.08] hover:text-slate-200 transition-colors duration-150 cursor-pointer"
              >
                {s}
              </button>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto px-3 md:px-6 py-6 space-y-5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
      {messages.map((msg, i) => {
        const streaming = msg.pending && i === messages.length - 1;
        if (streaming && !msg.content) {
          return <GeneratingIndicator key={msg._id || i} status={status} />;
        }
        return (
          <motion.div
            key={msg._id || i}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.22, ease: "easeOut" }}
          >
            <MessageBubble message={msg} streaming={streaming} />
            {streaming && status && (
              <p className="mt-1 ml-1 text-[12px] text-slate-500 animate-pulse">{status}…</p>
            )}
          </motion.div>
        );
      })}
      <div ref={bottomRef} />
    </div>
  );
}
