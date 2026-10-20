import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowUp, ChevronDown, Paperclip, Square, FileText, X, Mic, MicOff } from "lucide-react";
import { useDispatch, useSelector } from "react-redux";
import {
  addMessage,
  appendToLast,
  patchLast,
  pushToLast,
  setArtifacts,
  setDraft,
  setIsLoading,
  setStatus,
} from "../redux/message.slice";
import { streamPrompt } from "../features/agent.api";
import { createConversation } from "../features/conversation.api";
import { addConversation, setConvTitle, setSelectedConversation } from "../redux/conversation.slice";
import { setCredits } from "../redux/user.slice";
import { setAgent } from "../redux/ui.slice";
import { PICKABLE } from "../utils/agents";
import { errorInfo } from "../utils/axios";

const MAX_FILE_MB = 20;

const PLACEHOLDERS = {
  auto: "Ask CortexAI anything, or attach a PDF or image…",
  chat: "Chat with CortexAI…",
  coding: "Describe the app, website or code you need…",
  pdf: "Generate a PDF report about…",
  ppt: "Create a slide deck about…",
  image: "Describe the image to generate…",
  search: "Search the web for…",
};

const titleFrom = (text) => {
  const t = text.split(/\s+/).join(" ").trim();
  return t.length > 60 ? `${t.slice(0, 60)}…` : t;
};

