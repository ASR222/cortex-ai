"""Short-term conversation memory: a Redis cache in front of the chat service.

The chat service (MongoDB) is the source of truth. Redis keeps the last
MAX_MESSAGES turns of active conversations so each request does not have to
fetch history over HTTP.

Two rules keep the cache consistent:
1. Read history *before* the new turn is added, so the model never sees the
   current prompt twice.
2. Append with RPUSHX, which only writes if the key already exists. A cold
   cache is therefore never seeded with a partial history; the next read
   loads the full history from the database instead.
"""

import json

from redis.asyncio import Redis

from app.clients import ChatClient

MAX_MESSAGES = 20
TTL_SECONDS = 60 * 60 * 24
MAX_CHARS_PER_MESSAGE = 6000


def _key(conversation_id: str) -> str:
    return f"memory:{conversation_id}"


def _compact(role: str, content: str) -> str:
    return json.dumps({"role": role, "content": content[:MAX_CHARS_PER_MESSAGE]})


class ConversationMemory:
    def __init__(self, redis: Redis, chat: ChatClient) -> None:
        self._redis = redis
        self._chat = chat

    async def history(self, user_id: str, conversation_id: str) -> list[dict]:
        key = _key(conversation_id)
        cached = await self._redis.lrange(key, 0, -1)
        if cached:
            return [json.loads(item) for item in cached]

        messages = await self._chat.recent_messages(user_id, conversation_id, MAX_MESSAGES)
        items = [_compact(m["role"], m.get("content", "")) for m in messages]
        if items:
            pipe = self._redis.pipeline()
            pipe.delete(key)
            pipe.rpush(key, *items)
            pipe.expire(key, TTL_SECONDS)
            await pipe.execute()
        return [json.loads(item) for item in items]

    async def append(self, conversation_id: str, turns: list[tuple[str, str]]) -> None:
        key = _key(conversation_id)
        pipe = self._redis.pipeline()
        pipe.rpushx(key, *[_compact(role, content) for role, content in turns])
        pipe.ltrim(key, -MAX_MESSAGES, -1)
        pipe.expire(key, TTL_SECONDS)
        await pipe.execute()

    async def forget(self, conversation_id: str) -> None:
        await self._redis.delete(_key(conversation_id))
