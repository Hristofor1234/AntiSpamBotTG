# === Этап 1: сборка ===
FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY . .
# go.sum не хранится в репозитории — go mod tidy скачивает зависимости
# и детерминированно достраивает его при каждой сборке образа.
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bot ./cmd/bot

# === Этап 2: runtime ===
FROM alpine:3.19

RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 10001 botuser

COPY --from=builder /bot /bot

USER botuser

CMD ["/bot"]
