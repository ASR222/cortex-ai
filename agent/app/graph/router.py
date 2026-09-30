"""Router and guard nodes.

router: decides which agent handles the request.
  1. An agent the user picked explicitly always wins.
  2. An uploaded image goes to vision, an uploaded PDF to rag.
  3. Otherwise a small, fast model classifies the prompt with structured
     output (a JSON enum), so the answer can only be a valid agent name.
     Any failure falls back to plain chat.

guard: enforces rate limits and reserves credits *after* routing (the price
depends on the agent) but *before* any expensive work runs.
"""

import logging
from typing import Literal

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel, Field

from app.costs import CREDIT_COST
from app.errors import AgentError
from app.graph.context import AgentState, deps_of, emit, request_of
from app.uploads import is_image

log = logging.getLogger(__name__)

AgentName = Literal["chat", "search", "coding", "pdf", "ppt", "image", "rag"]


class Route(BaseModel):
    agent: AgentName = Field(description="The single best agent for the request")


ROUTER_PROMPT = """You route requests for CortexAI to exactly one agent.

Agents:
- chat: conversation, explanations, learning, advice, writing, general questions.
- search: needs current or recent information from the web (news, prices, releases, events, "latest", "today").
- coding: write, debug, review, explain or refactor code; build apps or websites.
- pdf: the user asks you to CREATE a PDF document or report file.
- ppt: the user asks you to CREATE a presentation or slide deck.
- image: the user asks you to GENERATE or draw an image, picture, logo or artwork.
{rag_line}
Pick "chat" when unsure. Only pick pdf/ppt/image when the user wants a file or picture produced."""


async def route(state: AgentState, config: RunnableConfig) -> AgentState:
    req = request_of(config)
    requested = state.get("requested_agent", "auto")

    if requested != "auto":
        agent = requested
    elif req.upload and is_image(req.upload.mime):
        agent = "vision"
    elif req.upload:
        agent = "rag"
    else:
        agent = await classify(state, config)

    if agent == "vision" and not (req.upload and is_image(req.upload.mime)):
        raise AgentError(400, "bad_request", "Attach an image to use image understanding.")
    if agent == "rag" and not state.get("documents"):
        raise AgentError(400, "bad_request", "Upload a PDF first to ask questions about it.")

    log.info("routed", extra={"agent": agent, "requested": requested})
    emit("meta", agent=agent, cost=CREDIT_COST[agent])
    return {"agent": agent}


async def classify(state: AgentState, config: RunnableConfig) -> str:
    deps = deps_of(config)
    docs = state.get("documents") or []
    rag_line = ""
    if docs:
        names = ", ".join(d["name"] for d in docs[-5:])
        rag_line = f"- rag: questions about the user's uploaded documents ({names})."
    try:
        model = deps.models.raw(deps.settings.router_model, temperature=0, max_tokens=1024)
        result = await model.with_structured_output(Route).ainvoke(
            [
                SystemMessage(ROUTER_PROMPT.format(rag_line=rag_line)),
                HumanMessage(state["prompt"][:2000]),
            ]
        )
        agent = result.agent if isinstance(result, Route) else "chat"
    except Exception:  # classification is best effort
        log.warning("router classification failed; defaulting to chat", exc_info=True)
        return "chat"
    if agent == "rag" and not docs:
        return "chat"
    return agent


async def guard(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    agent = state["agent"]
    await deps.limiter.check(req.user_id, agent)
    # Reserve last: if anything above fails there is nothing to refund.
    req.reservation_id, req.credits = await deps.auth.reserve(req.user_id, CREDIT_COST[agent], agent)
    return {}
