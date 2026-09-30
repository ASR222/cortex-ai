"""Coding agent.

Two output modes, chosen by the model:
- Project: the reply starts with "FILE: <name>" blocks. We parse them into an
  artifact (shown in the code/preview panel). While generating, raw file text
  is not streamed into the chat; instead the user sees "Writing index.html…".
- Answer: reviews, explanations and debugging are streamed as Markdown.
"""

import re
import uuid
from datetime import UTC, datetime

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig

from app.agents.common import emit_text, history_messages, text_of
from app.graph.context import AgentState, deps_of, emit, request_of, status

SYSTEM = """You are CortexAI's coding agent.

First decide what the user wants:
A) A NEW project, app, website, component or multi-file program  -> PROJECT mode.
B) Review, explain, debug, optimize, convert or answer a question about code -> ANSWER mode.

PROJECT mode:
- Output ONLY files, each starting on its own line with:  FILE: <relative/path>
  followed by the file content. No prose before, between or after files.
- Websites default to plain HTML/CSS/JS in exactly three files: index.html, style.css, script.js
  (index.html must not link external CSS/JS files of its own; they are injected automatically).
  Use a framework only if the user asks for one.
- Build a single page with sections unless multiple pages are requested.
- Modern, responsive design: CSS variables, flexbox/grid, subtle hover effects and transitions.
- Use real image URLs from https://images.unsplash.com when images help. No placeholders.
- Keep JavaScript minimal and purposeful. Keep the total output under ~2500 tokens.

ANSWER mode:
- Reply in Markdown with clear sections (for reviews: Overview, Problems, Improvements, Improved code).
- Use single backticks for identifiers and fenced blocks with a language tag for code.
- Do NOT use the FILE: format."""

FILE_LINE = re.compile(r"^FILE:\s*(\S[^\n]*?)\s*$", re.MULTILINE)
SAFE_PATH = re.compile(r"^[A-Za-z0-9_][A-Za-z0-9_./-]{0,120}$")
FENCE = re.compile(r"^\s*```[\w+-]*\s*\n|\n\s*```\s*$")


def parse_files(text: str) -> list[dict]:
    """Splits "FILE: name" blocks into [{name, content}], dropping unsafe paths."""
    matches = list(FILE_LINE.finditer(text))
    files: list[dict] = []
    for i, m in enumerate(matches):
        name = m.group(1).strip().strip("`*")
        end = matches[i + 1].start() if i + 1 < len(matches) else len(text)
        content = FENCE.sub("", text[m.end() : end].strip("\n")).strip("\n")
        if SAFE_PATH.match(name) and ".." not in name and not any(f["name"] == name for f in files):
            files.append({"name": name, "content": content + "\n"})
    return files


def summarize(title: str, files: list[dict]) -> str:
    names = ", ".join(f"`{f['name']}`" for f in files)
    preview = (
        " Switch to **Preview** in the side panel to try it." if any(f["name"] == "index.html" for f in files) else ""
    )
    return f"Built **{title}** — {len(files)} file{'s' if len(files) != 1 else ''}: {names}.{preview}"


async def coding_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    model = deps.models.get(deps.settings.coding_model, temperature=0.2, max_tokens=6000)
    messages = [SystemMessage(SYSTEM), *history_messages(state.get("history", []), 6), HumanMessage(state["prompt"])]

    buffer = ""
    mode: str | None = None  # "project" | "answer", decided from the first characters
    announced: set[str] = set()

    async for chunk in model.astream(messages):
        text = text_of(chunk)
        if not text:
            continue
        buffer += text
        if mode == "answer":
            emit_text(req, text)
            continue
        if mode is None:
            head = buffer.lstrip()[:5]
            if not "FILE:".startswith(head):
                mode = "answer"  # cannot be a FILE: header; flush what we held back
                emit_text(req, buffer)
                continue
            if len(head) < 5:
                continue  # still ambiguous ("FI…"); keep buffering
            mode = "project"
        complete_lines = buffer[: buffer.rfind("\n") + 1]
        for m in FILE_LINE.finditer(complete_lines):
            name = m.group(1).strip()
            if name not in announced:
                announced.add(name)
                status(f"Writing {name}")

    if mode is None:  # very short reply
        emit_text(req, buffer)
        return {"response": buffer}

    if mode == "answer":
        return {"response": buffer}

    files = parse_files(buffer)
    if not files:
        emit_text(req, buffer)
        return {"response": buffer}

    title = " ".join(state["prompt"].split())[:60]
    artifact = {
        "id": uuid.uuid4().hex,
        "type": "project",
        "title": title,
        "files": files,
        "createdAt": datetime.now(UTC).isoformat(),
    }
    emit("artifact", artifact=artifact)
    summary = summarize(title, files)
    emit_text(req, summary)
    return {"response": summary, "artifacts": [artifact]}
