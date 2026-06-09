# glex Makefile
#
# 用法:
#   make build       编译到 release/glex
#   make test        运行所有测试
#   make run         编译并用示例文件跑一遍
#   make clean       清理 release/
#   make help        显示所有可用 target

# ─────────────────────────────────────────────
# 配置
# ─────────────────────────────────────────────
BINARY      := glex
RELEASE_DIR := release
BIN_PATH    := $(RELEASE_DIR)/$(BINARY)
MAIN_PKG    := ./cmd/glex
EXAMPLE     := examples/tokens.glex

# 去掉调试信息，减小二进制体积
LDFLAGS := -s -w

# 默认目标
.DEFAULT_GOAL := help

# ─────────────────────────────────────────────
# 构建
# ─────────────────────────────────────────────
.PHONY: build
build: ## 编译到 release/ 文件夹
	@mkdir -p $(RELEASE_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BIN_PATH) $(MAIN_PKG)
	@echo "✓ Built: $(BIN_PATH)"

.PHONY: build-verbose
build-verbose: ## 编译并显示详细过程
	@mkdir -p $(RELEASE_DIR)
	go build -v -ldflags="$(LDFLAGS)" -o $(BIN_PATH) $(MAIN_PKG)
	@ls -lh $(BIN_PATH)

.PHONY: build-all
build-all: ## 跨平台编译到 release/（linux / darwin / windows）
	@mkdir -p $(RELEASE_DIR)
	GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(RELEASE_DIR)/$(BINARY)-linux-amd64       $(MAIN_PKG)
	GOOS=darwin  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(RELEASE_DIR)/$(BINARY)-darwin-amd64      $(MAIN_PKG)
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(RELEASE_DIR)/$(BINARY)-windows-amd64.exe $(MAIN_PKG)
	@ls -lh $(RELEASE_DIR)

.PHONY: install
install: ## 安装到 $(GOPATH)/bin
	go install $(MAIN_PKG)

# ─────────────────────────────────────────────
# 测试
# ─────────────────────────────────────────────
.PHONY: test
test: ## 运行所有测试
	go test -count=1 ./...

.PHONY: test-verbose
test-verbose: ## 详细测试输出
	go test -v -count=1 ./...

.PHONY: test-coverage
test-coverage: ## 测试并生成覆盖率报告
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo "详细 HTML 报告: go tool cover -html=coverage.out"

# ─────────────────────────────────────────────
# 代码质量
# ─────────────────────────────────────────────
.PHONY: fmt
fmt: ## go fmt
	go fmt ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: check
check: fmt vet test ## fmt + vet + test 一把跑

# ─────────────────────────────────────────────
# 依赖
# ─────────────────────────────────────────────
.PHONY: deps
deps: ## 下载并整理依赖
	go mod download
	go mod tidy

# ─────────────────────────────────────────────
# 运行
# ─────────────────────────────────────────────
.PHONY: run
run: build ## 编译并用 examples/tokens.glex 跑一遍（统计 + AST）
	./$(BIN_PATH) -i $(EXAMPLE) -v -stats -ast | head -60

.PHONY: run-stats
run-stats: build ## 编译并只跑统计表
	./$(BIN_PATH) -i $(EXAMPLE) -stats

.PHONY: run-ast
run-ast: build ## 编译并打印所有 AST
	./$(BIN_PATH) -i $(EXAMPLE) -ast

# ─────────────────────────────────────────────
# 清理
# ─────────────────────────────────────────────
.PHONY: clean
clean: ## 清理 release/ 目录和测试产物
	rm -rf $(RELEASE_DIR)
	rm -f coverage.out
	@echo "✓ Cleaned"

# ─────────────────────────────────────────────
# 帮助
# ─────────────────────────────────────────────
.PHONY: help
help: ## 显示帮助
	@echo ""
	@echo "  glex Makefile"
	@echo ""
	@echo "  用法: make [target]"
	@echo ""
	@echo "  可用目标:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""
