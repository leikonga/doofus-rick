FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o doofus-rick ./main.go
RUN adduser -D -g '' appuser

FROM scratch

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /app/doofus-rick /doofus-rick
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER appuser
EXPOSE 8080

ENTRYPOINT ["/doofus-rick"]