"""Image understanding agent (Gemini multimodal)."""

import base64

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig

from app.agents.common import stream_llm
from app.graph.context import AgentState, deps_of, request_of, status

SYSTEM = """You are CortexAI's vision assistant.
- Answer the user's question about the attached image accurately.
- Transcribe any text in the image when relevant.
- Explain charts, tables and diagrams clearly.
- If something is unclear or not visible, say so. Never guess details.
- Use Markdown when it helps."""


async def vision_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    upload = req.upload
    assert upload is not None  # guaranteed by the router

    status("Looking at your image")
    data_uri = f"data:{upload.mime};base64,{base64.b64encode(upload.data).decode()}"
    model = deps.models.raw(deps.settings.vision_model, temperature=0.2)
    messages = [
        SystemMessage(SYSTEM),
        HumanMessage(
            content=[
                {"type": "text", "text": state["prompt"] or "Describe this image in detail."},
                {"type": "image_url", "image_url": {"url": data_uri}},
            ]
        ),
    ]
    return {"response": await stream_llm(model, messages, req)}
