FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PATH="/app/.venv/bin:$PATH"

COPY --from=ghcr.io/astral-sh/uv:0.11.7 /uv /usr/local/bin/uv
WORKDIR /app

# Dependencies first so code changes don't reinstall them (layer caching).
COPY pyproject.toml uv.lock .python-version ./
RUN uv sync --frozen --no-dev --no-install-project

COPY app ./app

RUN useradd --system --uid 10001 agent && mkdir -p /data/files && chown agent /data/files
USER agent

EXPOSE 8084
HEALTHCHECK --interval=15s --timeout=3s --retries=5 \
  CMD python -c "import os, urllib.request; urllib.request.urlopen('http://127.0.0.1:' + os.environ.get('PORT', '8084') + '/healthz', timeout=2)"

# Cloud Run injects PORT; locally it defaults to 8084. `exec` replaces the
# shell so uvicorn receives SIGTERM directly and shuts down gracefully
# (Cloud Run allows 10 seconds).
CMD ["sh", "-c", "exec uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8084} --timeout-graceful-shutdown 8"]
