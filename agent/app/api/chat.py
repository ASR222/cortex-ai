"""POST /chat — runs one conversation turn and streams it as Server-Sent Events.

Event types sent to the browser:
  meta        {agent, cost}              which agent was chosen
  status      {message}                  progress ("Searching the web")
  token       {text}                     a piece of the answer
  sources     {sources, images}          citations
  artifact    {artifact}                 generated code project
  attachment  {attachment}               generated/uploaded file
  done        {messageId, credits, agent}
  error       {status, code, title, message, credits?}

Credit lifecycle: reserved in the graph's guard node, then committed here
after the answer is produced, or refunded if anything fails.
"""

import asyncio
import json
import logging
import re
from collections.abc import AsyncIterator

from fastapi import APIRouter, File, Form, Request, UploadFile
from fastapi.responses import JSONResponse, StreamingResponse

from app.costs import AGENTS
from app.errors import AgentError, bad_request
from app.graph.context import Deps, RequestContext, Upload
from app.graph.graph import graph
from app.storage import make_key, safe_filename
from app.uploads import is_image, sniff_mime

log = logging.getLogger(__name__)
router = APIRouter()

OBJECT_ID = re.compile(r"^[0-9a-f]{24}$")
VALID_AGENTS = {"auto", *AGENTS}


def sse(event: str, data: dict) -> bytes:
    return f"event: {event}\ndata: {json.dumps(data, ensure_ascii=False)}\n\n".encode()


def user_id_of(request: Request) -> str:
    uid = request.headers.get("x-user-id", "")
    if not uid:
        raise AgentError(401, "unauthorized", "Please sign in to continue.")
    return uid


async def read_upload(file: UploadFile, max_bytes: int) -> bytes:
    data = await file.read(max_bytes + 1)
    if len(data) > max_bytes:
        raise AgentError(413, "too_large", f"Files are limited to {max_bytes // (1024 * 1024)} MB.")
    return data


@router.post("/chat")
async def chat(
    request: Request,
    conversationId: str = Form(...),  # noqa: N803 - matches the frontend field name
    prompt: str = Form(""),
    agent: str = Form("auto"),
    file: UploadFile | None = File(None),
):
    deps: Deps = request.app.state.deps
    try:
        user_id = user_id_of(request)
        prompt = prompt.strip()
        if not OBJECT_ID.match(conversationId):
            raise AgentError(404, "not_found", "Conversation not found.")
        if agent not in VALID_AGENTS:
            raise bad_request("Unknown agent.")
        if not prompt and file is None:
            raise bad_request("Type a message or attach a file.")
        if len(prompt) > deps.settings.max_prompt_chars:
            raise bad_request(f"Messages are limited to {deps.settings.max_prompt_chars} characters.")

        # Ownership check: 404 unless this user owns the conversation.
        conversation = await deps.chat.get_conversation(user_id, conversationId)

        upload = None
        if file is not None:
            data = await read_upload(file, deps.settings.max_upload_mb * 1024 * 1024)
            mime = sniff_mime(data[:16])
            if mime is None:
                raise bad_request("Only PDF and image files (PNG, JPEG, WebP, GIF) are supported.")
            name = safe_filename(file.filename or "", "upload")
            upload = Upload(key=make_key(user_id, conversationId, name), name=name, mime=mime, data=data)
    except AgentError as e:
        return JSONResponse(e.to_dict(), status_code=e.status)

    rc = RequestContext(user_id=user_id, conversation_id=conversationId, upload=upload)
    return StreamingResponse(
        run_turn(deps, rc, prompt, agent, conversation),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


async def ingest_upload(deps: Deps, rc: RequestContext, documents: list[dict]) -> AsyncIterator[bytes]:
    """Stores the upload and, for PDFs, indexes it for Q&A."""
    up = rc.upload
    assert up is not None
    await deps.storage.put(up.key, up.data, up.mime)
    attachment = {"fileId": up.key, "name": up.name, "mime": up.mime, "kind": "upload"}
    rc.extra["user_attachments"] = [attachment]
    if is_image(up.mime):
        return
    await deps.limiter.check(rc.user_id, "upload")
    yield sse("status", {"message": f"Reading {up.name}"})
    doc = await deps.index.ingest(rc.user_id, rc.conversation_id, up.key, up.name, up.data)
    await deps.chat.add_document(rc.user_id, rc.conversation_id, doc)
    documents.append(doc)


async def settle(deps: Deps, rc: RequestContext, commit: bool) -> None:
    if not rc.reservation_id:
        return
    reservation, rc.reservation_id = rc.reservation_id, None
    try:
        if commit:
            await deps.auth.commit(reservation)
            rc.extra["charged"] = True
        else:
            rc.credits = await deps.auth.refund(reservation)
    except Exception:
        log.exception("failed to settle reservation %s (commit=%s)", reservation, commit)


async def run_turn(deps: Deps, rc: RequestContext, prompt: str, agent: str, conversation: dict) -> AsyncIterator[bytes]:
    documents = list(conversation.get("documents") or [])
    try:
        if rc.upload:
            async for event in ingest_upload(deps, rc, documents):
                yield event

        history = await deps.memory.history(rc.user_id, rc.conversation_id)
        state = {"prompt": prompt, "requested_agent": agent, "history": history, "documents": documents}
        config = {"configurable": {"deps": deps, "request": rc}}

        final: dict = {}
        async for mode, chunk in graph.astream(state, config, stream_mode=["custom", "values"]):
            if mode == "custom":
                yield sse(chunk["event"], chunk["data"])
            else:
                final = chunk

        response = final.get("response", "")
        chosen = final.get("agent", "chat")
        await settle(deps, rc, commit=True)

        user_msg = {"role": "user", "content": prompt, "attachments": rc.extra.get("user_attachments", [])}
        assistant_msg = {
            "role": "assistant",
            "content": response,
            "agent": chosen,
            "artifacts": final.get("artifacts", []),
            "attachments": final.get("attachments", []),
            "images": final.get("images", []),
            "sources": final.get("sources", []),
        }
        saved = await deps.chat.append_messages(rc.user_id, rc.conversation_id, [user_msg, assistant_msg])
        await deps.memory.append(rc.conversation_id, [("user", prompt), ("assistant", response)])
        yield sse("done", {"messageId": saved[-1]["_id"], "credits": rc.credits, "agent": chosen})

    except AgentError as e:
        await settle(deps, rc, commit=False)
        yield sse("error", {**e.to_dict(), "credits": rc.credits})
    except asyncio.CancelledError:
        # The client disconnected. If they already received part of an
        # answer the work was done, so it is charged; otherwise refunded.
        await asyncio.shield(settle(deps, rc, commit=rc.streamed_chars > 0))
        raise
    except Exception:
        log.exception("agent turn failed")
        charged = rc.extra.get("charged", False)
        await settle(deps, rc, commit=False)
        note = "" if charged else " You have not been charged."
        yield sse(
            "error",
            {
                "status": 500,
                "code": "internal",
                "title": "Something went wrong",
                "message": f"The assistant hit an unexpected error.{note} Please try again.",
                "credits": rc.credits,
            },
        )
