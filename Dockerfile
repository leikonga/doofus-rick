FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-w -s" -o doofus-rick ./cmd/doofus-rick
RUN adduser -D -g '' appuser

FROM alpine:3

RUN apk add --no-cache \
    bash curl jq \
    git openssh-client \
    python3 uv \
    make coreutils \
    sqlite \
    diffutils patch \
    tzdata \
    file bc \
    bind-tools \
    openssl

RUN adduser -D -g '' appuser && \
    mkdir -p /rick/work && \
    chown appuser:appuser /rick/work

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/doofus-rick /doofus-rick

USER appuser
EXPOSE 8080

ENTRYPOINT ["/doofus-rick"]