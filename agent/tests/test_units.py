"""Unit tests for parsing, storage, uploads, memory and document rendering."""

from io import BytesIO

import fakeredis
import pytest
from pptx import Presentation
from pypdf import PdfReader

from app.agents.coding import parse_files
from app.documents.pdf_builder import build_pdf
from app.documents.ppt_builder import build_pptx
from app.documents.specs import DeckSpec, DocumentSpec, Section, Slide, Stat
from app.memory import ConversationMemory
from app.rag import chunk_pages
from app.storage import LocalStorage, display_name, make_key, owned_by, safe_filename
from app.uploads import sniff_mime


def test_parse_files_strips_fences_and_unsafe_paths():
    text = (
        "FILE: index.html\n```html\n<h1>Hi</h1>\n```\n"
        "FILE: ../../etc/passwd\nroot\n"
        "FILE: /abs.txt\nx\n"
        "FILE: src/app.js\nconsole.log(1)\n"
    )
    files = parse_files(text)
    assert [f["name"] for f in files] == ["index.html", "src/app.js"]
    assert files[0]["content"] == "<h1>Hi</h1>\n"


@pytest.mark.parametrize(
    ("data", "mime"),
    [
        (b"%PDF-1.7", "application/pdf"),
        (b"\x89PNG\r\n\x1a\n....", "image/png"),
        (b"\xff\xd8\xff\xe0", "image/jpeg"),
        (b"RIFF\x00\x00\x00\x00WEBP", "image/webp"),
        (b"GIF89a", "image/gif"),
        (b"<html>", None),
        (b"MZ\x90\x00", None),
    ],
)
def test_sniff_mime(data, mime):
    assert sniff_mime(data) == mime


def test_keys_and_ownership():
    key = make_key("u1", "c1", "../../My Report (final).pdf")
    assert key.startswith("u1/c1/") and ".." not in key
    assert display_name(key) == "My-Report-final-.pdf"
    assert owned_by(key, "u1")
    assert not owned_by(key, "u2")
    assert not owned_by("u1/../u2/x", "u1")
    assert not owned_by("u1/c1", "u1")
    assert safe_filename("") == "file"


async def test_local_storage_roundtrip(tmp_path):
    s = LocalStorage(str(tmp_path))
    key = make_key("u1", "c1", "a.pdf")
    await s.put(key, b"%PDF-data", "application/pdf")
    got = await s.get(key)
    assert got and got.mime == "application/pdf" and got.path.read_bytes() == b"%PDF-data"
    await s.delete_prefix("u1/c1/")
    assert await s.get(key) is None
    with pytest.raises(ValueError):
        await s.get("../outside")


class StubChat:
    def __init__(self, messages):
        self.messages = messages
        self.calls = 0

    async def recent_messages(self, user_id, conversation_id, limit=20):
        self.calls += 1
        return self.messages


async def test_memory_loads_from_db_then_caches():
    redis = fakeredis.FakeAsyncRedis(decode_responses=True)
    chat = StubChat([{"role": "user", "content": "a"}, {"role": "assistant", "content": "b"}])
    mem = ConversationMemory(redis, chat)
    assert [m["content"] for m in await mem.history("u", "c")] == ["a", "b"]
    await mem.append("c", [("user", "q"), ("assistant", "r")])
    assert [m["content"] for m in await mem.history("u", "c")] == ["a", "b", "q", "r"]
    assert chat.calls == 1


async def test_memory_append_never_seeds_a_cold_cache():
    redis = fakeredis.FakeAsyncRedis(decode_responses=True)
    chat = StubChat([])
    mem = ConversationMemory(redis, chat)
    await mem.append("c", [("user", "q"), ("assistant", "r")])
    assert await redis.exists("memory:c") == 0


def test_chunk_pages_keeps_page_numbers():
    chunks = chunk_pages(["short", "x" * 2500, ""])
    assert {page for page, _ in chunks} == {2}
    assert len(chunks) >= 3


def test_build_pdf_is_valid():
    spec = DocumentSpec(
        title="Redis <Internals>",
        introduction="Intro & more",
        sections=[Section(heading="A", paragraphs=["p"], bullets=["b1"]), Section(heading="B", paragraphs=["q"])],
        conclusion="Done",
    )
    reader = PdfReader(BytesIO(build_pdf(spec)))
    text = "".join(p.extract_text() for p in reader.pages)
    assert "Redis <Internals>" in text and "Conclusion" in text


def test_build_pptx_is_valid():
    spec = DeckSpec(
        title="Deck",
        subtitle="Sub",
        slides=[
            Slide(type="bullets", title="One", bullets=["a", "b"]),
            Slide(type="stats", title="Two", stats=[Stat(label="Users", value="1M")]),
            Slide(type="stats", title="Empty stats", bullets=["fallback"]),
            Slide(type="conclusion", title="End", bullets=["x"]),
        ],
    )
    prs = Presentation(BytesIO(build_pptx(spec)))
    assert len(prs.slides) == 5
