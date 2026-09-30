"""Helpers shared by agent nodes."""

from langchain_core.messages import AIMessage, BaseMessage, HumanMessage
from langchain_core.runnables import Runnable

from app.graph.context import RequestContext, emit

FORMATTING_RULES = """Formatting:
- For greetings and short questions, reply naturally in plain text.
- For technical or detailed topics, use clean Markdown: # for the title, ## for sections, a blank line after headings.
- Use bullet lists for lists and numbered lists for steps.
- Put code in fenced code blocks with a language tag.
- Keep paragraphs short. Never produce a wall of text."""


def history_messages(history: list[dict], max_messages: int = 12) -> list[BaseMessage]:
    out: list[BaseMessage] = []
    for m in history[-max_messages:]:
        if m["role"] == "user":
            out.append(HumanMessage(m["content"]))
        elif m["role"] == "assistant":
            out.append(AIMessage(m["content"]))
    return out


def text_of(chunk) -> str:
    """Extracts plain text from a message chunk (string or content blocks)."""
    content = chunk.content
    if isinstance(content, str):
        return content
    parts = []
    for block in content or []:
        if isinstance(block, str):
            parts.append(block)
        elif isinstance(block, dict) and block.get("type") == "text":
            parts.append(block.get("text", ""))
    return "".join(parts)


def emit_text(req: RequestContext, text: str) -> None:
    if text:
        req.streamed_chars += len(text)
        emit("token", text=text)


async def stream_llm(model: Runnable, messages: list[BaseMessage], req: RequestContext) -> str:
    """Streams model output to the client token by token; returns the full text."""
    parts: list[str] = []
    async for chunk in model.astream(messages):
        text = text_of(chunk)
        if text:
            parts.append(text)
            emit_text(req, text)
    return "".join(parts)


def file_url(key: str) -> str:
    """Stable download URL for a stored file. It never expires because access
    is checked on every request (session at the gateway, owner here)."""
    return f"/api/files/{key}"
