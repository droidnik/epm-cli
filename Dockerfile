# syntax=docker/dockerfile:1
# epm-cli container image for CI/CD pipelines and Kubernetes jobs.
#
#   docker build -t epm-cli --build-arg VERSION=0.1.0 .
#   docker run --rm -e EPM_URL -e EPM_CLIENT_ID -e EPM_CLIENT_SECRET epm-cli computers list --all -o json
#
# Cross-compiles natively on the build host, so multi-arch builds (amd64/arm64) are fast.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -X github.com/droidnik/epm-cli/internal/cli.Version=${VERSION}" \
      -o /out/epm-cli ./cmd/epm-cli

FROM alpine:3.22
ARG VERSION=dev
LABEL org.opencontainers.image.title="epm-cli" \
      org.opencontainers.image.description="CLI for the BeyondTrust EPM for Windows and Mac Management API" \
      org.opencontainers.image.source="https://github.com/droidnik/epm-cli" \
      org.opencontainers.image.version="${VERSION}"
# ca-certificates for TLS, jq for scripting in pipelines; non-root user.
RUN apk add --no-cache ca-certificates jq \
 && adduser -D -u 10001 -h /home/epm epm
COPY --from=build /out/epm-cli /usr/local/bin/epm-cli
USER 10001
# Token cache in /tmp works with read-only root filesystems (Kubernetes securityContext).
ENV EPM_CLI_CACHE_DIR=/tmp/epm-cli \
    EPM_CLI_NO_HINTS=1
ENTRYPOINT ["epm-cli"]
CMD ["--help"]
