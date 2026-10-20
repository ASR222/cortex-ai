import { memo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import { Copy, Check, FileText, Presentation, Download, AlertCircle, Image as ImageIcon, X } from "lucide-react";
import { AGENT_LABELS, CREDIT_COST } from "../utils/agents";
import useDialog from "../hooks/useDialog";

// oneDark without its text shadows, in the app's one code face.
const codeTheme = Object.fromEntries(
  Object.entries(oneDark).map(([key, rule]) => [
    key,
    key.includes("language-") ? { ...rule, textShadow: "none", fontFamily: "var(--font-mono)", background: "transparent" } : rule,
  ]),
);

const fileUrl = (att) => att.url || (att.fileId ? `/api/files/${att.fileId}` : att.localUrl);
const hostOf = (url) => {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
};

function AttachmentChip({ att }) {
  const Icon = att.mime?.includes("presentation") ? Presentation : att.mime?.startsWith("image/") ? ImageIcon : FileText;
  const url = fileUrl(att);
  const body = (
    <>
      <Icon size={15} strokeWidth={1.6} className="text-faint shrink-0" />
      <span className="truncate max-w-[240px]">{att.name}</span>
      {url && att.kind === "generated" && <Download size={14} strokeWidth={1.6} className="text-faint shrink-0" />}
    </>
  );
  const cls = "inline-flex items-center gap-2 h-9 rounded-full bg-surface px-3.5 text-[13px]";
  return url ? (
    <a href={url} target="_blank" rel="noreferrer" className={`${cls} hover:bg-hover transition-colors`}>
      {body}
    </a>
  ) : (
    <span className={cls}>{body}</span>
  );
}

/** Which specialist answered, what it cost, and how many sources it read. */
function RouteLine({ agent, cost, sources, error }) {
  const price = cost ?? CREDIT_COST[agent];
  return (
    <p className="flex items-center gap-1.5 text-[13px] text-faint mb-2">
      <span className="text-accent">{AGENT_LABELS[agent] || agent}</span>
      {error ? (
        <span>· refunded</span>
      ) : (
        price != null && (
          <span>
            · <span className="num">{price}</span> {price === 1 ? "credit" : "credits"}
          </span>
        )
      )}
      {sources > 0 && (
        <span>
          · <span className="num">{sources}</span> {sources === 1 ? "source" : "sources"}
        </span>
      )}
    </p>
  );
}

function MessageBubble({ message, streaming }) {
  const { role, content = "", images = [], attachments = [], sources = [], agent, cost, error, stopped } = message;
  const isUser = role === "user";
  const [lightboxSrc, setLightboxSrc] = useState(null);
  const [copiedCode, setCopiedCode] = useState("");
  const lightboxRef = useDialog(Boolean(lightboxSrc), () => setLightboxSrc(null));

  const copyCode = async (code) => {
    await navigator.clipboard.writeText(code);
    setCopiedCode(code);
    setTimeout(() => setCopiedCode(""), 2000);
  };

  const imageUploads = isUser ? attachments.filter((a) => a.mime?.startsWith("image/")) : [];
  const fileChips = attachments.filter((a) => !(isUser && a.mime?.startsWith("image/")));

  return (
    <div className={`flex ${isUser ? "justify-end" : "justify-start"}`}>
      <div className={`flex flex-col gap-2 min-w-0 ${isUser ? "items-end max-w-[85%]" : "w-full"}`}>
        {imageUploads.map((att, i) => (
          <img
            key={i}
            src={fileUrl(att)}
            alt={att.name}
            onClick={() => setLightboxSrc(fileUrl(att))}
            className="max-w-[240px] max-h-[200px] rounded-2xl object-cover cursor-zoom-in"
          />
        ))}

        {(content || error || stopped) &&
          (isUser ? (
            <div className="w-fit max-w-full px-4 py-2.5 rounded-[20px] bg-surface text-[15px] leading-[24px] break-words">
              <p className="whitespace-pre-wrap">{content}</p>
            </div>
          ) : (
            <div className="min-w-0 text-[15px] leading-[26px] break-words">
              {agent && !streaming && <RouteLine agent={agent} cost={cost} sources={sources.length} error={error} />}
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                  h1: ({ children }) => <h2 className="text-[20px] font-semibold mt-6 mb-2">{children}</h2>,
                  h2: ({ children }) => <h3 className="text-[18px] font-semibold mt-5 mb-2">{children}</h3>,
                  h3: ({ children }) => <h4 className="text-[16px] font-semibold mt-4 mb-1.5">{children}</h4>,
                  p: ({ children }) => <p className="mb-3 last:mb-0 whitespace-pre-wrap">{children}</p>,
                  ul: ({ children }) => <ul className="list-disc pl-5 space-y-1.5 mb-3 marker:text-faint">{children}</ul>,
                  ol: ({ children }) => <ol className="list-decimal pl-5 space-y-1.5 mb-3 marker:text-faint">{children}</ol>,
                  strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
                  table: ({ children }) => (
                    <div className="overflow-x-auto my-4">
                      <table className="min-w-full text-[14px]">{children}</table>
                    </div>
                  ),
                  th: ({ children }) => (
                    <th className="border-b border-raised pb-2 pr-6 text-left text-[13px] font-normal text-faint">
                      {children}
                    </th>
                  ),
                  td: ({ children }) => <td className="num border-b border-rule py-2 pr-6 align-top">{children}</td>,
                  a: ({ href, children }) => (
                    <a
                      href={href}
                      target="_blank"
                      rel="noreferrer"
                      className="underline decoration-control underline-offset-4 hover:decoration-ink"
                    >
                      {children}
                    </a>
                  ),
                  img: ({ src, alt }) =>
                    src ? (
                      <img
                        src={src}
                        alt={alt || ""}
                        loading="lazy"
                        onClick={() => setLightboxSrc(src)}
                        className="w-full max-w-md rounded-2xl cursor-zoom-in my-3"
                      />
                    ) : null,
                  code({ className, children }) {
                    const value = String(children).replace(/\n$/, "");
                    if (!className) {
                      return <code className="px-1.5 py-0.5 rounded-md bg-surface text-[14px]">{value}</code>;
                    }
                    const language = className.replace("language-", "");
                    return (
                      <div className="my-4 overflow-hidden rounded-2xl bg-surface">
                        <div className="flex items-center justify-between h-10 pl-4 pr-2">
                          <span className="text-[12px] text-faint">{language}</span>
                          <button
                            onClick={() => copyCode(value)}
                            className="h-7 pointer-coarse:h-10 flex items-center gap-1.5 px-2.5 rounded-full text-[12px] text-muted hover:text-ink hover:bg-hover cursor-pointer transition-colors"
                          >
                            {copiedCode === value ? <Check size={13} /> : <Copy size={13} />}
                            {copiedCode === value ? "Copied" : "Copy"}
                          </button>
                        </div>
                        <SyntaxHighlighter
                          language={language}
                          style={codeTheme}
                          wrapLongLines
                          customStyle={{ margin: 0, padding: "4px 16px 16px", background: "transparent", fontSize: "13px" }}
                          codeTagProps={{ style: { background: "transparent", fontFamily: "var(--font-mono)" } }}
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
              {error && (
                <p className="flex items-center gap-1.5 text-[13px] text-negative mt-1">
                  <AlertCircle size={14} /> {error}
                </p>
              )}
              {stopped && <p className="text-[13px] text-faint mt-1">Stopped.</p>}
            </div>
          ))}

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
                className="w-36 h-24 rounded-xl object-cover cursor-zoom-in hover:opacity-90 transition"
              />
            ))}
          </div>
        )}

        {sources.length > 0 && (
          <ol className="mt-1 rounded-2xl bg-surface py-1.5" aria-label="Sources">
            {sources.map((s, i) => (
              <li key={i}>
                <a
                  href={s.url}
                  target="_blank"
                  rel="noreferrer"
                  title={s.url}
                  className="flex items-center gap-3 h-9 pointer-coarse:h-11 px-4 text-[14px] hover:bg-hover transition-colors group"
                >
                  <span className="num w-4 text-accent shrink-0">{i + 1}</span>
                  <span className="truncate group-hover:underline underline-offset-4 decoration-control">{s.title}</span>
                  <span className="ml-auto pl-3 text-[13px] text-faint shrink-0 hidden sm:inline">{hostOf(s.url)}</span>
                </a>
              </li>
            ))}
          </ol>
        )}
      </div>

      {lightboxSrc && (
        <div
          ref={lightboxRef}
          role="dialog"
          aria-modal="true"
          aria-label="Image"
          className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center p-6"
          onClick={() => setLightboxSrc(null)}
        >
          <button
            type="button"
            onClick={() => setLightboxSrc(null)}
            aria-label="Close"
            className="absolute top-5 right-5 w-11 h-11 flex items-center justify-center rounded-full bg-surface text-ink cursor-pointer"
          >
            <X size={18} />
          </button>
          <img
            src={lightboxSrc}
            alt=""
            onClick={(e) => e.stopPropagation()}
            className="max-w-[90vw] max-h-[85vh] rounded-2xl object-contain"
          />
        </div>
      )}
    </div>
  );
}

export default memo(MessageBubble);
