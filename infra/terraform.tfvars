# Values for this deployment. Nothing here is secret. Keep it in sync with
# deploy/gcp/config.env (which CI uses to build and push images).

project_id     = "multi-agent-ai-platform-a63ca"
project_number = "720170616128"
region         = "asia-south1"
github_repo    = "ASR222/cortex-ai"
gcs_bucket     = "cortex-ai-files-asr222"

firebase_project_id = "multi-agent-ai-platform-a63ca"
razorpay_key_id     = "rzp_test_TiLtBTvnfoR4PL"
qdrant_url          = "https://186d3c17-f246-4d99-a278-59f5249143b2.europe-west3-0.gcp.cloud.qdrant.io:6333"

starting_credits  = 50
daily_request_cap = 1500

# Must match the secrets that actually exist (`gcloud secrets list`).
# Add or remove optional ones to match your project.
secrets = [
  "internal-token",
  "mongodb-uri",
  "redis-url",
  "qdrant-api-key",
  "groq-api-key",
  "google-api-key",
  "tavily-api-key",
  "razorpay-key-secret",
  # "razorpay-webhook-secret",
  # "openrouter-api-key",
]
