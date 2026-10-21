FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o bin/dutybot ./cmd/server

# 运行镜像：git 供仓库克隆使用
FROM alpine:3.21
RUN apk add --no-cache ca-certificates git
WORKDIR /app
COPY --from=builder /app/bin/dutybot .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/agents ./agents
EXPOSE 8080
CMD ["./dutybot"]