export default function ChatInput({ setBanner }) {
  const [isListening, setIsListening] = useState(false);
  const [selectedFile, setSelectedFile] = useState(null);

  const recognitionRef = useRef(null);
  const fileRef = useRef(null);
  const textRef = useRef(null);
  const abortRef = useRef(null);

  const dispatch = useDispatch();
  const { selectedConversation } = useSelector((state) => state.conversation);
  const selectedAgent = useSelector((state) => state.ui.agent);
  // The input text lives in Redux so the empty-state suggestion chips can fill it.
  const { isLoading, draft: value } = useSelector((state) => state.message);
  const setValue = (text) => dispatch(setDraft(text));

  // The box grows with the message, up to about ten lines, then scrolls.
  useLayoutEffect(() => {
    const el = textRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 264)}px`;
  }, [value]);

  const previewUrl = useMemo(
    () => (selectedFile?.type.startsWith("image/") ? URL.createObjectURL(selectedFile) : null),
    [selectedFile],
  );
  useEffect(() => () => previewUrl && URL.revokeObjectURL(previewUrl), [previewUrl]);

  useEffect(() => {
    const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!SpeechRecognition) return;
    const recognition = new SpeechRecognition();
    recognition.lang = "en-IN";
    recognition.interimResults = true;
    recognition.continuous = true;
    recognition.onresult = (event) => {
      let transcript = "";
      for (let i = event.resultIndex; i < event.results.length; i++) {
        transcript += event.results[i][0].transcript;
      }
      dispatch(setDraft(transcript));
    };
    recognition.onend = () => setIsListening(false);
    recognitionRef.current = recognition;
  }, [dispatch]);

  const toggleMic = () => {
    if (!recognitionRef.current) {
      setBanner({ open: true, title: "Not supported", message: "Voice input isn't available in this browser." });
      return;
    }
    if (isListening) recognitionRef.current.stop();
    else recognitionRef.current.start();
    setIsListening(!isListening);
  };

  const clearFile = () => {
    setSelectedFile(null);
    if (fileRef.current) fileRef.current.value = "";
  };

  const pickFile = (file) => {
    if (!file) return;
    if (file.size > MAX_FILE_MB * 1024 * 1024) {
      setBanner({ open: true, title: "File too large", message: `Files are limited to ${MAX_FILE_MB} MB.` });
      return;
    }
    setSelectedFile(file);
  };

  const showError = (title, message) => setBanner({ open: true, title, message });

  const handleEvent = (event, data) => {
    switch (event) {
      case "meta":
        dispatch(patchLast({ agent: data.agent, cost: data.cost }));
        break;
      case "status":
        dispatch(setStatus(data.message));
        break;
      case "token":
        dispatch(setStatus(""));
        dispatch(appendToLast(data.text));
        break;
      case "sources":
        dispatch(patchLast({ sources: data.sources, images: data.images }));
        break;
      case "artifact":
        dispatch(pushToLast({ field: "artifacts", value: data.artifact }));
        dispatch(setArtifacts([data.artifact]));
        break;
      case "attachment":
        dispatch(pushToLast({ field: "attachments", value: data.attachment }));
        break;
      case "done":
        dispatch(patchLast({ pending: false, _id: data.messageId }));
        dispatch(setCredits(data.credits));
        break;
      case "error":
        dispatch(patchLast({ pending: false, error: data.message }));
        dispatch(setCredits(data.credits));
        showError(data.title || "Something went wrong", data.message || "Please try again.");
        break;
      default:
    }
  };

  const handleSend = async () => {
    if (isLoading) {
      abortRef.current?.abort();
      return;
    }
    const prompt = value.trim();
    if (!prompt && !selectedFile) return;

    const file = selectedFile;
    dispatch(setIsLoading(true));
    dispatch(setStatus(""));
    setValue("");
    clearFile();

    try {
      let conversation = selectedConversation;
      if (!conversation) {
        conversation = await createConversation();
        dispatch(addConversation(conversation));
        dispatch(setSelectedConversation(conversation));
      }

      dispatch(
        addMessage({
          role: "user",
          content: prompt,
          attachments: file
            ? [
                {
                  name: file.name,
                  mime: file.type,
                  kind: "upload",
                  localUrl: file.type.startsWith("image/") ? URL.createObjectURL(file) : null,
                },
              ]
            : [],
        }),
      );
      dispatch(addMessage({ role: "assistant", content: "", pending: true }));

      const form = new FormData();
      form.append("conversationId", conversation._id);
      form.append("prompt", prompt);
      form.append("agent", selectedAgent);
      if (file) form.append("file", file);

      const controller = new AbortController();
      abortRef.current = controller;
      await streamPrompt(form, handleEvent, controller.signal);

      if (conversation.title === "New Chat" && prompt) {
        dispatch(setConvTitle({ conversationId: conversation._id, title: titleFrom(prompt) }));
      }
    } catch (error) {
      if (error.name === "AbortError") {
        dispatch(patchLast({ pending: false, stopped: true }));
      } else {
        dispatch(patchLast({ pending: false, error: "Request failed." }));
        const { title, message } = errorInfo(error);
        showError(title, message);
      }
    } finally {
      abortRef.current = null;
      dispatch(setIsLoading(false));
      dispatch(setStatus(""));
    }
  };

  const onKeyDown = (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      handleSend();
    }
  };

  const canSend = isLoading || value.trim() || selectedFile;

  const iconButton =
    "flex items-center justify-center w-9 h-9 pointer-coarse:w-11 pointer-coarse:h-11 rounded-full text-faint hover:text-ink hover:bg-hover transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed";

  return (
    <div className="w-full shrink-0 px-3 md:px-6 pt-2 pb-[max(12px,env(safe-area-inset-bottom))]">
      <div className="max-w-[760px] mx-auto">
        <div className="@container flex flex-col gap-2 bg-surface rounded-[24px] px-3 pt-3 pb-2.5">
          {/* Narrow composers pick the specialist from a menu; wider ones show every option. */}
          <label className="@min-[480px]:hidden relative self-start">
            <span className="sr-only">Specialist</span>
            <select
              value={selectedAgent}
              onChange={(e) => dispatch(setAgent(e.target.value))}
              className="appearance-none h-8 pointer-coarse:h-11 pl-3 pr-8 rounded-full bg-raised text-[13px] text-ink cursor-pointer outline-none"
            >
              {PICKABLE.map(({ id, label }) => (
                <option key={id} value={id}>
                  {label}
                </option>
              ))}
            </select>
            <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted" />
          </label>
          <div role="radiogroup" aria-label="Specialist" className="hidden @min-[480px]:flex gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            {PICKABLE.map(({ id, icon: Icon, label }) => {
              const active = selectedAgent === id;
              return (
                <button
                  key={id}
                  role="radio"
                  aria-checked={active}
                  onClick={() => dispatch(setAgent(id))}
                  className={`shrink-0 inline-flex items-center gap-1.5 h-8 px-3 rounded-full text-[13px] transition-colors cursor-pointer
                    ${active ? "bg-raised text-ink" : "text-muted hover:text-ink hover:bg-hover"}`}
                >
                  <Icon size={14} strokeWidth={1.6} className={`@max-[600px]:hidden ${active ? "text-ink" : "text-faint"}`} />
                  {label}
                </button>
              );
            })}
          </div>

          {selectedFile && (
            <div className="px-1">
              <div className="inline-flex items-center gap-2.5 rounded-2xl bg-hover pl-2 pr-1.5 py-1.5">
                {previewUrl ? (
                  <img src={previewUrl} alt="" className="h-9 w-9 rounded-xl object-cover" />
                ) : (
                  <span className="h-9 w-9 flex items-center justify-center">
                    <FileText size={16} strokeWidth={1.6} className="text-muted" />
                  </span>
                )}
                <div className="min-w-0">
                  <p className="text-[13px] max-w-[220px] truncate">{selectedFile.name}</p>
                  <p className="num text-[12px] text-faint">{Math.ceil(selectedFile.size / 1024).toLocaleString()} KB</p>
                </div>
                <button onClick={clearFile} className={iconButton} aria-label="Remove file">
                  <X size={15} />
                </button>
              </div>
            </div>
          )}

          <textarea
            id="composer"
            ref={textRef}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder={PLACEHOLDERS[selectedAgent]}
            aria-label="Message"
            rows={2}
            maxLength={8000}
            disabled={isLoading}
            className="w-full px-2 bg-transparent outline-none resize-none text-[15px] leading-[24px] placeholder:text-faint [scrollbar-width:thin] disabled:opacity-50"
          />

          <div className="flex items-center justify-between">
            <div className="flex items-center gap-0.5">
              <input
                ref={fileRef}
                type="file"
                hidden
                accept=".pdf,image/png,image/jpeg,image/webp,image/gif"
                onChange={(e) => pickFile(e.target.files[0])}
              />
              <button
                title="Attach a PDF or image"
                aria-label="Attach a PDF or image"
                className={iconButton}
                onClick={() => fileRef.current.click()}
                disabled={isLoading}
              >
                <Paperclip size={16} strokeWidth={1.6} />
              </button>
              <button
                onClick={toggleMic}
                title={isListening ? "Stop voice input" : "Voice input"}
                aria-label={isListening ? "Stop voice input" : "Voice input"}
                aria-pressed={isListening}
                className={isListening ? `${iconButton} !bg-negative-bg !text-negative` : iconButton}
              >
                {isListening ? <MicOff size={16} strokeWidth={1.6} /> : <Mic size={16} strokeWidth={1.6} />}
              </button>
            </div>

            <button
              onClick={handleSend}
              disabled={!canSend}
              title={isLoading ? "Stop" : "Send"}
              aria-label={isLoading ? "Stop" : "Send"}
              className="flex items-center justify-center w-9 h-9 pointer-coarse:w-11 pointer-coarse:h-11 rounded-full bg-ink text-ground hover:bg-soft active:scale-95 transition cursor-pointer disabled:bg-raised disabled:text-faint disabled:cursor-not-allowed"
            >
              {isLoading ? <Square size={12} fill="currentColor" /> : <ArrowUp size={18} strokeWidth={2} />}
            </button>
          </div>
        </div>

        <p className="text-center text-[12px] text-faint mt-2">CortexAI can make mistakes. Check important facts.</p>
      </div>
    </div>
  );
}
