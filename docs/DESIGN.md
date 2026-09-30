# CortexAI design notes

This document explains *why* the system is built the way it is. For each decision it gives the choice, the reasoning, the trade-offs, and what would change at larger scale. Use it to prepare for design questions in interviews.

---

## 1. Service boundaries

| Service | Language | Owns | Talks to |
|---|---|---|---|
| gateway | Go | sessions, rate limits, CSRF, routing | auth, all services (proxy) |
| auth | Go | users, credit balance, reservations | MongoDB |
| chat | Go | conversations, messages, document list | MongoDB, agent (cleanup) |
| billing | Go | payments, Razorpay | MongoDB, auth (grant credits) |
| agent | Python | LLM orchestration, RAG, generated files | chat, auth, Redis, Qdrant, LLM APIs |

**Why microservices at all?** The honest answer is that a monolith would be simpler at this scale. The split is along real seams, though:
- **Different runtimes.** The AI work needs Python's ecosystem; the rest benefits from Go's small memory footprint and simple concurrency.
- **Different failure modes.** An LLM provider timing out must not take down login or payments.
- **Different scaling needs.** The agent is CPU- and IO-heavy, while the other services are light.

**Why Go for gateway/auth/chat/billing?** They are IO-bound HTTP services where Go's standard library does most of the work: `net/http`, `httputil.ReverseProxy` and `crypto/hmac`. Each is a static binary of about 15 MB that starts in milliseconds and uses around 10 MB of RAM.

**Why Python for the agent?** LangGraph, the LangChain provider integrations, `pypdf`, `python-pptx`, `reportlab` and `qdrant-client` are all mature Python libraries. Go has no equivalent of LangGraph.

**Communication** is synchronous HTTP/JSON between services. That's simple to debug with curl and logs. At higher scale I'd consider:
- **gRPC**, for typed contracts and less JSON overhead.
- **A queue** (NATS or Redis Streams) for slow, retryable work such as PDF ingestion.

## 2. Authentication and sessions

**Flow:**
1. The browser signs in with Google through Firebase and gets a short-lived **ID token** (a JWT).
2. The gateway sends it to auth's `/internal/login`.
3. Auth **verifies the JWT itself**. It checks the RS256 signature against Google's published keys (cached using the `Cache-Control` max-age), plus `aud` = project id, `iss`, `exp`, `iat`, `auth_time` and a non-empty `sub`.
4. Auth upserts the user.
5. The gateway creates a session in Redis and sets the cookie.

**Why verify the JWT manually instead of using the Firebase Admin SDK?** Verification needs only public keys and the project id. That means no service-account private key exists anywhere in the deployment; the original prototype committed one to the repo. Tests cover forged keys, wrong audience and issuer, expiry, and the "alg confusion" attack (HS256 signed with public data).

**Why server-side sessions instead of JWT cookies?**
- **Instant revocation.** Logout deletes the session, and a stolen cookie stops working immediately.
- **The cookie is opaque**, a random 256-bit token.
- **Redis stores only `sha256(token)`**, so a Redis leak doesn't leak usable sessions.
- **The cost** is one Redis GET per request, well under a millisecond on the same host.

**Cookie flags:**
- `HttpOnly`: JavaScript can't read the cookie, so an XSS bug can't steal the session.
- `Secure`: sent over HTTPS only.
- `SameSite=Lax`: not sent on cross-site POSTs.
- The session id is rotated on every login, which prevents session fixation.

**CSRF:** every non-GET request must carry `X-Requested-With: XMLHttpRequest`. Browsers won't add custom headers to cross-origin requests without a CORS preflight, and the gateway never approves one. Together with `SameSite=Lax` that gives two independent layers.

**Same origin by design.** The SPA and `/api` are always served from one origin:
- **Cloud Run:** the gateway serves the built SPA itself (`STATIC_DIR`).
- **Docker Compose:** Caddy serves it.
- **Development:** Vite proxies `/api`.

So there's no CORS configuration and no third-party-cookie problem.

## 3. Trust between services

