# One Dockerfile for all four Go services; pick one with --build-arg SERVICE=gateway|auth|chat|billing.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# Distroless: no shell, no package manager, runs as a non-root user.
# The final image is ~15 MB and has almost nothing an attacker could use.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
