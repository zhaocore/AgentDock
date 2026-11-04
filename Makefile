.PHONY: build run test test-integration swagger dev db-up db-down db-migrate lint docker-build docker-up docker-down

# 编译 HTTP 服务二进制
build:
	go build -o bin/agentdock-server ./cmd/server

# 直接运行
run:
	go run ./cmd/server

# 单元测试
test:
	go test ./internal/... -count=1

# 集成测试（需要 docker-compose 基础设施）
test-integration:
	go test ./internal/... -tags=integration -count=1

# 生成 Swagger 文档
swagger: build-swagger

build-swagger:
	swag init -g cmd/server/main.go -o docs/swagger

# 本地开发：启动基础设施 + 运行服务
dev:
	docker-compose -f docker-compose.dev.yml up -d mysql redis rustfs
	go run ./cmd/server

# 启动基础设施
db-up:
	docker-compose -f docker-compose.dev.yml up -d mysql redis rustfs

# 停止基础设施
db-down:
	docker-compose -f docker-compose.dev.yml down

# 运行数据库迁移
db-migrate:
	go run ./cmd/migrate up

# Lint 检查
lint:
	golangci-lint run ./...

# Docker 镜像构建
docker-build:
	docker build -t agentdock:latest .

# 全栈启动（生产）
docker-up:
	docker-compose up -d

# 全栈停止
docker-down:
	docker-compose down
