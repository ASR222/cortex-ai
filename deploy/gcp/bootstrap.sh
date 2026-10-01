#!/usr/bin/env bash
# The only setup Terraform can't do for itself:
#   1. create the versioned bucket that holds Terraform's own state, and
#   2. upload secret values to Secret Manager (values never go through
#      Terraform, so they never appear in its state or in git).
# Everything else lives in infra/ and is applied with Terraform.
#
# Usage (Cloud Shell):
#   cp deploy/gcp/secrets.env.example deploy/gcp/secrets.env   # fill it in
#   bash deploy/gcp/bootstrap.sh
#   rm deploy/gcp/secrets.env
# Re-running is safe; a secret only gets a new version when its value changed.
set -euo pipefail
cd "$(dirname "$0")"

# shellcheck source=/dev/null
source ./config.env
: "${PROJECT_ID:?set PROJECT_ID in config.env}" "${REGION:?set REGION in config.env}"
gcloud config set project "$PROJECT_ID" >/dev/null

STATE_BUCKET="$PROJECT_ID-tfstate"
echo "==> Terraform state bucket gs://$STATE_BUCKET"
gcloud services enable storage.googleapis.com secretmanager.googleapis.com
if ! gcloud storage buckets describe "gs://$STATE_BUCKET" >/dev/null 2>&1; then
  gcloud storage buckets create "gs://$STATE_BUCKET" --location "$REGION" \
    --uniform-bucket-level-access --public-access-prevention
fi
# Versioning lets you recover an earlier state file if one is ever corrupted.
gcloud storage buckets update "gs://$STATE_BUCKET" --versioning >/dev/null

echo "==> Secret values"
if [[ ! -f secrets.env ]]; then
  echo "   secrets.env not found; skipping (copy secrets.env.example to change secrets)"
else
  # Parsed line by line (not sourced) because values such as MongoDB URIs
  # contain characters the shell would interpret.
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    key=${line%%=*}
    value=${line#*=}
    [[ -z "$value" ]] && continue
    name=$(echo "$key" | tr '[:upper:]_' '[:lower:]-')
    gcloud secrets describe "$name" >/dev/null 2>&1 ||
      gcloud secrets create "$name" --replication-policy=automatic >/dev/null
    current=$(gcloud secrets versions access latest --secret "$name" 2>/dev/null || true)
    if [[ "$current" != "$value" ]]; then
      printf '%s' "$value" | gcloud secrets versions add "$name" --data-file=- >/dev/null
      echo "   $name: new version"
    else
      echo "   $name: unchanged"
    fi
  done < secrets.env
fi

echo
echo "Next:"
echo "  cd infra"
echo "  terraform init -backend-config=\"bucket=$STATE_BUCKET\""
echo "  terraform plan     # read it before applying"
echo "  terraform apply"
