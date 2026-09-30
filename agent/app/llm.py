"""Chat model factory.

Models are configured as "provider:model" strings so a deployment can move
an agent between providers (for cost, quality or quota reasons) without code
changes. Every model gets a fallback on a different model, so a provider
outage or an exhausted free-tier quota degrades quality instead of failing.
"""

from functools import lru_cache

from langchain_core.language_models import BaseChatModel
from langchain_core.runnables import Runnable

from app.config import Settings
from app.errors import AgentError


def _build(spec: str, s: Settings, temperature: float, max_tokens: int) -> BaseChatModel:
    provider, _, model = spec.partition(":")
    common = {"temperature": temperature, "max_retries": 2, "timeout": s.llm_timeout_seconds}

    if provider == "groq":
        if not s.groq_api_key:
            raise AgentError(503, "not_configured", "The Groq API key is not configured.")
        from langchain_groq import ChatGroq

        return ChatGroq(model=model, api_key=s.groq_api_key, max_tokens=max_tokens, **common)

    if provider == "google":
        if not s.google_api_key:
            raise AgentError(503, "not_configured", "The Google API key is not configured.")
        from langchain_google_genai import ChatGoogleGenerativeAI

        return ChatGoogleGenerativeAI(
            model=model, google_api_key=s.google_api_key, max_output_tokens=max_tokens, **common
        )

    if provider == "openrouter":
        if not s.openrouter_api_key:
            raise AgentError(503, "not_configured", "The OpenRouter API key is not configured.")
        from langchain_openai import ChatOpenAI

        return ChatOpenAI(
            model=model,
            api_key=s.openrouter_api_key,
            base_url="https://openrouter.ai/api/v1",
            max_tokens=max_tokens,
            **common,
        )

    raise ValueError(f"unknown model provider in {spec!r}")


class Models:
    """Builds and caches models for a Settings instance."""

    def __init__(self, settings: Settings) -> None:
        self._s = settings

    @lru_cache(maxsize=32)  # noqa: B019 - one Models instance per process
    def get(self, spec: str, temperature: float = 0.3, max_tokens: int = 4096) -> Runnable:
        primary = _build(spec, self._s, temperature, max_tokens)
        fallback_spec = self._s.fallback_model
        if fallback_spec and fallback_spec != spec:
            try:
                fallback = _build(fallback_spec, self._s, temperature, max_tokens)
            except AgentError:
                return primary
            return primary.with_fallbacks([fallback])
        return primary

    def raw(self, spec: str, temperature: float = 0.3, max_tokens: int = 4096) -> BaseChatModel:
        """A model without fallbacks, for features the fallback cannot do
        (e.g. structured output or images)."""
        return _build(spec, self._s, temperature, max_tokens)
