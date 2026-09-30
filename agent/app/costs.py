"""Credit prices and per-minute limits for each agent.

Prices reflect relative cost: a chat turn is one cheap LLM call, while a
presentation needs a large structured generation plus file rendering.
"""

AGENTS = ("chat", "search", "coding", "pdf", "ppt", "image", "vision", "rag")

CREDIT_COST: dict[str, int] = {
    "chat": 1,
    "search": 3,
    "coding": 5,
    "pdf": 5,
    "ppt": 5,
    "image": 5,
    "vision": 3,
    "rag": 2,
}

PER_MINUTE_LIMIT: dict[str, int] = {
    "chat": 20,
    "search": 8,
    "coding": 6,
    "pdf": 4,
    "ppt": 4,
    "image": 3,
    "vision": 6,
    "rag": 10,
    "upload": 5,  # PDF indexing (embedding calls), checked separately from the agent
}