Services identify the user from the `X-User-Id` header. That's only safe if nobody but the gateway can call them, so there are several layers:
1. **Caller identity (Cloud Run).**
   - The internal services are deployed with `--no-allow-unauthenticated`.
   - A caller must present a **Google-signed identity token** (a JWT) for its own service account. The token comes from the metadata server, which is reachable only inside Google Cloud, is cached, and is refreshed 5 minutes before expiry.
   - The caller's service account must also hold `roles/run.invoker` **on that specific service**.
   - Google's front end verifies all of this before a request reaches the container.
   - The grants mirror the call graph exactly. For example, billing may call auth, but not chat or agent, so a compromised billing service can't touch conversations.
   - **In Docker Compose**, the equivalent layer is the private network: only Caddy publishes ports.
2. **Shared secret (defence in depth).** Every service also rejects requests without the correct `X-Internal-Token`. The comparison is constant-time (`subtle.ConstantTimeCompare`, `hmac.compare_digest`), so timing doesn't leak the token. This layer still protects the services if an IAM binding is ever misconfigured, and it's the main guard in Compose.
3. **Header hygiene.**
   - The gateway **deletes** any client-supplied `X-User-Id`, `X-Internal-Token`, `X-Request-Id`, `Cookie` and `Authorization` headers before setting its own.
   - It refuses any path containing an `internal` segment, so `/api/chat/internal/...` can never reach the internal APIs.

**Why identity tokens instead of mTLS?** On Cloud Run, Google terminates TLS, so there's no certificate for us to manage. IAM gives per-caller, per-service authorization with audit logs, and nothing to rotate: tokens last an hour and no key files exist.

## 4. Authorization: no IDOR

In the original prototype, anyone could read any conversation by id. Now:
- **Every MongoDB query filters by `userId` as well as `_id`.** Ownership is enforced in the storage layer, so there's no code path where a handler can "forget" the check.
- **"Not yours" and "doesn't exist" both return 404**, so ids can't be probed.
- **File keys are `userId/conversationId/random-name`.** A download is allowed only if the key's first segment is the caller's id, and path traversal is rejected.
- **Internal writes are validated too.** The chat service rejects attachments whose key belongs to another user.

## 5. Credits: correct under concurrency

**The problem:** the prototype did read-modify-write (`user.credits -= cost; save()`). Two parallel requests could both pass the balance check and overspend. It also charged *before* the work and never refunded failures.

**The design** has three steps:
```
reserve  → findOneAndUpdate({_id, credits: {$gte: cost}}, {$inc: {credits: -cost}})
           + insert reservation {status: "reserved"}
commit   → reservation: reserved → committed      (on success)
refund   → reservation: reserved → refunded, then $inc credits back (on failure)
```
- **Check-and-subtract is a single atomic document update.** The `$gte` guard in the filter means the balance can never go negative, and no transaction is needed. An integration test fires 50 concurrent reservations at a balance of 10: exactly 10 succeed.
- **Settling is a conditional state transition** (`status: "reserved"` in the filter). Only the call that flips it acts, so refunds are idempotent and a commit and a refund can't both win.
- **Ordering fails safe.** A crash between the two writes can lose a user a few credits, but can never mint credits. A background "reaper" that refunds stale `reserved` records would close that gap; it's listed under future work.

**Where it's enforced:** a LangGraph `guard` node runs after routing, because the price depends on the agent, and before any expensive call. The API layer commits after a successful turn and refunds on any error. If the client disconnects mid-answer it's charged only if some output was already streamed. That trade-off prevents free answers from someone who cancels at the last token.

## 6. Payments: idempotent fulfilment

**Order creation:**
- The price comes from the server's plan table and is never taken from the client.
- A `Payment` record is created with `userId` and `status: created`.

**Two confirmation paths:**
1. **Browser verify.** Checkout returns `HMAC_SHA256(order_id|payment_id, key_secret)`. The server checks the HMAC **and** that the order belongs to the caller. The prototype didn't check ownership, so anyone holding a signed payment could replay it to credit their own account.
2. **Webhook.** Razorpay posts `payment.captured` with `X-Razorpay-Signature` = HMAC of the **raw body**. The server verifies it, checks that the amount matches, then fulfils. This path is what credits a user who closes the tab too early.

