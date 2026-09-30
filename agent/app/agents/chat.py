"""General conversation agent."""

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig

from app.agents.common import FORMATTING_RULES, history_messages, stream_llm
from app.graph.context import AgentState, deps_of, request_of

SYSTEM = f"""You are CortexAI, a helpful, accurate and friendly AI assistant.
Be concise, admit uncertainty, and never invent facts.

{FORMATTING_RULES}"""


async def chat_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    model = deps.models.get(deps.settings.chat_model, temperature=0.5)
    messages = [SystemMessage(SYSTEM), *history_messages(state.get("history", [])), HumanMessage(state["prompt"])]
    return {"response": await stream_llm(model, messages, req)}
