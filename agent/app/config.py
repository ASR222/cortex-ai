"""Service configuration, read once from environment variables."""

from functools import lru_cache
from typing import Literal

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # Internal wiring
    internal_token: str
    # "google" on Cloud Run: attach identity tokens to service-to-service calls
    # and use the service account for Google APIs. "none" locally.
    service_auth: Literal["none", "google"] = "none"
    redis_url: str
    chat_service_url: str
    auth_service_url: str

    # Model providers. Only GROQ_API_KEY is strictly required; the others
    # enable vision + PDF Q&A (Google), web search (Tavily) and OpenRouter models.
    groq_api_key: str = ""
    google_api_key: str = ""
    openrouter_api_key: str = ""
    tavily_api_key: str = ""

    # Models as "provider:model". Providers: groq, google, openrouter.
    router_model: str = "groq:openai/gpt-oss-20b"
    chat_model: str = "groq:openai/gpt-oss-120b"
    coding_model: str = "groq:openai/gpt-oss-120b"
    documents_model: str = "groq:openai/gpt-oss-120b"
    vision_model: str = "google:gemini-2.5-flash"
    fallback_model: str = "groq:openai/gpt-oss-20b"
    llm_timeout_seconds: float = 90

    # RAG
    embedding_model: str = "gemini-embedding-001"
    embedding_dim: int = 768
    qdrant_url: str = "http://qdrant:6333"
    qdrant_api_key: str = ""
    qdrant_collection: str = "documents"

    # File storage: "local" (a Docker volume), "gcs" (Google Cloud Storage,
    # authenticated as the service account) or "s3" (any S3-compatible store).
    storage_backend: Literal["local", "s3", "gcs"] = "local"
    storage_dir: str = "/data/files"
    gcs_bucket: str = ""
    s3_bucket: str = ""
    s3_endpoint_url: str = ""
    s3_region: str = "auto"
    s3_access_key_id: str = ""
    s3_secret_access_key: str = ""

    # Image generation endpoint; {prompt} is replaced with the URL-encoded prompt.
    image_api_url: str = "https://image.pollinations.ai/prompt/{prompt}?width=1024&height=1024&nologo=true&model=flux"

    # Protects the owner's API quotas on a public demo: total agent runs per day.
    daily_request_cap: int = 1500
    max_upload_mb: int = 20
    max_prompt_chars: int = 8000


@lru_cache
def get_settings() -> Settings:
    return Settings()  # type: ignore[call-arg]