**Exactly once, via `fulfill`:**
- It marks the payment paid, a no-op if it already is.
- It then calls the auth service's `Grant(key = orderId)`.
- The grant pushes the key onto `user.appliedGrants` **in the same atomic update** that adds the credits, filtered by `appliedGrants: {$ne: key}`. Replays, retries and both paths firing all result in exactly one grant.

Tests cover a replayed verify, verify plus two webhooks, a forged signature, someone else's order and an amount mismatch.

## 7. The agent graph (LangGraph)

```
START → router → guard → {chat | search | coding | pdf | ppt | image | vision | rag} → END
```

**router:**
- An explicit user choice wins.
- An uploaded image goes to `vision`; an uploaded PDF goes to `rag`.
- Otherwise a small, fast model (`llama-3.1-8b-instant`) classifies the message using **structured output**, a Pydantic model with a `Literal` enum. That means the model can only return a valid agent name.
- If classification fails for any reason, the request goes to `chat`.
- `rag` is offered only when the conversation has documents.

The prototype's router parsed free text, and its prompt listed only four of six agents, so `ppt` and `image` were unreachable.

**guard:** checks the per-user, per-agent rate limits and the global daily budget, then reserves credits.

**Agents** push UI events through LangGraph's `custom` stream mode (`get_stream_writer()`). The API turns each event into a Server-Sent Event. The final state (`values` mode) provides the response, artifacts, attachments and sources to persist.

**Why LangGraph for a mostly linear graph?**
- Explicit, testable nodes with typed state.
- Built-in streaming modes.
- Easy extension: for example, adding a "search → then code" chain, a human-approval step, or a checkpointer for resumable runs.

## 8. Streaming (SSE)

`POST /api/agent/chat` returns `text/event-stream`. The events are `meta`, `status`, `token`, `sources`, `artifact`, `attachment`, `done` and `error`.

- **SSE rather than WebSockets:** the stream is one-way (server to client) and is plain HTTP, so it passes through Caddy and the Go proxy unchanged. The only settings needed are `flush_interval -1` in Caddy and `FlushInterval: -1` in the Go reverse proxy. It works with cookies and needs no extra protocol.
- **POST + fetch instead of EventSource:** `EventSource` supports only GET, and the request carries a file upload, so the frontend reads the body with `ReadableStream` and parses the frames itself.
- **Errors during a stream** arrive as an `error` event, because the HTTP 200 status has already been sent. Validation and ownership errors are returned *before* the stream starts, as normal HTTP errors.
- **The coding agent** detects "project mode" from the first 5 characters (`FILE:`). In that mode it shows "Writing index.html…" progress instead of streaming raw file text into the chat bubble.

## 9. Memory and history

MongoDB, through the chat service, is the source of truth. Redis caches the last 20 turns per conversation (24-hour TTL). The prototype had a real bug here: it pushed the new prompt into Redis before loading history. On a cold cache, that created a list containing only the new prompt, so all earlier context was lost and the prompt was sent twice.

**The fix:**
- History is **read before** the turn.
- New turns are appended with **`RPUSHX`**, which writes only if the key exists. A cold cache is never seeded with a partial list; the next read loads the full history from MongoDB.

## 10. RAG (document Q&A)

- **Ingestion runs once, at upload.** `pypdf` extracts text per page, then it's split into 1000-character chunks with 150 overlap, **keeping page numbers**. Chunks are embedded with Gemini `gemini-embedding-001` at 768 dimensions (`RETRIEVAL_DOCUMENT` task type) and upserted into Qdrant.
- **There's one collection for all users**, with payload fields `user_id`, `conversation_id` and `file_id`, each with a keyword index. Every search filters on user **and** conversation. The prototype created a new collection per question and then deleted it, which re-embedded the whole PDF each time and doesn't scale, since collections are heavyweight in Qdrant.
- **Retrieval** returns the top 6 chunks by cosine similarity with a minimum score of 0.35. The prompt demands answers only from the excerpts, with page citations. Sources are returned as `file.pdf · p. 4` links that open the PDF at that page.
- **Deleting a conversation** removes its vectors and files. The chat service triggers the agent's cleanup endpoint asynchronously, as a best-effort call.
- **Limits:** scanned PDFs, which have no text layer, are rejected with a clear message. OCR would be the next step.

