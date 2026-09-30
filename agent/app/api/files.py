"""File downloads and internal cleanup."""

import logging

from fastapi import APIRouter, Request
from fastapi.responses import FileResponse, JSONResponse, RedirectResponse, Response, StreamingResponse

from app.api.chat import user_id_of
from app.errors import AgentError
from app.graph.context import Deps
from app.storage import owned_by

log = logging.getLogger(__name__)
router = APIRouter()

# Only these types are shown in the browser; everything else downloads.
# Uploads were already type-checked by content, and none of these can run script.
INLINE = {"application/pdf", "image/png", "image/jpeg", "image/webp", "image/gif"}
FILE_CSP = "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox"


@router.get("/files/{key:path}")
async def download(key: str, request: Request) -> Response:
    deps: Deps = request.app.state.deps
    try:
        user_id = user_id_of(request)
    except AgentError as e:
        return JSONResponse(e.to_dict(), status_code=e.status)

    not_found = JSONResponse({"code": "not_found", "message": "File not found."}, status_code=404)
    if not owned_by(key, user_id):
        return not_found
    stored = await deps.storage.get(key)
    if stored is None:
        return not_found
    if stored.url:
        return RedirectResponse(stored.url, status_code=302)
    disposition = "inline" if stored.mime in INLINE else "attachment"
    if stored.chunks:
        return StreamingResponse(
            stored.chunks(),
            media_type=stored.mime,
            headers={
                "Content-Disposition": f'{disposition}; filename="{stored.name}"',
                "Content-Security-Policy": FILE_CSP,
            },
        )
    return FileResponse(
        stored.path,
        media_type=stored.mime,
        filename=stored.name,
        content_disposition_type=disposition,
        headers={"Content-Security-Policy": FILE_CSP},
    )


@router.delete("/internal/users/{user_id}/conversations/{conversation_id}")
async def cleanup(user_id: str, conversation_id: str, request: Request) -> Response:
    """Called by the chat service after a conversation is deleted."""
    deps: Deps = request.app.state.deps
    await deps.index.delete_conversation(user_id, conversation_id)
    if owned_by(f"{user_id}/{conversation_id}/x", user_id):
        await deps.storage.delete_prefix(f"{user_id}/{conversation_id}/")
    await deps.memory.forget(conversation_id)
    return Response(status_code=204)
