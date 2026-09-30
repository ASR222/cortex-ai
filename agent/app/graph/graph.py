"""The LangGraph supervisor graph.

    START -> router -> guard -> <agent> -> END

The router picks an agent, the guard enforces limits and reserves credits,
and exactly one agent node produces the answer. Credits are committed or
refunded by the API layer once the graph finishes, because only it knows
whether the whole request (including saving messages) succeeded.
"""

from langgraph.graph import END, START, StateGraph

from app.agents.chat import chat_agent
from app.agents.coding import coding_agent
from app.agents.files import image_agent, pdf_agent, ppt_agent
from app.agents.rag import rag_agent
from app.agents.search import search_agent
from app.agents.vision import vision_agent
from app.costs import AGENTS
from app.graph.context import AgentState
from app.graph.router import guard, route

AGENT_NODES = {
    "chat": chat_agent,
    "search": search_agent,
    "coding": coding_agent,
    "pdf": pdf_agent,
    "ppt": ppt_agent,
    "image": image_agent,
    "vision": vision_agent,
    "rag": rag_agent,
}
assert set(AGENT_NODES) == set(AGENTS)


def build_graph():
    g = StateGraph(AgentState)
    g.add_node("router", route)
    g.add_node("guard", guard)
    for name, node in AGENT_NODES.items():
        g.add_node(name, node)
        g.add_edge(name, END)
    g.add_edge(START, "router")
    g.add_edge("router", "guard")
    g.add_conditional_edges("guard", lambda s: s["agent"], {name: name for name in AGENT_NODES})
    return g.compile()


graph = build_graph()
