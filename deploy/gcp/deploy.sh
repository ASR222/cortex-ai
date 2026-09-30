#!/usr/bin/env bash
# Builds, pushes and deploys every service to Cloud Run.
#
# Runs in GitHub Actions on each push to main (see .github/workflows/ci.yml),
# or locally after `gcloud auth login`. Requires Docker and gcloud.
#
# Service URLs are deterministic (https://<service>-<project number>.<region>.run.app),
# so services can be told each other's URLs before they exist, which breaks
# the gateway -> agent -> chat -> agent dependency cycle.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

# shellcheck source=/dev/null
source deploy/gcp/config.env
: "${PROJECT_ID:?}" "${PROJECT_NUMBER:?run setup.sh and set PROJECT_NUMBER}" "${REGION:?}" "${GCS_BUCKET:?}"

TAG=${TAG:-$(git rev-parse --short HEAD)}
REGISTRY="$REGION-docker.pkg.dev/$PROJECT_ID/cortex"
url() { echo "https://cortex-$1-$PROJECT_NUMBER.$REGION.run.app"; }
sa() { echo "cortex-$1@$PROJECT_ID.iam.gserviceaccount.com"; }

gcloud config set project "$PROJECT_ID" >/dev/null
gcloud auth configure-docker "$REGION-docker.pkg.dev" --quiet >/dev/null

# ---------------------------------------------------------------- build + push
build() {
  local svc=$1; shift
  echo "==> Building $svc:$TAG"
  docker build --platform linux/amd64 -t "$REGISTRY/$svc:$TAG" "$@"
  docker push --quiet "$REGISTRY/$svc:$TAG"
}
for svc in auth chat billing; do
  build "$svc" --build-arg "SERVICE=$svc" backend
done
build agent agent
build gateway -f deploy/gcp/gateway.Dockerfile \
  --build-arg "VITE_FIREBASE_API_KEY=$VITE_FIREBASE_API_KEY" \
  --build-arg "VITE_FIREBASE_AUTH_DOMAIN=$VITE_FIREBASE_AUTH_DOMAIN" \
  --build-arg "VITE_FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID" \
  --build-arg "VITE_FIREBASE_APP_ID=$VITE_FIREBASE_APP_ID" \
  .

# ---------------------------------------------------------------- deploy
# ENV=secret-name pairs -> --set-secrets value, skipping optional secrets that
# were never created (e.g. no OpenRouter key).
secrets() {
  local out=() pair
  for pair in "$@"; do
    if gcloud secrets describe "${pair#*=}" >/dev/null 2>&1; then
      out+=("${pair%%=*}=${pair#*=}:latest")
    fi
  done
  (IFS=,; echo "${out[*]}")
}

deploy() {
  local svc=$1; shift
  echo "==> Deploying cortex-$svc"
  # Scale to zero when idle (free), cap at 2 instances (cost guardrail), and
  # give startup a CPU boost to shorten cold starts.
  gcloud run deploy "cortex-$svc" --quiet \
    --image "$REGISTRY/$svc:$TAG" \
    --region "$REGION" \
    --service-account "$(sa "$svc")" \
    --cpu 1 --cpu-boost \
    --min-instances 0 --max-instances 2 \
    "$@"
}

deploy auth --no-allow-unauthenticated --memory 256Mi \
  --set-env-vars "SERVICE_AUTH=google,FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID,STARTING_CREDITS=${STARTING_CREDITS:-50}" \
  --set-secrets "$(secrets INTERNAL_TOKEN=internal-token MONGODB_URI=mongodb-uri)"

deploy chat --no-allow-unauthenticated --memory 256Mi \
  --set-env-vars "SERVICE_AUTH=google,AGENT_SERVICE_URL=$(url agent)" \
  --set-secrets "$(secrets INTERNAL_TOKEN=internal-token MONGODB_URI=mongodb-uri)"

deploy billing --no-allow-unauthenticated --memory 256Mi \
  --set-env-vars "SERVICE_AUTH=google,AUTH_SERVICE_URL=$(url auth),RAZORPAY_KEY_ID=$RAZORPAY_KEY_ID" \
  --set-secrets "$(secrets INTERNAL_TOKEN=internal-token MONGODB_URI=mongodb-uri \
    RAZORPAY_KEY_SECRET=razorpay-key-secret RAZORPAY_WEBHOOK_SECRET=razorpay-webhook-secret)"

deploy agent --no-allow-unauthenticated --memory 1Gi --timeout 300 \
  --set-env-vars "SERVICE_AUTH=google,CHAT_SERVICE_URL=$(url chat),AUTH_SERVICE_URL=$(url auth),QDRANT_URL=$QDRANT_URL,STORAGE_BACKEND=gcs,GCS_BUCKET=$GCS_BUCKET,CODING_MODEL=${CODING_MODEL:-groq:llama-3.3-70b-versatile},DAILY_REQUEST_CAP=${DAILY_REQUEST_CAP:-1500}" \
  --set-secrets "$(secrets INTERNAL_TOKEN=internal-token REDIS_URL=redis-url QDRANT_API_KEY=qdrant-api-key \
    GROQ_API_KEY=groq-api-key GOOGLE_API_KEY=google-api-key TAVILY_API_KEY=tavily-api-key \
    OPENROUTER_API_KEY=openrouter-api-key)"

deploy gateway --allow-unauthenticated --memory 256Mi --timeout 300 \
  --set-env-vars "SERVICE_AUTH=google,AUTH_SERVICE_URL=$(url auth),CHAT_SERVICE_URL=$(url chat),BILLING_SERVICE_URL=$(url billing),AGENT_SERVICE_URL=$(url agent),COOKIE_SECURE=true,TRUST_PROXY=true" \
  --set-secrets "$(secrets INTERNAL_TOKEN=internal-token REDIS_URL=redis-url)"

# ---------------------------------------------------------------- who may call whom
# Mirrors the call graph exactly; any other caller gets 403 from Google's front end.
invoker() {
  gcloud run services add-iam-policy-binding "cortex-$1" --region "$REGION" --quiet \
    --member "serviceAccount:$(sa "$2")" --role roles/run.invoker >/dev/null
}
echo "==> Service-to-service permissions"
invoker auth gateway;    invoker auth agent;  invoker auth billing
invoker chat gateway;    invoker chat agent
invoker billing gateway
invoker agent gateway;   invoker agent chat

echo
echo "Deployed $TAG. Site: $(url gateway)"
