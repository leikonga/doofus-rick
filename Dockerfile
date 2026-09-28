FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS builder

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

FROM golang:1.27.1-alpine AS toolchain

FROM alpine:3

RUN apk add --no-cache tini

RUN --mount=type=bind,source=internal/sandbox/tools.txt,target=/tmp/tools.txt \
    apk add --no-cache $(sed -e '/^[[:space:]]*#/d' -e 's/;.*//' /tmp/tools.txt)

RUN addgroup -S rickwork && \
    adduser -D -g '' appuser && \
    adduser -D -g '' -h /rick/work -H rick && \
    addgroup appuser rickwork && \
    addgroup rick rickwork && \
    mkdir -p /rick/work && \
    chown appuser:rickwork /rick/work && \
    chmod 2775 /rick/work && \
    git config --system --add safe.directory '*'

COPY --from=toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:$PATH"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/doofus-rick /doofus-rick
RUN setcap cap_setuid,cap_setgid,cap_kill+ep /doofus-rick

USER appuser
EXPOSE 8080

ENTRYPOINT ["/sbin/tini", "--", "/doofus-rick"]
