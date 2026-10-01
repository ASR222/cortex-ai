terraform {
  # 1.7+ is needed for `import` blocks with for_each (see imports.tf).
  required_version = ">= 1.7"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.0, < 8.0"
    }
  }

  # State lives in a versioned GCS bucket created by deploy/gcp/bootstrap.sh.
  # The bucket name is passed at init time:
  #   terraform init -backend-config="bucket=<project-id>-tfstate"
  backend "gcs" {
    prefix = "cortex-ai"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}
