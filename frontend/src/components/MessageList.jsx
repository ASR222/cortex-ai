import { useEffect, useRef, useState } from "react";
import { useDispatch, useSelector } from "react-redux";
import { motion, AnimatePresence } from "framer-motion";
import MessageBubble from "./MessageBubble";
import { setDraft } from "../redux/message.slice";
import { setAgent } from "../redux/ui.slice";
import { AGENT_LABELS, CREDIT_COST, SPECIALISTS } from "../utils/agents";

const THINKING_LABELS = ["Thinking", "Reading your message", "Choosing a specialist", "Writing"];

/** The agent's live status ("Searching the web"), or a cycling label until it sends one. */
function GeneratingIndicator({ status, agent }) {
  const [labelIndex, setLabelIndex] = useState(0);
  const [slow, setSlow] = useState(false);

  useEffect(() => {
    if (status) return undefined;
    const interval = setInterval(() => setLabelIndex((p) => (p + 1) % THINKING_LABELS.length), 1800);
    return () => clearInterval(interval);
  }, [status]);

  // Services scale to zero when idle, so the first request after a quiet
  // period waits for containers to start. Say so instead of looking stuck.
  useEffect(() => {
    const timer = setTimeout(() => setSlow(true), 6000);
    return () => clearTimeout(timer);
  }, []);

  const label =
    status || (slow ? "Waking up the servers; the first request after a while takes a few seconds" : THINKING_LABELS[labelIndex]);

  return (
    <div className="flex items-center gap-2.5 h-6 text-[13px]" aria-live="polite">
      <motion.span
        className="w-1.5 h-1.5 rounded-full bg-accent shrink-0"
        animate={{ opacity: [1, 0.25, 1] }}
        transition={{ duration: 1.4, repeat: Infinity, ease: "easeInOut" }}
      />
      {agent && <span className="text-accent">{AGENT_LABELS[agent] || agent}</span>}
      <AnimatePresence mode="wait">
        <motion.span
          key={label}
          className="text-muted"
          initial={{ opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -4 }}
          transition={{ duration: 0.2, ease: "easeOut" }}
        >
          {label}…
        </motion.span>
      </AnimatePresence>
    </div>
  );
}

/** The first screen of a new chat: what each specialist does and what it costs. */
function Specialists() {
  const dispatch = useDispatch();
  const { userData } = useSelector((state) => state.user);

  const tryPrompt = (id, prompt) => {
    dispatch(setAgent(id));
    dispatch(setDraft(prompt));
    document.getElementById("composer")?.focus();
  };

  return (
    <div className="max-w-[760px] mx-auto px-5 md:px-6 pt-8 md:pt-10 pb-6">
      <h1 className="text-[24px] font-semibold tracking-[-0.01em]">What should we work on?</h1>
      <p className="mt-2 text-[15px] leading-[24px] text-muted">
        {userData ? (
          <>
            You have <span className="num text-ink">{userData.credits ?? 0}</span> credits. An answer costs 1 to 5, and
            failed answers are refunded.
          </>
        ) : (
          "Each message goes to the specialist that fits it."
        )}
      </p>

      <section aria-labelledby="specialists" className="mt-7">
        <div className="flex items-baseline justify-between px-3 pb-2">
          <h2 id="specialists" className="text-[16px] font-medium">
            Specialists
          </h2>
          <span className="text-[12px] text-faint">Credits per answer</span>
        </div>
        <ul>
          {SPECIALISTS.map(({ id, does, prompt }) => {
            const body = (
              <>
                <span className="hidden sm:block w-[132px] shrink-0 text-[14px] font-medium">{AGENT_LABELS[id]}</span>
                <span className="flex-1 min-w-0">
                  <span className="sm:hidden block text-[14px] font-medium">{AGENT_LABELS[id]}</span>
                  <span className="block text-[14px] text-muted sm:truncate">{does}</span>
                  <span className="block text-[13px] text-faint sm:truncate">
                    {prompt ? `“${prompt}”` : `Runs when you attach ${id === "rag" ? "a PDF" : "an image"}`}
                  </span>
                </span>
                <span className="num text-[14px] w-6 text-right shrink-0 self-start sm:self-center">{CREDIT_COST[id]}</span>
              </>
            );
            const row = "w-full min-h-[52px] flex items-center gap-4 px-3 py-2 rounded-2xl text-left";
            return (
              <li key={id}>
                {prompt ? (
                  <button
                    onClick={() => tryPrompt(id, prompt)}
                    className={`${row} hover:bg-surface active:bg-hover transition-colors cursor-pointer`}
                  >
                    {body}
                  </button>
                ) : (
                  <div className={row}>{body}</div>
                )}
              </li>
            );
          })}
        </ul>
      </section>
    </div>
  );
}

export default function MessageList() {
  const bottomRef = useRef(null);
  const { messages, isLoading, status } = useSelector((state) => state.message);
  const lastContentLength = messages[messages.length - 1]?.content?.length || 0;

  useEffect(() => {
    requestAnimationFrame(() => bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" }));
  }, [messages.length, isLoading, lastContentLength]);

  if (messages.length === 0 && !isLoading) {
    return (
      <div className="flex-1 overflow-y-auto">
        <Specialists />
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto" aria-busy={isLoading}>
      <div className="max-w-[760px] mx-auto px-5 md:px-6 py-8 space-y-8">
        {messages.map((msg, i) => {
          const streaming = msg.pending && i === messages.length - 1;
          if (streaming && !msg.content) {
            return <GeneratingIndicator key={msg._id || i} status={status} agent={msg.agent} />;
          }
          return (
            <motion.div
              key={msg._id || i}
              initial={{ opacity: 0, y: 6 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.2, ease: "easeOut" }}
            >
              <MessageBubble message={msg} streaming={streaming} />
              {streaming && status && <p className="mt-2 text-[13px] text-muted">{status}…</p>}
            </motion.div>
          );
        })}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
