"""Document retrieval (RAG) over uploaded PDFs.

Design:
- One Qdrant collection for everyone. Each chunk carries user_id,
  conversation_id and file_id in its payload, and every search filters on
  user_id AND conversation_id. Payload indexes keep those filters fast.
  (A collection per upload, as in the original prototype, does not scale:
  collections are heavyweight in Qdrant.)
- A PDF is embedded once, at upload time. Follow-up questions in the same
  conversation reuse the stored vectors.
- Chunks keep their page number so answers can cite "report.pdf, p. 4".
- Deleting a conversation deletes its vectors.
"""

import asyncio
import uuid
from dataclasses import dataclass
from io import BytesIO

from google import genai
from google.genai import types
from langchain_text_splitters import RecursiveCharacterTextSplitter
from pypdf import PdfReader
from qdrant_client import AsyncQdrantClient, models

from app.errors import AgentError

MAX_PAGES = 300
EMBED_BATCH = 100


@dataclass
class Chunk:
    text: str
    file_id: str
    name: str
    page: int
    score: float = 0.0


def extract_pages(pdf: bytes) -> list[str]:
    try:
        reader = PdfReader(BytesIO(pdf))
    except Exception as exc:  # pypdf raises many exception types
        raise AgentError(400, "bad_pdf", "That PDF could not be read.") from exc
    if reader.is_encrypted:
        raise AgentError(400, "bad_pdf", "Password-protected PDFs are not supported.")
    if len(reader.pages) > MAX_PAGES:
        raise AgentError(400, "bad_pdf", f"PDFs are limited to {MAX_PAGES} pages.")
    return [(page.extract_text() or "").strip() for page in reader.pages]


def chunk_pages(pages: list[str]) -> list[tuple[int, str]]:
    """Splits page text into overlapping chunks, keeping page numbers."""
    splitter = RecursiveCharacterTextSplitter(chunk_size=1000, chunk_overlap=150)
    out: list[tuple[int, str]] = []
    for number, text in enumerate(pages, start=1):
        for piece in splitter.split_text(text):
            if len(piece.strip()) > 20:
                out.append((number, piece))
    return out


class DocumentIndex:
    def __init__(
        self, qdrant: AsyncQdrantClient, google_api_key: str, collection: str, embedding_model: str, dim: int
    ) -> None:
        self._q = qdrant
        self._collection = collection
        self._model = embedding_model
        self._dim = dim
        self._genai = genai.Client(api_key=google_api_key) if google_api_key else None
        self._ready = False

    async def ensure_collection(self) -> None:
        if self._ready:
            return
        if not await self._q.collection_exists(self._collection):
            await self._q.create_collection(
                self._collection,
                vectors_config=models.VectorParams(size=self._dim, distance=models.Distance.COSINE),
            )
            for field in ("user_id", "conversation_id", "file_id"):
                await self._q.create_payload_index(
                    self._collection, field, field_schema=models.PayloadSchemaType.KEYWORD
                )
        self._ready = True

    async def _embed(self, texts: list[str], task: str) -> list[list[float]]:
        if self._genai is None:
            raise AgentError(503, "not_configured", "Document Q&A needs a Google API key.")
        vectors: list[list[float]] = []
        for i in range(0, len(texts), EMBED_BATCH):
            resp = await self._genai.aio.models.embed_content(
                model=self._model,
                contents=texts[i : i + EMBED_BATCH],
                config=types.EmbedContentConfig(task_type=task, output_dimensionality=self._dim),
            )
            vectors += [e.values for e in resp.embeddings]
        return vectors

    async def ingest(self, user_id: str, conversation_id: str, file_id: str, name: str, pdf: bytes) -> dict:
        pages = await asyncio.to_thread(extract_pages, pdf)
        chunks = chunk_pages(pages)
        if not chunks:
            raise AgentError(
                400,
                "no_text",
                "No readable text was found in that PDF. Scanned documents are not supported yet.",
                "Nothing to read",
            )
        await self.ensure_collection()
        vectors = await self._embed([text for _, text in chunks], "RETRIEVAL_DOCUMENT")
        points = [
            models.PointStruct(
                id=str(uuid.uuid4()),
                vector=vec,
                payload={
                    "user_id": user_id,
                    "conversation_id": conversation_id,
                    "file_id": file_id,
                    "name": name,
                    "page": page,
                    "text": text,
                },
            )
            for (page, text), vec in zip(chunks, vectors, strict=True)
        ]
        for i in range(0, len(points), 256):
            await self._q.upsert(self._collection, points=points[i : i + 256])
        return {"fileId": file_id, "name": name, "pages": len(pages), "chunks": len(points)}

    async def search(self, user_id: str, conversation_id: str, query: str, k: int = 6) -> list[Chunk]:
        await self.ensure_collection()
        [vector] = await self._embed([query], "RETRIEVAL_QUERY")
        result = await self._q.query_points(
            self._collection,
            query=vector,
            limit=k,
            query_filter=models.Filter(
                must=[
                    models.FieldCondition(key="user_id", match=models.MatchValue(value=user_id)),
                    models.FieldCondition(key="conversation_id", match=models.MatchValue(value=conversation_id)),
                ]
            ),
            with_payload=True,
        )
        return [
            Chunk(
                text=p.payload["text"],
                file_id=p.payload["file_id"],
                name=p.payload["name"],
                page=p.payload["page"],
                score=p.score,
            )
            for p in result.points
        ]

    async def delete_conversation(self, user_id: str, conversation_id: str) -> None:
        await self.ensure_collection()
        await self._q.delete(
            self._collection,
            points_selector=models.FilterSelector(
                filter=models.Filter(
                    must=[
                        models.FieldCondition(key="user_id", match=models.MatchValue(value=user_id)),
                        models.FieldCondition(key="conversation_id", match=models.MatchValue(value=conversation_id)),
                    ]
                )
            ),
        )
