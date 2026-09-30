"""HTTP clients for the Go services (chat and auth).

Every call carries the shared internal token and the request id, so logs
from all services can be joined on one id.
"""

from contextvars import ContextVar
from typing import Any
from urllib.parse import quote

import httpx

from app.errors import AgentError

request_id_var: ContextVar[str] = ContextVar("request_id", default="")


class ServiceClient:
    def __init__(self, base_url: str, token: str, client: httpx.AsyncClient) -> None:
        self._base = base_url.rstrip("/")
        self._token = token
        self._http = client

    async def request(self, method: str, path: str, json: Any = None) -> Any:
        headers = {"X-Internal-Token": self._token}
        if rid := request_id_var.get():
            headers["X-Request-Id"] = rid
        resp = await self._http.request(method, self._base + path, json=json, headers=headers)
        if resp.status_code >= 300:
            try:
                body = resp.json()
            except ValueError:
                body = {}
            raise AgentError(
                resp.status_code,
                body.get("code", "upstream_error"),
                body.get("message", "Upstream service error."),
                body.get("title", ""),
            )
        if resp.status_code == 204 or not resp.content:
            return None
        return resp.json()


def _conv_path(user_id: str, conversation_id: str) -> str:
    return f"/internal/users/{quote(user_id, safe='')}/conversations/{quote(conversation_id, safe='')}"


class ChatClient(ServiceClient):
    async def get_conversation(self, user_id: str, conversation_id: str) -> dict:
        """Returns the conversation, or raises 404 if the user does not own it."""
        return await self.request("GET", _conv_path(user_id, conversation_id) + "/")

    async def recent_messages(self, user_id: str, conversation_id: str, limit: int = 20) -> list[dict]:
        return await self.request("GET", f"{_conv_path(user_id, conversation_id)}/messages?limit={limit}")

    async def append_messages(self, user_id: str, conversation_id: str, messages: list[dict]) -> list[dict]:
        return await self.request("POST", _conv_path(user_id, conversation_id) + "/messages", {"messages": messages})

    async def add_document(self, user_id: str, conversation_id: str, document: dict) -> None:
        await self.request("POST", _conv_path(user_id, conversation_id) + "/documents", document)


class AuthClient(ServiceClient):
    async def reserve(self, user_id: str, amount: int, reason: str) -> tuple[str, int]:
        data = await self.request(
            "POST", "/internal/credits/reserve", {"userId": user_id, "amount": amount, "reason": reason}
        )
        return data["reservationId"], data["credits"]

    async def commit(self, reservation_id: str) -> None:
        await self.request("POST", f"/internal/credits/{quote(reservation_id, safe='')}/commit")

    async def refund(self, reservation_id: str) -> int:
        data = await self.request("POST", f"/internal/credits/{quote(reservation_id, safe='')}/refund")
        return data["credits"]
