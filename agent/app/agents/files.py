"""Agents that produce files: PDF reports, PowerPoint decks and images."""

import asyncio
import re
from urllib.parse import quote

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel, ValidationError

from app.agents.common import emit_text, file_url
from app.documents.pdf_builder import build_pdf
from app.documents.ppt_builder import build_pptx
from app.documents.specs import DeckSpec, DocumentSpec
from app.errors import AgentError
from app.graph.context import AgentState, Deps, RequestContext, deps_of, emit, request_of, status
from app.storage import make_key
from app.uploads import sniff_mime

PDF_MIME = "application/pdf"
PPTX_MIME = "application/vnd.openxmlformats-officedocument.presentationml.presentation"


def slug(text: str, default: str) -> str:
    s = re.sub(r"[^A-Za-z0-9]+", "-", text).strip("-").lower()[:50]
    return s or default


async def structured[T: BaseModel](deps: Deps, schema: type[T], system: str, prompt: str) -> T:
    """Asks the documents model for output matching schema, retrying once."""
    model = deps.models.raw(deps.settings.documents_model, temperature=0.4, max_tokens=6000)
    runnable = model.with_structured_output(schema)
    messages = [SystemMessage(system), HumanMessage(prompt)]
    last: Exception | None = None
    for _ in range(2):
        try:
            result = await runnable.ainvoke(messages)
            if isinstance(result, schema):
                return result
        except (ValidationError, ValueError) as exc:
            last = exc
    raise AgentError(502, "generation_failed", "The model returned an invalid outline. Please try again.") from last


async def save_file(deps: Deps, req: RequestContext, name: str, data: bytes, mime: str) -> dict:
    key = make_key(req.user_id, req.conversation_id, name)
    await deps.storage.put(key, data, mime)
    attachment = {"fileId": key, "name": name, "mime": mime, "kind": "generated"}
    emit("attachment", attachment={**attachment, "url": file_url(key)})
    return attachment


DOC_SYSTEM = """You write professional, well-structured documents.
Write substantive, specific content (not filler). Plain text only: no Markdown symbols."""

DECK_SYSTEM = """You design concise, professional slide decks.
- 7 to 9 slides. Mostly "bullets" slides with 4-6 short points (max ~15 words each).
- Include exactly one "stats" slide with 3-4 realistic metrics (label + short value).
- The last slide must be type "conclusion" with 3-4 takeaways.
Plain text only: no Markdown symbols."""


async def pdf_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    status("Outlining your document")
    spec = await structured(deps, DocumentSpec, DOC_SYSTEM, f"Write a document about: {state['prompt']}")
    status("Rendering PDF")
    data = await asyncio.to_thread(build_pdf, spec)
    att = await save_file(deps, req, f"{slug(spec.title, 'document')}.pdf", data, PDF_MIME)

    sections = "\n".join(f"- {s.heading}" for s in spec.sections)
    response = (
        f"📄 **{spec.title}**\n\n{spec.introduction[:300]}\n\n**Sections**\n{sections}\n\n"
        f"[Download PDF]({file_url(att['fileId'])})"
    )
    emit_text(req, response)
    return {"response": response, "attachments": [att]}


async def ppt_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    status("Planning slides")
    spec = await structured(deps, DeckSpec, DECK_SYSTEM, f"Create a presentation about: {state['prompt']}")
    status("Designing the deck")
    data = await asyncio.to_thread(build_pptx, spec)
    att = await save_file(deps, req, f"{slug(spec.title, 'presentation')}.pptx", data, PPTX_MIME)

    outline = "\n".join(f"{i}. {s.title}" for i, s in enumerate(spec.slides, start=1))
    response = (
        f"📊 **{spec.title}** — {len(spec.slides) + 1} slides\n\n{outline}\n\n"
        f"[Download presentation]({file_url(att['fileId'])})"
    )
    emit_text(req, response)
    return {"response": response, "attachments": [att]}


IMAGE_PROMPT = """Rewrite the user's request as one vivid image-generation prompt (max 60 words).
Describe subject, setting, composition, lighting, style and color palette.
Return only the prompt text."""

MAX_IMAGE_BYTES = 10 * 1024 * 1024


async def image_agent(state: AgentState, config: RunnableConfig) -> AgentState:
    deps, req = deps_of(config), request_of(config)
    status("Crafting the prompt")
    model = deps.models.get(deps.settings.chat_model, temperature=0.8, max_tokens=200)
    enhanced = (await model.ainvoke([SystemMessage(IMAGE_PROMPT), HumanMessage(state["prompt"])])).content
    enhanced = str(enhanced).strip().strip('"')[:500] or state["prompt"][:500]

    status("Painting")
    url = deps.settings.image_api_url.replace("{prompt}", quote(enhanced, safe=""))
    try:
        resp = await deps.http.get(url, timeout=120, follow_redirects=True)
    except Exception as exc:
        raise AgentError(502, "image_failed", "The image service is not responding. Please try again.") from exc
    data = resp.content
    mime = sniff_mime(data[:16])
    if resp.status_code != 200 or not mime or not mime.startswith("image/") or len(data) > MAX_IMAGE_BYTES:
        raise AgentError(502, "image_failed", "The image service returned an error. Please try again.")

    ext = mime.split("/")[1].replace("jpeg", "jpg")
    att = await save_file(deps, req, f"{slug(state['prompt'], 'image')}.{ext}", data, mime)
    link = file_url(att["fileId"])
    response = f"![Generated image]({link})\n\n*{enhanced}*\n\n[Download image]({link})"
    emit_text(req, response)
    return {"response": response, "attachments": [att]}
