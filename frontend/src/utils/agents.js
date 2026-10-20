import { Code2, FileText, Globe, Image as ImageIcon, MessageSquare, Presentation, Sparkles } from "lucide-react";

// Credits per answer. Mirrors agent/app/costs.py; the server's "meta" event
// carries the real cost, this is the fallback for messages loaded from history.
export const CREDIT_COST = { chat: 1, rag: 2, search: 3, vision: 3, coding: 5, pdf: 5, ppt: 5, image: 5 };

export const AGENT_LABELS = {
  chat: "Chat",
  search: "Web search",
  coding: "Coding",
  pdf: "PDF",
  ppt: "Slides",
  image: "Image",
  vision: "Vision",
  rag: "Document Q&A",
};

// The agents a user can pick in the composer. "auto" lets the router choose.
export const PICKABLE = [
  { id: "auto", icon: Sparkles, label: "Auto" },
  { id: "chat", icon: MessageSquare, label: "Chat" },
  { id: "search", icon: Globe, label: "Search" },
  { id: "coding", icon: Code2, label: "Coding" },
  { id: "pdf", icon: FileText, label: "PDF" },
  { id: "ppt", icon: Presentation, label: "Slides" },
  { id: "image", icon: ImageIcon, label: "Image" },
];

// What each specialist does, with a prompt to try. The two without a prompt
// run automatically when a file is attached.
export const SPECIALISTS = [
  { id: "chat", does: "Answers, explanations and writing", prompt: "Explain how Redis persistence works" },
  { id: "search", does: "Current answers with cited sources", prompt: "What's new in AI this week?" },
  { id: "coding", does: "Apps and sites with a live preview", prompt: "Build a landing page for a coffee shop" },
  { id: "ppt", does: "A slide deck you can download", prompt: "Create a presentation on climate tech" },
  { id: "pdf", does: "A formatted PDF report", prompt: "Write a PDF report on India's EV market" },
  { id: "image", does: "An image from a description", prompt: "A tea stall at dawn, in watercolour" },
  { id: "rag", does: "Questions about a PDF you attach", prompt: null },
  { id: "vision", does: "Questions about an image you attach", prompt: null },
];
