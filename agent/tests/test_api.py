"""End-to-end tests of POST /chat through the real graph with fake services."""

import httpx

from app.documents.specs import DeckSpec, Slide
from app.graph.router import Route
from app.rag import Chunk
from tests.conftest import CONV, TOKEN, USER, fake_llm, parse_sse


def test_requires_internal_token(env):
    resp = env.client.post("/chat", data={"prompt": "hi", "conversationId": CONV})
    assert resp.status_code == 401
    assert env.client.get("/healthz").status_code == 200


def test_chat_turn_streams_commits_and_saves(env):
    env.models.default = fake_llm("Goroutines are lightweight threads.")
    resp = env.post_chat("What is a goroutine?", agent="chat")
    assert resp.status_code == 200
    events = parse_sse(resp.text)
    kinds = [e for e, _ in events]

    assert kinds[0] == "meta" and events[0][1]["agent"] == "chat"
    assert "token" in kinds and kinds[-1] == "done"
    streamed = "".join(d["text"] for e, d in events if e == "token")
    assert streamed == "Goroutines are lightweight threads."

    assert env.auth.committed == ["r0"] and env.auth.refunded == []
    assert events[-1][1]["credits"] == 49
    assert [m["role"] for m in env.chat.messages] == ["user", "assistant"]
    assert env.chat.messages[1]["content"] == streamed


def test_history_is_read_before_the_new_turn(env):
    env.post_chat("first question", agent="chat")
    seen = {}

    class Spy:
        async def astream(self, messages):
            seen["messages"] = [m.content for m in messages]
            yield type("C", (), {"content": "ok"})()

    env.models.by_spec[env.deps.settings.chat_model] = Spy()
    env.post_chat("second question", agent="chat")
    contents = seen["messages"][1:]  # skip system prompt
    assert contents.count("second question") == 1
    assert contents[0] == "first question"


def test_unowned_conversation_is_404_before_streaming(env):
    resp = env.post_chat("hi", user="mallory")
    assert resp.status_code == 404
    assert env.auth.reserved == {}


def test_insufficient_credits_is_an_error_event(env):
    env.auth.balance = 0
    events = parse_sse(env.post_chat("hi", agent="chat").text)
    assert events[-1][0] == "error"
    assert events[-1][1]["code"] == "insufficient_credits"
    assert env.chat.messages == []


def test_agent_failure_refunds(env):
    class Broken:
        async def astream(self, messages):
            raise RuntimeError("provider down")
            yield  # pragma: no cover

    env.models.by_spec[env.deps.settings.chat_model] = Broken()
    events = parse_sse(env.post_chat("hi", agent="chat").text)
    assert events[-1][0] == "error"
    assert env.auth.refunded == ["r0"] and env.auth.committed == []
    assert events[-1][1]["credits"] == 50
    assert "not been charged" in events[-1][1]["message"]


def test_rate_limit(env):
    for _ in range(3):
        assert parse_sse(env.post_chat("draw a cat", agent="image").text)[-1][0] in ("done", "error")
    events = parse_sse(env.post_chat("draw a cat", agent="image").text)
    assert events[-1][1]["code"] == "rate_limited"


def test_auto_routing_uses_classifier(env):
    env.models.by_spec[env.deps.settings.router_model] = fake_llm(structured=Route(agent="coding"))
    env.models.default = fake_llm("FILE: index.html\n<h1>Hi</h1>\nFILE: style.css\nh1 { color: red; }\n")
    events = parse_sse(env.post_chat("build a landing page").text)
    assert events[0][1]["agent"] == "coding"
    artifact = next(d["artifact"] for e, d in events if e == "artifact")
    assert [f["name"] for f in artifact["files"]] == ["index.html", "style.css"]
    statuses = [d["message"] for e, d in events if e == "status"]
    assert "Writing index.html" in statuses
    # raw file contents are not streamed into the chat bubble
    assert "<h1>" not in "".join(d["text"] for e, d in events if e == "token")
    assert env.chat.messages[1]["artifacts"][0]["files"][0]["name"] == "index.html"


def test_router_failure_falls_back_to_chat(env):
    env.models.by_spec[env.deps.settings.router_model] = fake_llm(structured=RuntimeError("bad json"))
    events = parse_sse(env.post_chat("hello").text)
    assert events[0][1]["agent"] == "chat"
    assert events[-1][0] == "done"


def test_search_cites_sources(env):
    env.http_routes["https://api.tavily.com"] = lambda req: httpx.Response(
        200,
        json={
            "results": [{"title": "Go 1.26 released", "url": "https://go.dev/blog", "content": "..."}],
            "images": ["https://img.example/a.png", "http://insecure/b.png"],
        },
    )
    events = parse_sse(env.post_chat("latest go release", agent="search").text)
    sources = next(d for e, d in events if e == "sources")
    assert sources["sources"] == [{"title": "Go 1.26 released", "url": "https://go.dev/blog"}]
    assert sources["images"] == ["https://img.example/a.png"]
    assert env.auth.reserved == {"r0": 3}


def test_ppt_generation_stores_file(env):
    deck = DeckSpec(
        title="Go at Scale",
        slides=[
            Slide(type="bullets", title="Why Go", bullets=["Fast", "Simple"]),
            Slide(type="stats", title="Numbers", stats=[{"label": "Users", "value": "3M"}]),
            Slide(type="conclusion", title="Wrap up", bullets=["Use Go"]),
        ],
    )
    env.models.default = fake_llm(structured=deck)
    events = parse_sse(env.post_chat("presentation on go", agent="ppt").text)
    att = next(d["attachment"] for e, d in events if e == "attachment")
    assert att["fileId"].startswith(f"{USER}/{CONV}/") and att["name"] == "go-at-scale.pptx"

    path = att["url"].removeprefix("/api")
    download = env.client.get(path, headers={"X-Internal-Token": TOKEN, "X-User-Id": USER})
    assert download.status_code == 200 and download.content[:2] == b"PK"
    stolen = env.client.get(path, headers={"X-Internal-Token": TOKEN, "X-User-Id": "mallory"})
    assert stolen.status_code == 404


def test_pdf_upload_is_indexed_then_answered(env):
    env.index.results = [Chunk(text="Revenue grew 40%", file_id="f", name="r.pdf", page=3, score=0.9)]
    env.models.default = fake_llm("Revenue grew 40% (r.pdf, p. 3).")
    files = {"file": ("report.pdf", b"%PDF-1.4 fake", "application/pdf")}
    events = parse_sse(env.post_chat("how did revenue change?", files=files).text)

    assert events[-1][0] == "done", events
    assert len(env.index.ingested) == 1
    assert env.chat.conversations[CONV]["documents"][0]["name"] == "report.pdf"
    assert next(d for e, d in events if e == "meta")["agent"] == "rag"
    assert next(d for e, d in events if e == "sources")["sources"][0]["title"] == "r.pdf · p. 3"
    assert env.chat.messages[0]["attachments"][0]["kind"] == "upload"


def test_rejects_disguised_upload(env):
    files = {"file": ("evil.pdf", b"<script>alert(1)</script>", "application/pdf")}
    assert env.post_chat("read this", files=files).status_code == 400


def test_rag_without_documents_is_rejected(env):
    events = parse_sse(env.post_chat("what does it say?", agent="rag").text)
    assert events[-1][0] == "error" and env.auth.reserved == {}


def test_cleanup_endpoint(env):
    resp = env.client.delete(f"/internal/users/{USER}/conversations/{CONV}", headers={"X-Internal-Token": TOKEN})
    assert resp.status_code == 204
    assert env.index.deleted == [CONV]
