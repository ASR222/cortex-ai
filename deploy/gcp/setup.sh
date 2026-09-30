#!/usr/bin/env bash
# One-time Google Cloud setup for CortexAI. Safe to re-run.
#
# Creates: APIs, an Artifact Registry repo (with a cleanup policy), one service
# account per service plus a deployer account, Secret Manager secrets with
# least-privilege access, a private Cloud Storage bucket, and Workload
# Identity Federation so GitHub Actions can deploy without any key file.
#
# Usage (from Cloud Shell or any machine with gcloud, logged in as an owner):
#   cp deploy/gcp/secrets.env.example deploy/gcp/secrets.env   # fill it in
#   bash deploy/gcp/setup.sh
set -euo pipefail
cd "$(dirname "$0")"

# shellcheck source=/dev/null
source ./config.env
: "${PROJECT_ID:?set PROJECT_ID in config.env}" "${REGION:?}" "${GITHUB_REPO:?set GITHUB_REPO in config.env}"
: "${GCS_BUCKET:?set GCS_BUCKET in config.env}"

gcloud config set project "$PROJECT_ID" >/dev/null
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
SERVICES=(gateway auth chat billing agent)
sa() { echo "cortex-$1@$PROJECT_ID.iam.gserviceaccount.com"; }
quiet() { "$@" >/dev/null; }

echo "==> Enabling APIs (takes a minute the first time)"
gcloud services enable run.googleapis.com artifactregistry.googleapis.com secretmanager.googleapis.com \
  iam.googleapis.com iamcredentials.googleapis.com sts.googleapis.com storage.googleapis.com

echo "==> Artifact Registry repository 'cortex'"
gcloud artifacts repositories describe cortex --location "$REGION" >/dev/null 2>&1 ||
  gcloud artifacts repositories create cortex --repository-format=docker --location "$REGION" \
    --description "CortexAI container images"
# Keeps storage under the free 0.5 GB: only the 3 newest images per service survive.
quiet gcloud artifacts repositories set-cleanup-policies cortex --location "$REGION" \
  --policy=cleanup-policy.json --no-dry-run

echo "==> Service accounts (one per service = least privilege)"
for s in "${SERVICES[@]}" deployer; do
  gcloud iam service-accounts describe "$(sa "$s")" >/dev/null 2>&1 ||
    gcloud iam service-accounts create "cortex-$s" --display-name "CortexAI $s"
done

echo "==> Secrets"
# Which service accounts may read which secret. Nothing else can.
declare -A READERS=(
  [internal-token]="gateway auth chat billing agent"
  [mongodb-uri]="auth chat billing"
  [redis-url]="gateway agent"
  [qdrant-api-key]="agent"
  [groq-api-key]="agent"
  [google-api-key]="agent"
  [tavily-api-key]="agent"
  [openrouter-api-key]="agent"
  [razorpay-key-secret]="billing"
  [razorpay-webhook-secret]="billing"
)
if [[ -f secrets.env ]]; then
  # Parsed line by line (not sourced) because values such as MongoDB URIs
  # contain characters the shell would interpret.
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    key=${line%%=*}
    value=${line#*=}
    [[ -z "$value" ]] && continue
    name=$(echo "$key" | tr '[:upper:]_' '[:lower:]-')
    [[ -z "${READERS[$name]:-}" ]] && { echo "   skipping unknown key $key"; continue; }

    gcloud secrets describe "$name" >/dev/null 2>&1 || quiet gcloud secrets create "$name" --replication-policy=automatic
    current=$(gcloud secrets versions access latest --secret "$name" 2>/dev/null || true)
    if [[ "$current" != "$value" ]]; then
      printf '%s' "$value" | quiet gcloud secrets versions add "$name" --data-file=-
      echo "   $name: new version"
    fi
    for s in ${READERS[$name]}; do
      quiet gcloud secrets add-iam-policy-binding "$name" --member "serviceAccount:$(sa "$s")" \
        --role roles/secretmanager.secretAccessor
    done
  done < secrets.env
else
  echo "   secrets.env not found; skipping (copy secrets.env.example first)"
fi

echo "==> Cloud Storage bucket gs://$GCS_BUCKET (private)"
gcloud storage buckets describe "gs://$GCS_BUCKET" >/dev/null 2>&1 ||
  gcloud storage buckets create "gs://$GCS_BUCKET" --location "$REGION" \
    --uniform-bucket-level-access --public-access-prevention
quiet gcloud storage buckets add-iam-policy-binding "gs://$GCS_BUCKET" \
  --member "serviceAccount:$(sa agent)" --role roles/storage.objectAdmin

echo "==> Deployer permissions"
quiet gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$(sa deployer)" \
  --role roles/run.admin --condition=None
quiet gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$(sa deployer)" \
  --role roles/secretmanager.viewer --condition=None  # metadata only, cannot read values
quiet gcloud artifacts repositories add-iam-policy-binding cortex --location "$REGION" \
  --member "serviceAccount:$(sa deployer)" --role roles/artifactregistry.writer
for s in "${SERVICES[@]}"; do
  # Lets the deployer launch each service as its runtime account.
  quiet gcloud iam service-accounts add-iam-policy-binding "$(sa "$s")" \
    --member "serviceAccount:$(sa deployer)" --role roles/iam.serviceAccountUser
done

echo "==> Workload Identity Federation for GitHub Actions ($GITHUB_REPO)"
gcloud iam workload-identity-pools describe github --location global >/dev/null 2>&1 ||
  gcloud iam workload-identity-pools create github --location global --display-name "GitHub Actions"
gcloud iam workload-identity-pools providers describe github-oidc --location global \
  --workload-identity-pool github >/dev/null 2>&1 ||
  gcloud iam workload-identity-pools providers create-oidc github-oidc --location global \
    --workload-identity-pool github --display-name "GitHub OIDC" \
    --issuer-uri "https://token.actions.githubusercontent.com" \
    --attribute-mapping "google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.ref=assertion.ref" \
    --attribute-condition "assertion.repository == '$GITHUB_REPO' && assertion.ref == 'refs/heads/main'"
quiet gcloud iam service-accounts add-iam-policy-binding "$(sa deployer)" \
  --role roles/iam.workloadIdentityUser \
  --member "principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github/attribute.repository/$GITHUB_REPO"

echo
echo "Done. Put this in deploy/gcp/config.env if it is not there yet:"
echo "  PROJECT_NUMBER=$PROJECT_NUMBER"
echo "Your site will be: https://cortex-gateway-$PROJECT_NUMBER.$REGION.run.app"
echo "Remember to create a budget alert: Billing > Budgets & alerts."
