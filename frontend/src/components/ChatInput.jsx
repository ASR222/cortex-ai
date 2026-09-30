import { useEffect, useMemo, useRef, useState } from "react";
import {
  Send,
  Paperclip,
  Square,
  Zap,
  MessageSquare,
  Code2,
  Presentation,
  Image as ImageIcon,
  Globe,
  FileText,
  X,
  Mic,
  MicOff,
} from "lucide-react";
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
import { errorInfo } from "../utils/axios";

const MAX_FILE_MB = 20;

const AGENTS = [
  { id: "auto", icon: Zap, label: "Auto" },
  { id: "chat", icon: MessageSquare, label: "Chat" },
  { id: "search", icon: Globe, label: "Search" },
  { id: "coding", icon: Code2, label: "Coding" },
  { id: "pdf", icon: FileText, label: "PDF" },
  { id: "ppt", icon: Presentation, label: "PPT" },
  { id: "image", icon: ImageIcon, label: "Image" },
];

const PLACEHOLDERS = {
  auto: "Ask CortexAI anything, or attach a PDF or image…",
  chat: "Chat with CortexAI…",
  coding: "Describe the app, website or code you need…",
  pdf: "Generate a PDF report about…",
  ppt: "Create a presentation about…",
  image: "Describe the image to generate…",
  search: "Search the web for…",
};

const titleFrom = (text) => {
  const t = text.split(/\s+/).join(" ").trim();
  return t.length > 60 ? `${t.slice(0, 60)}…` : t;
};

export default function ChatInput({ setBanner }) {
  const [selectedAgent, setSelectedAgent] = useState("auto");
  const [isListening, setIsListening] = useState(false);
  const [selectedFile, setSelectedFile] = useState(null);

  const recognitionRef = useRef(null);
  const fileRef = useRef(null);
  const abortRef = useRef(null);

  const dispatch = useDispatch();
  const { selectedConversation } = useSelector((state) => state.conversation);
  // The input text lives in Redux so the empty-state suggestion chips can fill it.
  const { isLoading, draft: value } = useSelector((state) => state.message);
  const setValue = (text) => dispatch(setDraft(text));

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
        dispatch(patchLast({ agent: data.agent }));
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

  return (
    <div className="w-full overflow-hidden px-3 md:px-5 py-4 border-t border-white/[0.06] bg-[#0d0f14]">
      <div className="flex flex-col gap-2 bg-white/[0.03] border border-white/[0.07] rounded-2xl px-4 pt-3.5 pb-3">
        <div className="flex gap-2 flex-wrap">
          {AGENTS.map(({ id, icon: Icon, label }) => {
            const active = selectedAgent === id;
            return (
              <button
                key={id}
                onClick={() => setSelectedAgent(id)}
                className={`flex-shrink-0 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium border transition-all cursor-pointer
                  ${
                    active
                      ? "bg-gradient-to-r from-indigo-500 to-violet-600 text-white border-transparent shadow-[0_1px_8px_rgba(99,102,241,.35)]"
                      : "bg-white/[0.03] text-slate-400 border-white/[0.06] hover:bg-white/[0.07]"
                  }`}
              >
                <Icon size={13} className={active ? "text-white" : "text-slate-500"} />
                {label}
              </button>
            );
          })}
        </div>

        {selectedFile && (
          <div className="my-2">
            <div className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-2">
              {previewUrl ? (
                <img src={previewUrl} alt="" className="h-10 w-10 rounded-lg object-cover" />
              ) : (
                <FileText size={16} className="text-red-400" />
              )}
              <div>
                <p className="text-xs text-white max-w-[220px] truncate">{selectedFile.name}</p>
                <p className="text-[10px] text-slate-500">{Math.ceil(selectedFile.size / 1024)} KB</p>
              </div>
              <button onClick={clearFile} className="ml-2 cursor-pointer" aria-label="Remove file">
                <X size={14} className="text-slate-500 hover:text-white" />
              </button>
            </div>
          </div>
        )}

        <textarea
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={PLACEHOLDERS[selectedAgent]}
          rows={3}
          maxLength={8000}
          disabled={isLoading}
          className="w-full bg-transparent outline-none resize-none text-[14px] text-slate-200 placeholder:text-slate-600 leading-relaxed [scrollbar-width:none] [&::-webkit-scrollbar]:hidden disabled:opacity-50"
        />

        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1">
            <input
              ref={fileRef}
              type="file"
              hidden
              accept=".pdf,image/png,image/jpeg,image/webp,image/gif"
              onChange={(e) => pickFile(e.target.files[0])}
            />
            <button
              title="Attach a PDF or image"
              className="flex items-center justify-center w-8 h-8 rounded-lg text-slate-600 hover:text-slate-400 hover:bg-white/[0.05] transition-all duration-150 cursor-pointer"
              onClick={() => fileRef.current.click()}
              disabled={isLoading}
            >
              <Paperclip size={14} />
            </button>
            <button
              onClick={toggleMic}
              title="Voice input"
              className={`flex items-center justify-center w-8 h-8 rounded-lg transition-all cursor-pointer ${
                isListening ? "bg-red-500 text-white" : "text-slate-600 hover:bg-white/[0.05]"
              }`}
            >
              {isListening ? <MicOff size={14} /> : <Mic size={14} />}
            </button>
          </div>

          <button
            onClick={handleSend}
            disabled={!canSend}
            title={isLoading ? "Stop" : "Send"}
            className={`flex items-center justify-center w-8 h-8 rounded-lg cursor-pointer transition-all duration-150
              ${
                isLoading
                  ? "bg-white text-[#0d0f14] hover:bg-slate-200"
                  : canSend
                    ? "bg-gradient-to-br from-indigo-500 to-violet-700 hover:opacity-90 text-white"
                    : "bg-white/[0.05] text-slate-600 cursor-not-allowed"
              }`}
          >
            {isLoading ? <Square size={12} fill="currentColor" /> : <Send size={14} />}
          </button>
        </div>
      </div>

      <p className="text-center text-[10.5px] text-slate-700 mt-2.5">
        CortexAI can make mistakes. Verify important info.
      </p>
    </div>
  );
}
