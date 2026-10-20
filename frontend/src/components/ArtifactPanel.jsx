import { useState } from "react";
import { useDispatch, useSelector } from "react-redux";
import Editor from "@monaco-editor/react";
import { detectLanguage } from "../utils/detectLanguage";
import { setCodeOpen } from "../redux/ui.slice";
import useDialog from "../hooks/useDialog";
import { Code2, Eye, PanelRightClose, PanelRightOpen, X, Copy, Check } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

// Monaco on the app's own ground instead of vs-dark's blue-grey.
const defineTheme = (monaco) =>
  monaco.editor.defineTheme("cortex", {
    base: "vs-dark",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": "#000000",
      "editorGutter.background": "#000000",
      "editorLineNumber.foreground": "#5f5f5f",
      "editorLineNumber.activeForeground": "#b4b4b4",
    },
  });

const iconButton =
  "flex items-center justify-center w-8 h-8 pointer-coarse:w-11 pointer-coarse:h-11 rounded-full text-faint hover:text-ink hover:bg-surface transition-colors cursor-pointer shrink-0";

export default function ArtifactPanel() {
  const [tab, setTab] = useState("code");
  const [activeFile, setActiveFile] = useState(0);
  const [collapsed, setCollapsed] = useState(false);
  const [copied, setCopied] = useState(false);
  const dispatch = useDispatch();
  const codeOpen = useSelector((state) => state.ui.codeOpen);
  const sheetRef = useDialog(codeOpen, () => dispatch(setCodeOpen(false)));

  const { artifacts } = useSelector((state) => state.message);
  const artifact = artifacts?.[0];

  if (!artifact) return null;

  const file = artifact?.files?.[activeFile] ?? artifact?.files?.[0];
  const htmlFile = artifact?.files?.find((f) => f.name === "index.html");
  const cssFile = artifact?.files?.find((f) => f.name === "style.css");
  const jsFile = artifact?.files?.find((f) => f.name === "script.js");
  const canPreview = Boolean(htmlFile);

  // The preview runs in a sandboxed iframe (scripts allowed, but no access to
  // this page's origin, cookies or storage). CSS and JS are inlined.
  const html = htmlFile?.content || "";
  const style = `<style>${cssFile?.content || ""}</style>`;
  const script = `<script>${jsFile?.content || ""}</` + `script>`;
  const previewDoc = /<html[\s>]/i.test(html)
    ? html.replace(/<\/head>/i, `${style}</head>`).replace(/<\/body>/i, `${script}</body>`)
    : `<!DOCTYPE html><html><head><meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1.0"/>${style}</head>
<body>${html}${script}</body></html>`;

  const handleCopy = () => {
    navigator.clipboard.writeText(file?.content || "");
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const segment = (id, Icon, label) => (
    <button
      role="tab"
      aria-selected={tab === id}
      onClick={() => setTab(id)}
      className={`h-7 pointer-coarse:h-10 flex items-center gap-1.5 px-3 rounded-full text-[13px] transition-colors cursor-pointer
        ${tab === id ? "bg-raised text-ink" : "text-muted hover:text-ink"}`}
    >
      <Icon size={13} strokeWidth={1.6} /> {label}
    </button>
  );

  const renderPanel = ({ onClose }) => (
    <div className="flex flex-col h-full bg-ground">
      <div className="h-14 shrink-0 px-3 flex items-center gap-2 border-b border-rule">
        <button
          onClick={onClose ?? (() => setCollapsed(true))}
          aria-label={onClose ? "Close code" : "Hide code"}
          title={onClose ? "Close" : "Hide code"}
          className={iconButton}
        >
          {onClose ? <X size={16} /> : <PanelRightClose size={16} strokeWidth={1.6} />}
        </button>
        <h2 className="flex-1 min-w-0 text-[14px] font-medium truncate">{artifact.title}</h2>
        {tab === "code" && (
          <button
            onClick={handleCopy}
            className="h-7 flex items-center gap-1.5 px-2.5 rounded-full text-[13px] text-muted hover:text-ink hover:bg-surface cursor-pointer transition-colors"
          >
            {copied ? <Check size={13} /> : <Copy size={13} />}
            {copied ? "Copied" : "Copy"}
          </button>
        )}
        {canPreview && (
          <div role="tablist" aria-label="View" className="flex items-center gap-0.5 bg-surface p-0.5 rounded-full">
            {segment("code", Code2, "Code")}
            {segment("preview", Eye, "Preview")}
          </div>
        )}
      </div>

      {tab === "code" && (
        <div role="tablist" aria-label="Files" className="flex gap-1 px-3 border-b border-rule overflow-x-auto shrink-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {artifact.files?.map((f, index) => {
            const active = file?.name === f.name;
            return (
              <button
                key={f.name}
                role="tab"
                aria-selected={active}
                onClick={() => setActiveFile(index)}
                className={`relative h-10 px-2.5 text-[13px] whitespace-nowrap cursor-pointer transition-colors
                  ${active ? "text-ink" : "text-faint hover:text-muted"}`}
              >
                {f.name}
                {active && <span className="absolute bottom-0 left-2.5 right-2.5 h-0.5 bg-ink rounded-full" />}
              </button>
            );
          })}
        </div>
      )}

      <div className="flex-1 overflow-hidden">
        {tab === "preview" && canPreview ? (
          <iframe title="Preview" sandbox="allow-scripts" srcDoc={previewDoc} className="w-full h-full bg-white" />
        ) : (
          <Editor
            theme="cortex"
            beforeMount={defineTheme}
            onMount={(_, monaco) => document.fonts?.ready.then(() => monaco.editor.remeasureFonts())}
            language={detectLanguage(file?.name || "")}
            value={file?.content || ""}
            options={{
              readOnly: true,
              minimap: { enabled: false },
              fontSize: 13,
              fontFamily: "'JetBrains Mono', ui-monospace, monospace", // code editor; taste-skill-ignore
              wordWrap: "on",
              automaticLayout: true,
              scrollBeyondLastLine: false,
              padding: { top: 16 },
              lineNumbers: "on",
              renderLineHighlight: "none",
            }}
          />
        )}
      </div>
    </div>
  );

  return (
    <>
      <AnimatePresence>
        {codeOpen && (
            <motion.div
              key="code-backdrop"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2 }}
              onClick={() => dispatch(setCodeOpen(false))}
              className="lg:hidden fixed inset-0 z-50 bg-black/70"
            />
        )}
        {codeOpen && (
            <motion.div
              key="code-sheet"
              ref={sheetRef}
              tabIndex={-1}
              role="dialog"
              aria-modal="true"
              aria-label="Code"
              initial={{ x: "100%" }}
              animate={{ x: 0 }}
              exit={{ x: "100%" }}
              transition={{ duration: 0.25, ease: "easeInOut" }}
              className="lg:hidden fixed inset-y-0 right-0 z-50 w-full sm:w-[min(520px,90vw)] overflow-hidden outline-none"
            >
              {renderPanel({ onClose: () => dispatch(setCodeOpen(false)) })}
            </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence initial={false}>
        {!collapsed ? (
          <motion.aside
            key="open"
            aria-label="Code"
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: "clamp(360px, 40%, 680px)", opacity: 1 }}
            exit={{ width: 0, opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeInOut" }}
            className="hidden lg:flex h-full border-l border-rule flex-col overflow-hidden shrink-0"
          >
            {renderPanel({})}
          </motion.aside>
        ) : (
          <motion.aside
            key="collapsed"
            aria-label="Code"
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: 56, opacity: 1 }}
            exit={{ width: 0, opacity: 0 }}
            transition={{ duration: 0.22, ease: "easeInOut" }}
            className="hidden lg:flex h-full border-l border-rule flex-col items-center py-3 gap-3 shrink-0"
          >
            <button onClick={() => setCollapsed(false)} aria-label="Show code" title="Show code" className={iconButton}>
              <PanelRightOpen size={16} strokeWidth={1.6} />
            </button>
            <p
              className="text-[12px] text-faint whitespace-nowrap overflow-hidden max-h-[60vh] text-ellipsis"
              style={{ writingMode: "vertical-rl", transform: "rotate(180deg)" }}
            >
              {artifact.title}
            </p>
          </motion.aside>
        )}
      </AnimatePresence>
    </>
  );
}