## 11. Files and storage

- **Storage is an interface** with three backends: local disk (a Docker volume, the default for local use), **Google Cloud Storage** (production on Cloud Run) and any S3-compatible bucket (AWS S3, Cloudflare R2, MinIO).
- **Links never expire.** The prototype embedded 24-hour presigned S3 URLs in chat text, so every old link died. Now messages store only the storage key and link to `/api/files/<key>`, and access is checked on every download: the session at the gateway, the owner in the agent. With S3 the agent redirects to a 5-minute presigned URL.
- **Uploads** are limited to 20 MB and identified by **magic bytes** (`%PDF-`, PNG, JPEG, WebP and GIF signatures). The filename and `Content-Type` are ignored. Filenames are sanitised, and the storage path is checked so it can't escape the root directory.
- **Downloads** are served with a restrictive `Content-Security-Policy` including `sandbox`. Only PDFs and images are shown inline; everything else downloads.

## 12. Generated documents

- **PDF and PPTX use structured output.** The model fills a Pydantic schema (`DocumentSpec`, `DeckSpec`) instead of free text parsed with regexes. Invalid output is retried once, then reported as an error, and the credits are refunded.
- **Rendering is deterministic code.** ReportLab platypus lays out the PDF; python-pptx builds the deck, reproducing the original visual design. Model text is XML-escaped before it reaches ReportLab's markup parser.
- **The coding preview** runs in an `<iframe sandbox="allow-scripts">` with no `allow-same-origin`, so generated JavaScript can't touch the app's origin, cookies or storage.

## 13. Reliability and operations

- **Structured JSON logs** in every service, all carrying one `request_id`. The gateway creates it, ignoring any id the client sends, and every internal call forwards it, so one request can be traced across all five services.
- **Graceful shutdown.** Go services drain for 20 seconds, and uvicorn uses a graceful timeout.
- **Health checks.** Each distroless Go binary has a `healthcheck` subcommand, and Compose waits for dependencies to be healthy.
- **LLM fallbacks.** Every model is wrapped with `.with_fallbacks()` on a different model. An exhausted free-tier quota or a provider outage degrades quality instead of failing.
- **Timeouts** on every outbound call: LLM 90 s, image generation 120 s, internal calls 15 s.
- **Rate limiter behaviour.** If Redis is unavailable, the gateway's limiter **fails open** so the API stays up; this is logged. The agent's limiter fails closed, because it protects paid quotas.
- **A daily budget cap** (`DAILY_REQUEST_CAP`) protects the owner's API quotas on a public demo.

## 14. Deployment on Google Cloud Run

| Decision | Why |
|---|---|
| **Cloud Run** instead of a VM or GKE | No servers to patch; HTTPS and autoscaling are built in; it scales to zero, so an idle demo costs nothing. GKE's cluster fee and operational load aren't justified for five small services. |
| **Scale to zero, max 2 instances** | Stays inside the free tier and caps worst-case cost. The price is **cold starts**: Go services start in about a second, the Python agent in a few. `--cpu-boost` shortens that, and the UI says "waking up" instead of looking stuck. `--min-instances 1` on the gateway would remove it for a few dollars a month. |
| **One service account per service** | Least privilege. Each can read only its own secrets, and only the agent can write to the bucket. |
| **Secret Manager**, injected as env vars | Secrets never touch the repo, the image or CI logs. The deployer account can see secret *names*, but not values. |
| **Workload Identity Federation** for GitHub Actions | GitHub mints a short-lived OIDC token; Google exchanges it for deployer credentials **only for this repo's `main` branch**. There's no JSON key to leak or rotate. |
| **Deterministic service URLs** (`https://<svc>-<project#>.<region>.run.app`) | Services can be told each other's URLs before they exist, which breaks the gateway → agent → chat → agent cycle at deploy time. |
| **The gateway serves the SPA** | One public service and one origin, so cookies are first-party and there's no CORS or extra hop. |
| **Managed free tiers for data** (Atlas M0, Upstash Redis, Qdrant Cloud) | Cloud Run has no disk. Google's own managed Redis (Memorystore) and databases have no free tier. |
| **Cloud Storage via the JSON API** | The agent authenticates with its own identity. Downloads stream through the agent (after the owner check) instead of using signed URLs, which would need a private key or a `signBlob` grant. |
| **Synchronous cleanup on delete** | Cloud Run throttles the CPU once a response is sent, so background goroutines may never finish. |
| **Artifact Registry cleanup policy** | Keeps the 3 newest images per service, which holds storage near the free 0.5 GB. |

