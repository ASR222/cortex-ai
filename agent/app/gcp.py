"""Google Cloud credentials from the metadata server.

On Cloud Run every service runs as its own service account. The metadata
server (reachable only from inside Google Cloud) hands out short-lived tokens
for that identity:
- identity tokens (JWTs) to call other Cloud Run services, which check the
  caller's roles/run.invoker permission, and
- OAuth access tokens to call Google APIs such as Cloud Storage.
No key files are needed or stored anywhere.
"""

import asyncio
import base64
import json
import time

import httpx

METADATA = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default"
HEADERS = {"Metadata-Flavor": "Google"}
REFRESH_MARGIN = 300  # seconds before expiry


def _jwt_exp(token: str) -> float | None:
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return float(json.loads(base64.urlsafe_b64decode(payload))["exp"])
    except (IndexError, KeyError, ValueError):
        return None


class MetadataTokens:
    def __init__(self, http: httpx.AsyncClient, base: str = METADATA) -> None:
        self._http = http
        self._base = base
        self._cache: dict[str, tuple[str, float]] = {}
        self._lock = asyncio.Lock()

    def _cached(self, key: str) -> str | None:
        hit = self._cache.get(key)
        return hit[0] if hit and time.time() < hit[1] else None

    async def id_token(self, audience: str) -> str:
        key = f"id:{audience}"
        if tok := self._cached(key):
            return tok
        async with self._lock:
            if tok := self._cached(key):
                return tok
            resp = await self._http.get(f"{self._base}/identity", params={"audience": audience}, headers=HEADERS)
            resp.raise_for_status()
            tok = resp.text.strip()
            exp = _jwt_exp(tok) or time.time() + 3600
            self._cache[key] = (tok, exp - REFRESH_MARGIN)
            return tok

    async def access_token(self) -> str:
        if tok := self._cached("access"):
            return tok
        async with self._lock:
            if tok := self._cached("access"):
                return tok
            resp = await self._http.get(f"{self._base}/token", headers=HEADERS)
            resp.raise_for_status()
            data = resp.json()
            self._cache["access"] = (data["access_token"], time.time() + data["expires_in"] - REFRESH_MARGIN)
            return data["access_token"]
