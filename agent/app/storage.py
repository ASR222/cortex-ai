"""File storage for uploads and generated files.

Keys look like  <userId>/<conversationId>/<random>-<name>.  Namespacing by
user makes ownership checks a prefix comparison, and namespacing by
conversation lets a deleted conversation's files be removed in one sweep.
Files are never served from a public URL; downloads go through the gateway,
which checks the session, and then through `/files/{key}` here, which checks
the owner.
"""

import asyncio
import json
import re
import shutil
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

_SAFE = re.compile(r"[^A-Za-z0-9._-]+")


def safe_filename(name: str, default: str = "file") -> str:
    name = _SAFE.sub("-", Path(name or "").name).strip(".-")[:80]
    return name or default


def make_key(user_id: str, conversation_id: str, filename: str) -> str:
    return f"{user_id}/{conversation_id}/{uuid.uuid4().hex[:12]}-{safe_filename(filename)}"


def display_name(key: str) -> str:
    """The user-facing file name (without the random prefix)."""
    last = key.rsplit("/", 1)[-1]
    return last.split("-", 1)[1] if "-" in last else last


def owned_by(key: str, user_id: str) -> bool:
    parts = key.split("/")
    return (
        len(parts) == 3 and parts[0] == user_id and all(p and p not in (".", "..") for p in parts) and "\\" not in key
    )


@dataclass
class StoredFile:
    mime: str
    name: str
    path: Path | None = None  # local backend
    url: str | None = None  # s3 backend (short-lived presigned URL)


class Storage(Protocol):
    async def put(self, key: str, data: bytes, mime: str) -> None: ...
    async def get(self, key: str) -> StoredFile | None: ...
    async def delete_prefix(self, prefix: str) -> None: ...


class LocalStorage:
    """Stores files on disk (a Docker volume). Zero cost, zero setup."""

    def __init__(self, root: str) -> None:
        self._root = Path(root).resolve()
        self._root.mkdir(parents=True, exist_ok=True)

    def _path(self, key: str) -> Path:
        p = (self._root / key).resolve()
        if not p.is_relative_to(self._root):
            raise ValueError("invalid key")
        return p

    async def put(self, key: str, data: bytes, mime: str) -> None:
        def write() -> None:
            p = self._path(key)
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(data)
            p.with_name(p.name + ".meta").write_text(json.dumps({"mime": mime}))

        await asyncio.to_thread(write)

    async def get(self, key: str) -> StoredFile | None:
        p = self._path(key)
        if not p.is_file():
            return None
        meta = json.loads(p.with_name(p.name + ".meta").read_text())
        return StoredFile(mime=meta["mime"], name=display_name(key), path=p)

    async def delete_prefix(self, prefix: str) -> None:
        p = self._path(prefix.rstrip("/"))
        await asyncio.to_thread(shutil.rmtree, p, True)


class S3Storage:
    """Stores files in any S3-compatible bucket (AWS S3, Cloudflare R2, MinIO)."""

    def __init__(self, bucket: str, endpoint_url: str, region: str, key_id: str, secret: str) -> None:
        import boto3

        self._bucket = bucket
        self._s3 = boto3.client(
            "s3",
            endpoint_url=endpoint_url or None,
            region_name=region,
            aws_access_key_id=key_id,
            aws_secret_access_key=secret,
        )

    async def put(self, key: str, data: bytes, mime: str) -> None:
        await asyncio.to_thread(self._s3.put_object, Bucket=self._bucket, Key=key, Body=data, ContentType=mime)

    async def get(self, key: str) -> StoredFile | None:
        try:
            head = await asyncio.to_thread(self._s3.head_object, Bucket=self._bucket, Key=key)
        except self._s3.exceptions.ClientError:
            return None
        url = await asyncio.to_thread(
            self._s3.generate_presigned_url,
            "get_object",
            Params={"Bucket": self._bucket, "Key": key},
            ExpiresIn=300,
        )
        return StoredFile(mime=head.get("ContentType", "application/octet-stream"), name=display_name(key), url=url)

    async def delete_prefix(self, prefix: str) -> None:
        def delete() -> None:
            paginator = self._s3.get_paginator("list_objects_v2")
            for page in paginator.paginate(Bucket=self._bucket, Prefix=prefix):
                objs = [{"Key": o["Key"]} for o in page.get("Contents", [])]
                if objs:
                    self._s3.delete_objects(Bucket=self._bucket, Delete={"Objects": objs})

        await asyncio.to_thread(delete)
