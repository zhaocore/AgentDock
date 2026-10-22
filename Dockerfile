FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o bin/dutybot ./cmd/server

# 运行镜像提供无磁盘树采集和 Markdown 转换。
FROM oven/bun:1.3.11-alpine AS bun-runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates git python3 libstdc++
COPY --from=bun-runtime /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /app
COPY --from=builder /app/bin/dutybot .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/agents ./agents
RUN bun install --cwd /app/agents/skills/url-to-markdown --frozen-lockfile
EXPOSE 8080
CMD ["./dutybot"]
