"""FastAPI application for the agent service."""

import hmac
import json
import logging
import sys
import uuid
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from qdrant_client import AsyncQdrantClient
from redis.asyncio import Redis

from app.api import chat as chat_api
from app.api import files as files_api
from app.clients import AuthClient, ChatClient, request_id_var
from app.config import Settings, get_settings
from app.gcp import MetadataTokens
from app.graph.context import Deps
from app.limits import Limiter
from app.llm import Models
from app.memory import ConversationMemory
from app.rag import DocumentIndex
from app.storage import GCSStorage, LocalStorage, S3Storage, Storage


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        entry = {
            "time": self.formatTime(record),
            "level": record.levelname,
            "service": "agent",
            "logger": record.name,
            "msg": record.getMessage(),
            "request_id": request_id_var.get(),
        }
        if record.exc_info:
            entry["error"] = self.formatException(record.exc_info)
        return json.dumps(entry)


def setup_logging() -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter())
    logging.basicConfig(level=logging.INFO, handlers=[handler], force=True)
    logging.getLogger("httpx").setLevel(logging.WARNING)


def make_storage(s: Settings, http: httpx.AsyncClient, tokens: MetadataTokens | None) -> Storage:
    if s.storage_backend == "gcs":
        if tokens is None:
            raise RuntimeError("STORAGE_BACKEND=gcs requires SERVICE_AUTH=google")
        return GCSStorage(s.gcs_bucket, http, tokens)
    if s.storage_backend == "s3":
        return S3Storage(s.s3_bucket, s.s3_endpoint_url, s.s3_region, s.s3_access_key_id, s.s3_secret_access_key)
    return LocalStorage(s.storage_dir)


def make_deps(s: Settings) -> Deps:
    http = httpx.AsyncClient(timeout=httpx.Timeout(30, connect=5))
    redis = Redis.from_url(s.redis_url, decode_responses=True)
    tokens = MetadataTokens(http) if s.service_auth == "google" else None
    chat = ChatClient(s.chat_service_url, s.internal_token, http, tokens)
    qdrant = AsyncQdrantClient(url=s.qdrant_url, api_key=s.qdrant_api_key or None)
    return Deps(
        settings=s,
        models=Models(s),
        chat=chat,
        auth=AuthClient(s.auth_service_url, s.internal_token, http, tokens),
        memory=ConversationMemory(redis, chat),
        limiter=Limiter(redis, s.daily_request_cap),
        storage=make_storage(s, http, tokens),
        index=DocumentIndex(qdrant, s.google_api_key, s.qdrant_collection, s.embedding_model, s.embedding_dim),
        http=http,
    )


def create_app(deps: Deps | None = None) -> FastAPI:
    @asynccontextmanager
    async def lifespan(app: FastAPI):
        if deps is None:
            setup_logging()
            app.state.deps = make_deps(get_settings())
        else:
            app.state.deps = deps
        yield
        await app.state.deps.http.aclose()

    app = FastAPI(title="CortexAI agent service", lifespan=lifespan, docs_url=None, redoc_url=None)

    @app.middleware("http")
    async def internal_only(request: Request, call_next):
        """Every route except /healthz requires the shared internal token."""
        rid = request.headers.get("x-request-id", "")[:64] or uuid.uuid4().hex
        request_id_var.set(rid)
        if request.url.path != "/healthz":
            expected = request.app.state.deps.settings.internal_token.encode()
            got = request.headers.get("x-internal-token", "").encode()
            if not expected or not hmac.compare_digest(got, expected):
                return JSONResponse({"code": "unauthorized", "message": "Invalid internal token."}, status_code=401)
        response = await call_next(request)
        response.headers["X-Request-Id"] = rid
        return response

    @app.get("/healthz")
    async def healthz() -> dict:
        return {"status": "ok"}

    app.include_router(chat_api.router)
    app.include_router(files_api.router)
    return app


app = create_app()
