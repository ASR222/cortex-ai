#!/usr/bin/env bash
# Ships new code: builds and pushes all five images, then points each Cloud
# Run service at its new image.
#
# Only the image changes here. Everything else about the services (env vars,
# secrets, scaling, IAM) is owned by Terraform in infra/, which ignores the
# image, so code deploys and infrastructure changes never overwrite each other.
#
# Runs in GitHub Actions on each green push to main, or locally after
# `gcloud auth login`. Requires Docker and gcloud.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

# shellcheck source=/dev/null
source deploy/gcp/config.env
: "${PROJECT_ID:?}" "${PROJECT_NUMBER:?}" "${REGION:?}"

TAG=${TAG:-$(git rev-parse --short HEAD)}
REGISTRY="$REGION-docker.pkg.dev/$PROJECT_ID/cortex"

gcloud config set project "$PROJECT_ID" >/dev/null
gcloud auth configure-docker "$REGION-docker.pkg.dev" --quiet >/dev/null

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

# Internal services first, the public gateway last.
for svc in auth chat billing agent gateway; do
  echo "==> Rolling out cortex-$svc"
  gcloud run services update "cortex-$svc" --region "$REGION" --image "$REGISTRY/$svc:$TAG" --quiet
done

echo
echo "Deployed $TAG. Site: https://cortex-gateway-$PROJECT_NUMBER.$REGION.run.app"
