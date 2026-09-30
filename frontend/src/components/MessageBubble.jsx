import { memo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { FiExternalLink, FiX } from "react-icons/fi";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import { Copy, Check, FileText, Presentation, Download, AlertCircle, Image as ImageIcon } from "lucide-react";

const AGENT_LABELS = {
  chat: "Chat",
  search: "Web search",
  coding: "Coding",
  pdf: "PDF",
  ppt: "Slides",
  image: "Image",
  vision: "Vision",
  rag: "Document Q&A",
};

const fileUrl = (att) => att.url || (att.fileId ? `/api/files/${att.fileId}` : att.localUrl);

function AttachmentChip({ att }) {
  const Icon = att.mime?.includes("presentation") ? Presentation : att.mime?.startsWith("image/") ? ImageIcon : FileText;
  const url = fileUrl(att);
  const body = (
    <>
      <Icon size={15} className="text-indigo-300 shrink-0" />
      <span className="truncate max-w-[220px]">{att.name}</span>
      {url && att.kind === "generated" && <Download size={13} className="text-slate-400 shrink-0" />}
    </>
  );
  const cls =
    "inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.05] px-3 py-2 text-xs text-slate-200";
  return url ? (
    <a href={url} target="_blank" rel="noreferrer" className={`${cls} hover:bg-white/[0.09] transition-colors`}>
      {body}
    </a>
  ) : (
    <span className={cls}>{body}</span>
  );
}

function MessageBubble({ message, streaming }) {
  const { role, content = "", images = [], attachments = [], sources = [], agent, error, stopped } = message;
  const isUser = role === "user";
  const [lightboxSrc, setLightboxSrc] = useState(null);
  const [copiedCode, setCopiedCode] = useState("");

  const copyCode = async (code) => {
    await navigator.clipboard.writeText(code);
    setCopiedCode(code);
    setTimeout(() => setCopiedCode(""), 2000);
  };

  const imageUploads = isUser ? attachments.filter((a) => a.mime?.startsWith("image/")) : [];
  const fileChips = attachments.filter((a) => !(isUser && a.mime?.startsWith("image/")));

  return (
    <div className={`flex ${isUser ? "justify-end" : "justify-start"}`}>
      <div className={`flex flex-col gap-2 min-w-0 ${isUser ? "items-end max-w-[85%] md:max-w-[72%]" : "w-full md:max-w-[80%]"}`}>
        {!isUser && agent && agent !== "chat" && (
          <span className="text-[10px] uppercase tracking-widest text-indigo-400/80 font-semibold">
            {AGENT_LABELS[agent] || agent}
          </span>
        )}

        {imageUploads.map((att, i) => (
          <img
            key={i}
            src={fileUrl(att)}
            alt={att.name}
            onClick={() => setLightboxSrc(fileUrl(att))}
            className="max-w-[240px] max-h-[200px] rounded-xl object-cover border border-white/10 cursor-zoom-in"
          />
        ))}

        {(content || error || stopped) && (
          <div
            className={`w-fit max-w-full px-4 py-2.5 rounded-2xl break-words overflow-hidden leading-relaxed text-[14px]
              ${isUser ? "bg-gradient-to-br from-indigo-500 to-violet-700 text-white rounded-tr-sm" : "text-slate-200 rounded-tl-sm px-0"}`}
          >
            {isUser ? (
              <p className="whitespace-pre-wrap">{content}</p>
            ) : (
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                  h1: ({ children }) => <h1 className="text-2xl font-bold mt-5 mb-3">{children}</h1>,
                  h2: ({ children }) => <h2 className="text-xl font-semibold mt-4 mb-2">{children}</h2>,
                  h3: ({ children }) => <h3 className="text-lg font-semibold mt-3 mb-2">{children}</h3>,
                  p: ({ children }) => <p className="mb-3 whitespace-pre-wrap break-words">{children}</p>,
                  ul: ({ children }) => <ul className="list-disc pl-5 space-y-1 my-2">{children}</ul>,
                  ol: ({ children }) => <ol className="list-decimal pl-5 space-y-1 my-2">{children}</ol>,
                  table: ({ children }) => (
                    <div className="overflow-x-auto my-4">
                      <table className="min-w-full border border-white/10">{children}</table>
                    </div>
                  ),
                  th: ({ children }) => (
                    <th className="border border-white/10 bg-white/5 px-3 py-2 text-left">{children}</th>
                  ),
                  td: ({ children }) => <td className="border border-white/10 px-3 py-2">{children}</td>,
                  a: ({ href, children }) => (
                    <a
                      href={href}
                      target="_blank"
                      rel="noreferrer"
                      className="text-indigo-400 underline inline-flex items-center gap-1"
                    >
                      {children}
                      <FiExternalLink size={11} />
                    </a>
                  ),
                  img: ({ src, alt }) =>
                    src ? (
                      <img
                        src={src}
                        alt={alt || ""}
                        loading="lazy"
                        onClick={() => setLightboxSrc(src)}
                        className="w-full max-w-md rounded-xl border border-white/10 cursor-zoom-in my-2"
                      />
                    ) : null,
                  code({ className, children }) {
                    const value = String(children).replace(/\n$/, "");
                    if (!className) {
                      return <code className="px-1.5 py-0.5 rounded bg-white/10 text-pink-400">{value}</code>;
                    }
                    const language = className.replace("language-", "");
                    return (
                      <div className="my-4 overflow-hidden rounded-xl border border-white/10 bg-[#111318]">
                        <div className="flex items-center justify-between bg-[#1b1d24] border-b border-white/10 px-4 py-2">
                          <span className="uppercase text-xs text-slate-400">{language}</span>
                          <button onClick={() => copyCode(value)} className="flex items-center gap-1 text-xs cursor-pointer">
                            {copiedCode === value ? (
                              <>
                                <Check size={14} /> Copied
                              </>
                            ) : (
                              <>
                                <Copy size={14} /> Copy
                              </>
                            )}
                          </button>
                        </div>
                        <SyntaxHighlighter
                          language={language}
                          style={oneDark}
                          wrapLongLines
                          customStyle={{ margin: 0, padding: "16px", background: "#0d1117", fontSize: "13px" }}
                        >
                          {value}
                        </SyntaxHighlighter>
                      </div>
                    );
                  },
                }}
              >
                {content + (streaming ? " ▍" : "")}
              </ReactMarkdown>
            )}
            {error && (
              <p className="flex items-center gap-1.5 text-[12px] text-red-400 mt-1">
                <AlertCircle size={13} /> {error}
              </p>
            )}
            {stopped && <p className="text-[12px] text-slate-500 mt-1">Stopped.</p>}
          </div>
        )}

        {fileChips.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {fileChips.map((att, i) => (
              <AttachmentChip key={i} att={att} />
            ))}
          </div>
        )}

        {images.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {images.map((img, i) => (
              <img
                key={i}
                src={img}
                alt=""
                loading="lazy"
                referrerPolicy="no-referrer"
                onClick={() => setLightboxSrc(img)}
                onError={(e) => e.currentTarget.remove()}
                className="w-36 h-24 rounded-xl object-cover border border-white/10 cursor-zoom-in hover:opacity-90 transition"
              />
            ))}
          </div>
        )}

        {sources.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {sources.map((s, i) => (
              <a
                key={i}
                href={s.url}
                target="_blank"
                rel="noreferrer"
                title={s.url}
                className="inline-flex items-center gap-1.5 max-w-[260px] rounded-lg bg-white/[0.04] border border-white/[0.07] px-2.5 py-1 text-[11px] text-slate-400 hover:text-slate-200 hover:bg-white/[0.08]"
              >
                <span className="text-indigo-400 font-semibold">{i + 1}</span>
                <span className="truncate">{s.title}</span>
              </a>
            ))}
          </div>
        )}
      </div>

      {lightboxSrc && (
        <div
          className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-6"
          onClick={() => setLightboxSrc(null)}
        >
          <button
            type="button"
            onClick={() => setLightboxSrc(null)}
            className="absolute top-5 right-5 text-white/80 hover:text-white bg-white/10 rounded-full p-2 cursor-pointer"
          >
            <FiX size={20} />
          </button>
          <img
            src={lightboxSrc}
            alt=""
            onClick={(e) => e.stopPropagation()}
            className="max-w-[90vw] max-h-[85vh] rounded-2xl border border-white/10 shadow-2xl object-contain"
          />
        </div>
      )}
    </div>
  );
}

export default memo(MessageBubble);
