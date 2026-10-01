variable "project_id" {
  type        = string
  description = "Google Cloud project id."
}

variable "project_number" {
  type        = string
  description = "Google Cloud project number (used to build deterministic Cloud Run URLs)."
}

variable "region" {
  type        = string
  description = "Region for Cloud Run, Artifact Registry and the files bucket."
  default     = "asia-south1"
}

variable "github_repo" {
  type        = string
  description = "GitHub repository (owner/name) allowed to deploy via Workload Identity Federation."
}

variable "gcs_bucket" {
  type        = string
  description = "Globally unique name of the private bucket for uploads and generated files."
}

variable "firebase_project_id" {
  type = string
}

variable "razorpay_key_id" {
  type        = string
  description = "Public Razorpay key id (rzp_test_...)."
}

variable "qdrant_url" {
  type = string
}

variable "starting_credits" {
  type    = number
  default = 50
}

variable "daily_request_cap" {
  type    = number
  default = 1500
}

variable "models" {
  type        = map(string)
  description = "Agent model settings, as provider:model strings."
  default = {
    CHAT_MODEL      = "groq:openai/gpt-oss-120b"
    CODING_MODEL    = "groq:openai/gpt-oss-120b"
    DOCUMENTS_MODEL = "groq:openai/gpt-oss-120b"
    ROUTER_MODEL    = "groq:openai/gpt-oss-20b"
    FALLBACK_MODEL  = "groq:openai/gpt-oss-20b"
  }
}

variable "secrets" {
  type        = set(string)
  description = <<-EOT
    Secret Manager secrets that exist and have a value (uploaded by bootstrap.sh).
    Optional ones (google-api-key, tavily-api-key, openrouter-api-key,
    razorpay-webhook-secret) can be left out; services then run without them.
  EOT
}

variable "max_instances" {
  type        = number
  description = "Upper bound on instances per service (cost guardrail)."
  default     = 2
}

variable "agent_min_instances" {
  type        = number
  description = "Keep this many agent instances warm (removes cold starts; costs money above 0)."
  default     = 0
}
