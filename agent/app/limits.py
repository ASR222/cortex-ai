"""Per-user agent rate limits and a global daily budget, stored in Redis."""

import time
from datetime import UTC, datetime

from redis.asyncio import Redis

from app.costs import PER_MINUTE_LIMIT
from app.errors import AgentError


class Limiter:
    def __init__(self, redis: Redis, daily_cap: int) -> None:
        self._redis = redis
        self._daily_cap = daily_cap

    async def _hit(self, key: str, ttl: int) -> int:
        pipe = self._redis.pipeline()
        pipe.incr(key)
        pipe.expire(key, ttl)
        count, _ = await pipe.execute()
        return int(count)

    async def check(self, user_id: str, agent: str) -> None:
        """Raises AgentError(429) if the user is over the agent's limit, or
        503 if the whole deployment has used up today's budget."""
        limit = PER_MINUTE_LIMIT.get(agent, 10)
        window = int(time.time() // 60)
        count = await self._hit(f"agent-rl:{agent}:{user_id}:{window}", 61)
        if count > limit:
            wait = 60 - int(time.time() % 60)
            raise AgentError(
                429,
                "rate_limited",
                f"You've reached the {agent} limit ({limit} per minute). Try again in {wait}s.",
                "Slow down",
            )

        day = datetime.now(UTC).strftime("%Y%m%d")
        total = await self._hit(f"daily-budget:{day}", 60 * 60 * 26)
        if total > self._daily_cap:
            raise AgentError(
                503,
                "daily_cap",
                "This demo has reached its daily usage limit. Please come back tomorrow.",
                "Daily limit reached",
            )
