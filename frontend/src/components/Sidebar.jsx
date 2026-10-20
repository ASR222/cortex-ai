import { useEffect, useState } from "react";
import { Plus, LogOut, PanelLeftClose, PanelLeftOpen, X, Trash2 } from "lucide-react";
import { useDispatch, useSelector } from "react-redux";
import { setUserData } from "../redux/user.slice";
import { deleteConversation, getConversations } from "../features/conversation.api";
import { removeConversation, setConversations, setSelectedConversation } from "../redux/conversation.slice";
import { getMessages } from "../features/message.api";
import { setArtifacts, setMessages } from "../redux/message.slice";
import { setBillingOpen, setNavOpen } from "../redux/ui.slice";
import { logout as logoutRequest } from "../features/user.api";
import BillingDrawer from "./BillingDrawer";
import ConfirmDialog from "./ConfirmDialog";
import useDialog from "../hooks/useDialog";
import Mark from "./Mark";

const planName = (plan = "free") => plan.charAt(0).toUpperCase() + plan.slice(1);

const iconButton =
  "flex items-center justify-center w-8 h-8 pointer-coarse:w-11 pointer-coarse:h-11 rounded-full text-faint hover:text-ink hover:bg-hover transition-colors cursor-pointer";

function Avatar({ user }) {
  const [broken, setBroken] = useState(false);
  if (user.avatar && !broken) {
    return (
      <img
        src={user.avatar}
        alt=""
        onError={() => setBroken(true)}
        className="w-8 h-8 rounded-full object-cover shrink-0"
      />
    );
  }
  return (
    <span className="w-8 h-8 rounded-full bg-raised text-[13px] font-medium flex items-center justify-center shrink-0">
      {user.name?.[0]?.toUpperCase() || "?"}
    </span>
  );
}

