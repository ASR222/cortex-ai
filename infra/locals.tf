locals {
  services = ["gateway", "auth", "chat", "billing", "agent"]

  apis = [
    "run.googleapis.com",
    "artifactregistry.googleapis.com",
    "secretmanager.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "sts.googleapis.com",
    "storage.googleapis.com",
  ]

  # Cloud Run URLs are deterministic, so services can reference each other
  # without depending on each other's resources (no cycles).
  url = { for s in local.services : s => "https://cortex-${s}-${var.project_number}.${var.region}.run.app" }

  # Secret name -> env var it is exposed as, and which services may read it.
  secret_env = {
    "internal-token"          = "INTERNAL_TOKEN"
    "mongodb-uri"             = "MONGODB_URI"
    "redis-url"               = "REDIS_URL"
    "qdrant-api-key"          = "QDRANT_API_KEY"
    "groq-api-key"            = "GROQ_API_KEY"
    "google-api-key"          = "GOOGLE_API_KEY"
    "tavily-api-key"          = "TAVILY_API_KEY"
    "openrouter-api-key"      = "OPENROUTER_API_KEY"
    "razorpay-key-secret"     = "RAZORPAY_KEY_SECRET"
    "razorpay-webhook-secret" = "RAZORPAY_WEBHOOK_SECRET"
  }
  secret_readers = {
    "internal-token"          = ["gateway", "auth", "chat", "billing", "agent"]
    "mongodb-uri"             = ["auth", "chat", "billing"]
    "redis-url"               = ["gateway", "agent"]
    "qdrant-api-key"          = ["agent"]
    "groq-api-key"            = ["agent"]
    "google-api-key"          = ["agent"]
    "tavily-api-key"          = ["agent"]
    "openrouter-api-key"      = ["agent"]
    "razorpay-key-secret"     = ["billing"]
    "razorpay-webhook-secret" = ["billing"]
  }

  # One IAM binding per (secret, reader) pair.
  secret_access = merge([
    for name in var.secrets : {
      for svc in local.secret_readers[name] : "${name}/${svc}" => { secret = name, service = svc }
    }
  ]...)

  # Secrets each service mounts as env vars (sorted for a stable plan).
  service_secrets = {
    for svc in local.services : svc => sort([for name in var.secrets : name if contains(local.secret_readers[name], svc)])
  }

  # Who may call whom. Mirrors the call graph exactly; anything else gets 403.
  invokers = {
    "gateway->auth"    = { caller = "gateway", target = "auth" }
    "agent->auth"      = { caller = "agent", target = "auth" }
    "billing->auth"    = { caller = "billing", target = "auth" }
    "gateway->chat"    = { caller = "gateway", target = "chat" }
    "agent->chat"      = { caller = "agent", target = "chat" }
    "gateway->billing" = { caller = "gateway", target = "billing" }
    "gateway->agent"   = { caller = "gateway", target = "agent" }
    "chat->agent"      = { caller = "chat", target = "agent" }
  }

  # Per-service runtime settings and plain (non-secret) env vars.
  run = {
    gateway = {
      memory        = "256Mi"
      min_instances = 0
      env = {
        SERVICE_AUTH        = "google"
        AUTH_SERVICE_URL    = local.url.auth
        CHAT_SERVICE_URL    = local.url.chat
        BILLING_SERVICE_URL = local.url.billing
        AGENT_SERVICE_URL   = local.url.agent
        COOKIE_SECURE       = "true"
        TRUST_PROXY         = "true"
      }
    }
    auth = {
      memory        = "256Mi"
      min_instances = 0
      env = {
        SERVICE_AUTH        = "google"
        FIREBASE_PROJECT_ID = var.firebase_project_id
        STARTING_CREDITS    = tostring(var.starting_credits)
      }
    }
    chat = {
      memory        = "256Mi"
      min_instances = 0
      env = {
        SERVICE_AUTH      = "google"
        AGENT_SERVICE_URL = local.url.agent
      }
    }
    billing = {
      memory        = "256Mi"
      min_instances = 0
      env = {
        SERVICE_AUTH     = "google"
        AUTH_SERVICE_URL = local.url.auth
        RAZORPAY_KEY_ID  = var.razorpay_key_id
      }
    }
    agent = {
      memory        = "1Gi"
      min_instances = var.agent_min_instances
      env = merge({
        SERVICE_AUTH      = "google"
        CHAT_SERVICE_URL  = local.url.chat
        AUTH_SERVICE_URL  = local.url.auth
        QDRANT_URL        = var.qdrant_url
        STORAGE_BACKEND   = "gcs"
        GCS_BUCKET        = var.gcs_bucket
        DAILY_REQUEST_CAP = tostring(var.daily_request_cap)
      }, var.models)
    }
  }
}
