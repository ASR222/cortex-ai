"""Web search agent: Tavily search, then an answer grounded in the results
with numbered citations."""

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig

from app.agents.common import FORMATTING_RULES, history_messages, stream_llm
from app.errors import AgentError
from app.graph.context import AgentState, deps_of, emit, request_of, status

TAVILY_URL = "https://api.tavily.com/search"

SYSTEM = """You are CortexAI answering with fresh web search results.

Rules:
- Answer using ONLY the search results below. If they do not contain the answer, say so.
- Cite sources inline with their number, like [1] or [2][3].
- Do not mention that you used a search tool.

{formatting}

Search results:
{results}"""


async def web_search(deps, query: str) -> dict:
    if not deps.settings.tavily_api_key:
        raise AgentError(503, "not_configured", "Web search is not configured on this server.")
    resp = await deps.http.post(
        TAVILY_URL,
        json={"query": query[:400], "max_results": 5, "include_images": True, "search_depth": "basic"},
        headers={"Authorization": f"Bearer {deps.settings.tavily_api_key}"},
        timeout=30,
    )
    if resp.status_code != 200:
        raise AgentError(502, "search_failed", "Web search is unavailable right now. Please try again.")
    return resp.json()


async def search_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    status("Searching the web")
    data = await web_search(deps, state["prompt"])

    results = data.get("results", [])[:5]
    sources = [{"title": r.get("title") or r["url"], "url": r["url"]} for r in results if r.get("url")]
    images = [i for i in data.get("images", []) if isinstance(i, str) and i.startswith("https://")][:4]
    emit("sources", sources=sources, images=images)

    context = (
        "\n\n".join(
            f"[{n}] {r.get('title', '')}\nURL: {r.get('url', '')}\n{r.get('content', '')[:1500]}"
            for n, r in enumerate(results, start=1)
        )
        or "(no results)"
    )

    status("Reading sources")
    model = deps.models.get(deps.settings.chat_model, temperature=0.2)
    messages = [
        SystemMessage(SYSTEM.format(formatting=FORMATTING_RULES, results=context)),
        *history_messages(state.get("history", []), max_messages=6),
        HumanMessage(state["prompt"]),
    ]
    answer = await stream_llm(model, messages, req)
    return {"response": answer, "sources": sources, "images": images}
