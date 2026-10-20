import { Code2, Menu } from "lucide-react";
import { useDispatch, useSelector } from "react-redux";
import { setBillingOpen, setCodeOpen, setNavOpen } from "../redux/ui.slice";

/** Credits left at their true ratio, both ends of the track named. Opens billing. */
function CreditMeter({ user }) {
  const dispatch = useDispatch();
  const credits = user.credits ?? 0;
  const total = Math.max(user.totalCredits ?? 0, credits, 1);
  const low = credits <= 10;
  return (
    <button
      data-open-billing
      onClick={() => dispatch(setBillingOpen(true))}
      title="Credits and plans"
      className="h-full flex items-center gap-3 pl-4 pr-1 cursor-pointer group"
    >
      <span className="text-[13px] text-muted group-hover:text-ink transition-colors whitespace-nowrap">
        <span className={`num font-medium ${low ? "text-warning" : "text-ink"}`}>{credits}</span>
        <span className="hidden sm:inline"> of </span>
        <span className="hidden sm:inline num">{total}</span> credits
      </span>
      <span className="hidden md:block w-16 h-1.5 rounded-full bg-raised overflow-hidden" aria-hidden="true">
        <span className="block h-full rounded-full bg-accent" style={{ width: `${(credits / total) * 100}%` }} />
      </span>
    </button>
  );
}

export default function Navbar() {
  const dispatch = useDispatch();
  const { selectedConversation } = useSelector((state) => state.conversation);
  const { messages, artifacts } = useSelector((state) => state.message);
  const { userData } = useSelector((state) => state.user);
  const hasCode = artifacts?.length > 0;

  return (
    <header className="h-14 shrink-0 flex items-center gap-2 pl-3 lg:pl-6 pr-3 lg:pr-5 border-b border-rule">
      <button
        onClick={() => dispatch(setNavOpen(true))}
        aria-label="Open chats"
        className="lg:hidden flex items-center justify-center w-9 h-9 pointer-coarse:w-11 pointer-coarse:h-11 rounded-full text-muted hover:text-ink hover:bg-surface cursor-pointer"
      >
        <Menu size={18} strokeWidth={1.6} />
      </button>

      <div className="flex-1 min-w-0 flex items-baseline gap-2.5">
        {selectedConversation ? (
          <>
            <h1 className="text-[15px] font-medium truncate">{selectedConversation.title}</h1>
            <span className="num text-[12px] text-faint whitespace-nowrap hidden sm:inline">
              {messages.length} {messages.length === 1 ? "message" : "messages"}
            </span>
          </>
        ) : (
          <p className="text-[15px] font-medium">New chat</p>
        )}
      </div>

      {hasCode && (
        <button
          onClick={() => dispatch(setCodeOpen(true))}
          className="lg:hidden h-8 pointer-coarse:h-11 flex items-center gap-1.5 px-3 rounded-full bg-surface hover:bg-hover text-[13px] cursor-pointer"
        >
          <Code2 size={14} strokeWidth={1.6} className="text-faint" />
          Code
        </button>
      )}

      {userData && (
        <div className="h-full flex items-center border-l border-rule ml-1">
          <CreditMeter user={userData} />
        </div>
      )}
    </header>
  );
}
