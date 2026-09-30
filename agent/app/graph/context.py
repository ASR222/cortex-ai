"""Objects shared by graph nodes: long-lived dependencies and per-request context."""

from dataclasses import dataclass, field
from typing import Any, TypedDict

import httpx
from langchain_core.runnables import RunnableConfig
from langgraph.config import get_stream_writer

from app.clients import AuthClient, ChatClient
from app.config import Settings
from app.limits import Limiter
from app.llm import Models
from app.memory import ConversationMemory
from app.rag import DocumentIndex
from app.storage import Storage


@dataclass
class Deps:
    """Process-wide dependencies, created once at startup."""

    settings: Settings
    models: Models
    chat: ChatClient
    auth: AuthClient
    memory: ConversationMemory
    limiter: Limiter
    storage: Storage
    index: DocumentIndex
    http: httpx.AsyncClient


@dataclass
class Upload:
    key: str
    name: str
    mime: str
    data: bytes


@dataclass
class RequestContext:
    """Mutable per-request data that must survive a node raising an error
    (for example, which credit reservation to refund)."""

    user_id: str
    conversation_id: str
    upload: Upload | None = None
    reservation_id: str | None = None
    credits: int | None = None
    streamed_chars: int = 0
    extra: dict[str, Any] = field(default_factory=dict)


class AgentState(TypedDict, total=False):
    prompt: str
    requested_agent: str
    agent: str
    history: list[dict]
    documents: list[dict]
    response: str
    artifacts: list[dict]
    attachments: list[dict]
    images: list[str]
    sources: list[dict]


def deps_of(config: RunnableConfig) -> Deps:
    return config["configurable"]["deps"]


def request_of(config: RunnableConfig) -> RequestContext:
    return config["configurable"]["request"]


# --- streaming events -------------------------------------------------------
# Nodes push UI events through LangGraph's custom stream. The API layer turns
# each one into a Server-Sent Event.


def emit(event: str, **data: Any) -> None:
    get_stream_writer()({"event": event, "data": data})


def status(message: str) -> None:
    emit("status", message=message)