export default function Sidebar() {
  const [collapsed, setCollapsed] = useState(false);
  const [pendingDelete, setPendingDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);
  const { userData } = useSelector((state) => state.user);
  const { conversations, selectedConversation } = useSelector((state) => state.conversation);
  const { isLoading } = useSelector((state) => state.message);
  const { navOpen, billingOpen } = useSelector((state) => state.ui);
  const dispatch = useDispatch();
  const closeNav = () => dispatch(setNavOpen(false));
  const navRef = useDialog(navOpen, closeNav);

  const resetChat = () => {
    dispatch(setSelectedConversation(null));
    dispatch(setMessages([]));
    dispatch(setArtifacts([]));
  };

  const logout = async () => {
    try {
      await logoutRequest();
    } finally {
      resetChat();
      dispatch(setConversations([]));
      dispatch(setUserData(null));
    }
  };

  useEffect(() => {
    if (!userData?._id) return;
    getConversations()
      .then((data) => dispatch(setConversations(data)))
      .catch(() => {});
  }, [userData?._id, dispatch]);

  const handleCreateConversation = () => {
    if (isLoading) return;
    resetChat();
    closeNav();
  };

  const handleSelectConversation = async (conversation) => {
    if (isLoading || conversation._id === selectedConversation?._id) return;
    closeNav();
    dispatch(setSelectedConversation(conversation));
    dispatch(setMessages([]));
    dispatch(setArtifacts([]));
    const messages = await getMessages(conversation._id);
    dispatch(setMessages(messages));
    const withArtifacts = [...messages].reverse().find((m) => m.artifacts?.length);
    dispatch(setArtifacts(withArtifacts?.artifacts || []));
  };

  const askDelete = (e, conversation) => {
    e.stopPropagation();
    if (!isLoading) setPendingDelete(conversation);
  };

  const confirmDelete = async () => {
    const conversation = pendingDelete;
    setDeleting(true);
    try {
      await deleteConversation(conversation._id);
      if (selectedConversation?._id === conversation._id) resetChat();
      dispatch(removeConversation(conversation._id));
      setPendingDelete(null);
    } finally {
      setDeleting(false);
    }
  };

  const billing = (
    <>
      <BillingDrawer open={billingOpen} onClose={() => dispatch(setBillingOpen(false))} />
      {pendingDelete && (
        <ConfirmDialog
          title="Delete this chat?"
          body={`“${pendingDelete.title}” and any files it created will be deleted. This can't be undone.`}
          confirmLabel="Delete chat"
          busy={deleting}
          onConfirm={confirmDelete}
          onCancel={() => setPendingDelete(null)}
        />
      )}
    </>
  );

  /* Collapsed rail, desktop only: the mark, expand and new chat. */
  if (collapsed) {
    return (
      <>
        <nav
          aria-label="Chats"
          className="hidden lg:flex flex-col items-center w-14 h-full shrink-0 border-r border-rule py-3 gap-2"
        >
          <span className="h-8 flex items-center">
            <Mark />
          </span>
          <button onClick={() => setCollapsed(false)} title="Show chats" aria-label="Show chats" className={iconButton}>
            <PanelLeftOpen size={16} strokeWidth={1.6} />
          </button>
          <button onClick={handleCreateConversation} title="New chat" aria-label="New chat" className={iconButton}>
            <Plus size={16} strokeWidth={1.6} />
          </button>
        </nav>
        {billing}
      </>
    );
  }

  return (
    <>
      {navOpen && <div onClick={closeNav} className="lg:hidden fixed inset-0 z-40 bg-black/70" aria-hidden="true" />}

      <nav
        ref={navRef}
        aria-label="Chats"
        role={navOpen ? "dialog" : undefined}
        aria-modal={navOpen || undefined}
        tabIndex={-1}
        className={`outline-none fixed lg:static inset-y-0 left-0 z-50 w-[min(320px,86vw)] lg:w-[232px] h-full shrink-0
          flex flex-col bg-ground pb-[env(safe-area-inset-bottom)] lg:border-r lg:border-rule transition-transform duration-200
          ${navOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0"}`}
      >
        {/* Product */}
        <div className="h-14 shrink-0 flex items-center gap-2.5 pl-4 pr-3">
          <Mark />
          <span className="text-[16px] font-semibold tracking-[-0.01em] flex-1">CortexAI</span>
          <button
            onClick={() => setCollapsed(true)}
            title="Hide chats"
            aria-label="Hide chats"
            className={`hidden lg:flex ${iconButton}`}
          >
            <PanelLeftClose size={16} strokeWidth={1.6} />
          </button>
          <button onClick={closeNav} aria-label="Close menu" className={`lg:hidden ${iconButton}`}>
            <X size={18} strokeWidth={1.6} />
          </button>
        </div>

        <div className="px-3 pt-1 pb-3">
          <button
            onClick={handleCreateConversation}
            className="w-full h-9 pointer-coarse:h-11 flex items-center gap-2 px-3.5 rounded-full bg-surface hover:bg-hover text-[14px] font-medium cursor-pointer transition-colors"
          >
            <Plus size={16} strokeWidth={1.6} className="text-faint" />
            New chat
          </button>
        </div>

        <p className="px-4 pt-2 pb-1.5 text-[12px] text-faint">Chats</p>

        <div className="flex-1 overflow-y-auto px-2 pb-3 [scrollbar-width:thin]">
          {conversations.length === 0 && (
            <p className="px-2 py-2 text-[13px] text-faint">Your chats will appear here.</p>
          )}
          {conversations.map((chat) => {
            const isActive = selectedConversation?._id === chat._id;
            return (
              <div key={chat._id} className="group relative">
                <button
                  data-conversation
                  onClick={() => handleSelectConversation(chat)}
                  aria-current={isActive ? "page" : undefined}
                  className={`w-full h-[34px] pointer-coarse:h-11 flex items-center pl-2.5 pr-9 rounded-full text-left text-[14px] truncate cursor-pointer transition-colors hover:bg-surface
                    ${isActive ? "text-ink font-medium" : "text-soft"}`}
                >
                  <span className="truncate">{chat.title}</span>
                </button>
                <button
                  onClick={(e) => askDelete(e, chat)}
                  title="Delete chat"
                  aria-label={`Delete ${chat.title}`}
                  className={`absolute right-1 top-1/2 -translate-y-1/2 w-7 h-7 pointer-coarse:w-10 pointer-coarse:h-10 rounded-full flex items-center justify-center text-faint hover:text-negative cursor-pointer transition-opacity
                    ${isActive ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus:opacity-100"}`}
                >
                  <Trash2 size={14} strokeWidth={1.6} />
                </button>
              </div>
            );
          })}
        </div>

        {/* Account */}
        {userData && (
          <div className="shrink-0 border-t border-rule px-3 py-3 flex items-center gap-2.5">
            <Avatar user={userData} />
            <div className="flex-1 min-w-0">
              <p className="text-[14px] font-medium truncate">{userData.name}</p>
              <p className="text-[12px] text-faint">{planName(userData.plan)} plan</p>
            </div>
            <button onClick={logout} title="Sign out" aria-label="Sign out" className={iconButton}>
              <LogOut size={15} strokeWidth={1.6} />
            </button>
          </div>
        )}
      </nav>

      {billing}
    </>
  );
}
