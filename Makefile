# cf-proxy Makefile

.PHONY: all build build-all build-market build-platform clean dev test fmt vet deps upx help

APP_NAME := cf-proxy
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S_UTC')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

BUILD_DIR := build
DIST_DIR := dist

LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)
BUILD_FLAGS := -ldflags="$(LDFLAGS)" -trimpath

all: clean deps build

deps:
	@echo "整理依赖..."
	@go mod tidy

# 构建当前平台
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@echo "构建完成: $(BUILD_DIR)/$(APP_NAME)"

# 构建全部平台 + 插件市场二进制
build-all: clean deps
	@chmod +x scripts/build.sh
	@./scripts/build.sh --all

# 只构建插件市场二进制（linux/amd64、windows/amd64、linux/arm64）
build-market: clean deps
	@chmod +x scripts/build.sh
	@./scripts/build.sh --market

# 构建指定平台: make build-platform PLATFORM=linux/amd64
build-platform: clean deps
	@chmod +x scripts/build.sh
	@./scripts/build.sh --platform $(PLATFORM)

dev:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(APP_NAME) .
	@echo "开发构建完成: $(BUILD_DIR)/$(APP_NAME)"

test:
	go test -v ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# UPX 压缩 build/ 下全部二进制
upx:
	@command -v upx >/dev/null 2>&1 || { echo "未安装 upx"; exit 1; }
	@for f in $(BUILD_DIR)/$(APP_NAME)*; do \
		[ -f "$$f" ] || continue; \
		echo "压缩 $$f"; upx --best --lzma "$$f" || true; \
	done

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

help:
	@echo "cf-proxy 构建目标:"
	@echo "  make build                    当前平台构建"
	@echo "  make build-all                全平台构建 + 发布包"
	@echo "  make build-market             插件市场二进制"
	@echo "  make build-platform PLATFORM=linux/amd64"
	@echo "  make test / vet / fmt / upx / clean"
