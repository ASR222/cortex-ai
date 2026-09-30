"""Tests for Google Cloud integration: metadata tokens, Cloud Storage and
identity tokens on service-to-service calls."""

import base64
import json
import time

import httpx

from app.clients import AuthClient
from app.gcp import MetadataTokens
from app.storage import GCSStorage


def jwt(exp: float) -> str:
    payload = base64.urlsafe_b64encode(json.dumps({"exp": int(exp)}).encode()).decode().rstrip("=")
    return f"h.{payload}.s"


def metadata_transport(calls: list[str]):
    def handler(req: httpx.Request) -> httpx.Response:
        assert req.headers["Metadata-Flavor"] == "Google"
        calls.append(str(req.url))
        if req.url.path.endswith("/identity"):
            return httpx.Response(200, text=jwt(time.time() + 3600) + req.url.params["audience"])
        return httpx.Response(200, json={"access_token": "ya29.token", "expires_in": 3599})

    return handler


async def test_metadata_tokens_are_cached():
    calls: list[str] = []
    tokens = MetadataTokens(httpx.AsyncClient(transport=httpx.MockTransport(metadata_transport(calls))))
    a1 = await tokens.id_token("https://auth")
    a2 = await tokens.id_token("https://auth")
    b = await tokens.id_token("https://chat")
    assert a1 == a2 and a1 != b
    assert await tokens.access_token() == await tokens.access_token() == "ya29.token"
    assert len(calls) == 3


async def test_service_client_sends_identity_token():
    seen = {}

    def handler(req: httpx.Request) -> httpx.Response:
        if "metadata" in req.url.host:
            return metadata_transport([])(req)
        seen.update(req.headers)
        return httpx.Response(200, json={"reservationId": "r1", "credits": 9})

    http = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    client = AuthClient("https://auth.run.app", "secret", http, MetadataTokens(http))
    await client.reserve("u1", 1, "chat")
    assert seen["authorization"].startswith("Bearer ") and seen["authorization"].endswith("https://auth.run.app")
    assert seen["x-internal-token"] == "secret"


async def test_gcs_storage_roundtrip():
    objects: dict[str, tuple[bytes, str]] = {}

    def handler(req: httpx.Request) -> httpx.Response:
        if "metadata" in req.url.host:
            return metadata_transport([])(req)
        assert req.headers["Authorization"] == "Bearer ya29.token"
        path = req.url.path
        if req.method == "POST" and path == "/upload/storage/v1/b/bkt/o":
            objects[req.url.params["name"]] = (req.content, req.headers["Content-Type"])
            return httpx.Response(200, json={})
        if req.method == "GET" and path == "/storage/v1/b/bkt/o":
            names = [n for n in objects if n.startswith(req.url.params["prefix"])]
            return httpx.Response(200, json={"items": [{"name": n} for n in names]})
        name = path.removeprefix("/storage/v1/b/bkt/o/")  # httpx decodes %2F in .path
        if req.method == "DELETE":
            objects.pop(name, None)
            return httpx.Response(204)
        if name not in objects:
            return httpx.Response(404)
        data, mime = objects[name]
        if req.url.params.get("alt") == "media":
            return httpx.Response(200, content=data)
        return httpx.Response(200, json={"contentType": mime})

    http = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    store = GCSStorage("bkt", http, MetadataTokens(http))

    await store.put("u1/c1/abc-report.pdf", b"%PDF-data", "application/pdf")
    got = await store.get("u1/c1/abc-report.pdf")
    assert got and got.mime == "application/pdf" and got.name == "report.pdf"
    assert b"".join([c async for c in got.chunks()]) == b"%PDF-data"

    await store.delete_prefix("u1/c1/")
    assert await store.get("u1/c1/abc-report.pdf") is None
