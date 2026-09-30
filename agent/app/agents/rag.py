"""Document Q&A over the PDFs uploaded to this conversation."""

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig

from app.agents.common import emit_text, file_url, history_messages, stream_llm
from app.graph.context import AgentState, deps_of, emit, request_of, status

SYSTEM = """You are CortexAI answering questions about the user's documents.

Rules:
- Answer ONLY from the excerpts below. Never use outside knowledge or invent details.
- Cite the source after each claim, like (report.pdf, p. 4).
- If the excerpts do not contain the answer, say you could not find it in the document.
- Use Markdown when it helps.

Excerpts:
{context}"""

MIN_SCORE = 0.35


async def rag_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    question = state["prompt"] or "Summarize this document."

    status("Searching your documents")
    chunks = [c for c in await deps.index.search(req.user_id, req.conversation_id, question) if c.score >= MIN_SCORE]
    if not chunks:
        names = ", ".join(d["name"] for d in state.get("documents", [])) or "your documents"
        response = f"I couldn't find anything about that in {names}. Try rephrasing, or ask about a specific section."
        emit_text(req, response)
        return {"response": response}

    context = "\n\n".join(f"[{c.name}, p. {c.page}]\n{c.text}" for c in chunks)
    seen: set[tuple[str, int]] = set()
    sources = []
    for c in sorted(chunks, key=lambda c: (c.name, c.page)):
        if (c.file_id, c.page) not in seen:
            seen.add((c.file_id, c.page))
            sources.append({"title": f"{c.name} · p. {c.page}", "url": f"{file_url(c.file_id)}#page={c.page}"})
    emit("sources", sources=sources, images=[])

    model = deps.models.get(deps.settings.chat_model, temperature=0.1)
    messages = [
        SystemMessage(SYSTEM.format(context=context)),
        *history_messages(state.get("history", []), 6),
        HumanMessage(question),
    ]
    return {"response": await stream_llm(model, messages, req), "sources": sources}