**Known trade-offs:**
- **Atlas access.** Atlas must allow `0.0.0.0/0`, because Cloud Run's outbound IPs aren't fixed without Cloud NAT, which costs money. The strong generated password and TLS are the protection.
- **Budget alerts are manual.** One is set up in the console.
- **Infrastructure is created by shell scripts.** Terraform is the planned next step.

## 15. Known limitations and next steps

- **Terraform:** replace `setup.sh` with Terraform so every resource is declarative and reviewable.
- **Stale credit reservations:** add a periodic reaper that refunds `reserved` records older than about 10 minutes (crash recovery). Cloud Scheduler plus a small endpoint would fit well.
- **Fixed-window rate limits** allow bursts of up to 2× at window edges. A sliding window or token bucket (a Redis Lua script) would fix this.
- **Content Security Policy:** add a strict CSP for the SPA. It needs allow-listing for Firebase auth, Razorpay and the Monaco CDN, and iframe previews inherit it.
- **Observability:** OpenTelemetry traces across Go and Python, exported to Cloud Trace, plus metrics (latency, tokens, credits).
- **PDF ingestion as a background job** (a queue) for large files, with progress events.
- **Evaluation:** a labelled prompt set to measure router accuracy, and RAG answer faithfulness.
- **Backups:** a scheduled `mongodump` to object storage.
- **Horizontal scaling:** on Cloud Run every service is already stateless (Redis, MongoDB, Qdrant and Cloud Storage hold all state), so scaling is just raising `--max-instances`.

## 16. Likely interview questions and short answers

- **"Walk me through a request."** Use the sequence diagram in the README: cookie → gateway (session, rate limit, CSRF) → agent (ownership check, router, guard reserves credits, agent streams) → commit or refund → persist → `done`.
- **"How do you prevent double spending?"** An atomic conditional update (`$gte` in the filter), a reservation state machine, and idempotent settle. See §5.
- **"What if Razorpay calls the webhook twice?"** Grants are keyed by order id and applied with `$ne` in the same atomic update. See §6.
- **"Why not JWTs?"** Instant revocation, an opaque cookie, and hashed storage. See §2.
- **"How does streaming work through the proxies?"** SSE with buffering disabled in Caddy and in Go's ReverseProxy. See §8.
- **"How do you stop users reading each other's data?"** Ownership filters in every query, namespaced file keys, and 404 for both "missing" and "not yours". See §4.
- **"Why is the router reliable?"** Structured output with an enum, plus a safe default and explicit overrides. See §7.
- **"What would break first at 100× traffic?"** LLM provider quotas, then the free-tier databases (Atlas M0 limits, Upstash command quotas). The fixes are paid tiers or self-hosted models, dedicated database tiers, and higher `--max-instances`; the services themselves are stateless.
- **"How do your services authenticate each other?"** Google identity tokens from the metadata server, with `roles/run.invoker` granted per service to mirror the call graph, plus a shared token for defence in depth. See §3.
- **"How does CI/CD reach GCP without a key?"** Workload Identity Federation: GitHub's OIDC token is exchanged for short-lived deployer credentials, restricted to this repo's `main` branch. See §14.
- **"Why Cloud Run and not Kubernetes?"** Five small stateless services, spiky low traffic and a free-tier budget. Scale-to-zero and no cluster to operate win; I'd revisit GKE for long-running workers, GPUs or a service mesh. See §14.
