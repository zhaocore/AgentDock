FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOMEMLIMIT=256MiB GOGC=20 CGO_ENABLED=0 go build -p 1 -o bin/agentdock ./cmd/server

# 浏览器采集使用 Playwright 支持的 Debian 运行环境和配套 Chromium。
FROM oven/bun:1.3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git python3 tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY agents/skills/url-to-markdown/package.json agents/skills/url-to-markdown/bun.lock ./agents/skills/url-to-markdown/
RUN bun install --cwd /app/agents/skills/url-to-markdown --frozen-lockfile
RUN cd /app/agents/skills/url-to-markdown && bun node_modules/playwright/cli.js install --with-deps chromium
COPY --from=builder /app/bin/agentdock .
COPY migrations ./migrations
COPY agents ./agents
EXPOSE 8080
CMD ["./agentdock"]
