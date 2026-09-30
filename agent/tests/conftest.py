"""Test doubles: a fake LLM plus in-memory stand-ins for the Go services, so the
whole request pipeline (routing, credits, streaming, persistence) runs without
network access or API keys."""

import itertools
import json
from dataclasses import dataclass, field

import fakeredis
import httpx
import pytest
from fastapi.testclient import TestClient
from langchain_core.language_models.fake_chat_models import GenericFakeChatModel
from langchain_core.messages import AIMessage
from langchain_core.runnables import RunnableLambda

from app.config import Settings
from app.errors import AgentError
from app.graph.context import Deps
from app.limits import Limiter
from app.main import create_app
from app.memory import ConversationMemory
from app.storage import LocalStorage

TOKEN = "test-internal-token"
USER = "user-1"
CONV = "a" * 24


class FakeLLM(GenericFakeChatModel):
    """Replies with fixed text; with_structured_output returns a fixed object."""

    structured: object = None

    def with_structured_output(self, schema, **kwargs):
        result = self.structured

        async def _run(_):
            if isinstance(result, Exception):
                raise result
            return result

        return RunnableLambda(lambda _: result, afunc=_run)


def fake_llm(reply: str = "Hello there, friend!", structured: object = None) -> FakeLLM:
    return FakeLLM(messages=itertools.cycle([AIMessage(reply)]), structured=structured)


class FakeModels:
    def __init__(self) -> None:
        self.by_spec: dict[str, FakeLLM] = {}
        self.default = fake_llm()

    def get(self, spec: str, temperature: float = 0.3, max_tokens: int = 4096) -> FakeLLM:
        return self.by_spec.get(spec, self.default)

    raw = get


class FakeChat:
    def __init__(self) -> None:
        self.conversations = {CONV: {"_id": CONV, "userId": USER, "title": "New Chat", "documents": []}}
        self.messages: list[dict] = []

    async def get_conversation(self, user_id: str, conversation_id: str) -> dict:
        conv = self.conversations.get(conversation_id)
        if not conv or conv["userId"] != user_id:
            raise AgentError(404, "not_found", "Conversation not found.")
        return conv

    async def recent_messages(self, user_id: str, conversation_id: str, limit: int = 20) -> list[dict]:
        return self.messages[-limit:]

    async def append_messages(self, user_id: str, conversation_id: str, messages: list[dict]) -> list[dict]:
        saved = [{**m, "_id": f"m{len(self.messages) + i}"} for i, m in enumerate(messages)]
        self.messages += saved
        return saved

    async def add_document(self, user_id: str, conversation_id: str, document: dict) -> None:
        self.conversations[conversation_id]["documents"].append(document)


@dataclass
class FakeAuth:
    balance: int = 50
    reserved: dict[str, int] = field(default_factory=dict)
    committed: list[str] = field(default_factory=list)
    refunded: list[str] = field(default_factory=list)

    async def reserve(self, user_id: str, amount: int, reason: str) -> tuple[str, int]:
        if amount > self.balance:
            raise AgentError(402, "insufficient_credits", "Not enough credits.", "Out of credits")
        self.balance -= amount
        rid = f"r{len(self.reserved)}"
        self.reserved[rid] = amount
        return rid, self.balance

    async def commit(self, reservation_id: str) -> None:
        self.committed.append(reservation_id)

    async def refund(self, reservation_id: str) -> int:
        self.refunded.append(reservation_id)
        self.balance += self.reserved[reservation_id]
        return self.balance


class FakeIndex:
    def __init__(self) -> None:
        self.ingested: list[str] = []
        self.deleted: list[str] = []
        self.results: list = []

    async def ingest(self, user_id, conversation_id, file_id, name, pdf) -> dict:
        self.ingested.append(file_id)
        return {"fileId": file_id, "name": name, "pages": 1, "chunks": 1}

    async def search(self, user_id, conversation_id, query, k=6) -> list:
        return self.results

    async def delete_conversation(self, user_id: str, conversation_id: str) -> None:
        self.deleted.append(conversation_id)


@dataclass
class Env:
    client: TestClient
    deps: Deps
    models: FakeModels
    chat: FakeChat
    auth: FakeAuth
    index: FakeIndex
    http_routes: dict

    def post_chat(self, prompt: str = "hi", agent: str = "auto", files=None, conv: str = CONV, user: str = USER):
        return self.client.post(
            "/chat",
            data={"prompt": prompt, "conversationId": conv, "agent": agent},
            files=files,
            headers={"X-Internal-Token": TOKEN, "X-User-Id": user},
        )


def parse_sse(body: str) -> list[tuple[str, dict]]:
    events = []
    for block in body.strip().split("\n\n"):
        lines = dict(line.split(": ", 1) for line in block.splitlines() if ": " in line)
        if "event" in lines:
            events.append((lines["event"], json.loads(lines["data"])))
    return events


@pytest.fixture
def env(tmp_path) -> Env:
    settings = Settings(
        internal_token=TOKEN,
        redis_url="redis://unused",
        chat_service_url="http://chat",
        auth_service_url="http://auth",
        groq_api_key="x",
        tavily_api_key="x",
        storage_dir=str(tmp_path / "files"),
        _env_file=None,
    )
    redis = fakeredis.FakeAsyncRedis(decode_responses=True)
    routes: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        for prefix, fn in routes.items():
            if str(request.url).startswith(prefix):
                return fn(request)
        return httpx.Response(404)

    models, chat, auth, index = FakeModels(), FakeChat(), FakeAuth(), FakeIndex()
    deps = Deps(
        settings=settings,
        models=models,  # type: ignore[arg-type]
        chat=chat,  # type: ignore[arg-type]
        auth=auth,  # type: ignore[arg-type]
        memory=ConversationMemory(redis, chat),  # type: ignore[arg-type]
        limiter=Limiter(redis, daily_cap=1000),
        storage=LocalStorage(settings.storage_dir),
        index=index,  # type: ignore[arg-type]
        http=httpx.AsyncClient(transport=httpx.MockTransport(handler)),
    )
    with TestClient(create_app(deps)) as client:
        yield Env(client, deps, models, chat, auth, index, routes)
