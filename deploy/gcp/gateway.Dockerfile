# Cloud Run image for the gateway: the Go binary plus the built React app.
# The gateway serves the SPA itself (STATIC_DIR), so the site and the API share
# one HTTPS origin on Cloud Run without a separate web server.
#
# Build from the repository root:
#   docker build -f deploy/gcp/gateway.Dockerfile .

FROM node:24-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
ARG VITE_FIREBASE_API_KEY
ARG VITE_FIREBASE_AUTH_DOMAIN
ARG VITE_FIREBASE_PROJECT_ID
ARG VITE_FIREBASE_APP_ID
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=web /web/dist /srv
ENV STATIC_DIR=/srv
USER nonroot:nonroot
ENTRYPOINT ["/app"]
