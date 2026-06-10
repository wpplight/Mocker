# circle 编译器 Makefile
#
# 用法：
#   make build       编译 circle CLI 到 ./release/circle
#   make test        跑所有单元测试
#   make run         编译并跑（默认参数：-ast -i example/cookie.mocker）
#   make clean       删除 release/ 目录
#   make tidy        go mod tidy
#   make help        显示所有 target

# ─────────── 变量 ───────────
BINARY      := circle
RELEASE_DIR := release
ENTRY       := ./cmd/circle
LDFLAGS     := -s -w              # 减小二进制体积
GO          := go
GOFLAGS     := -trimpath           # 去除构建路径（可重现构建）

# 平台 / 架构（可覆盖：make build GOOS=linux GOARCH=amd64）
GOOS        ?= $(shell $(GO) env GOOS)
GOARCH      ?= $(shell $(GO) env GOARCH)
BIN_NAME    := $(BINARY)-$(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
	BIN_NAME := $(BIN_NAME).exe
endif

# 完整输出路径
OUTPUT      := $(RELEASE_DIR)/$(BIN_NAME)

# ─────────── 默认目标 ───────────
.PHONY: all
all: build

# ─────────── 编译 ───────────
.PHONY: build
build: $(OUTPUT)
	@echo ""
	@echo "✅ build done: $(OUTPUT)"
	@ls -lh $(OUTPUT) | awk '{print "   size:", $$5}'
	@echo ""
	@echo "用法示例："
	@echo "  $(OUTPUT) -ast -i ../example/cookie.ce"
	@echo "  cat ../example/main.ce | $(OUTPUT) -i -"

$(OUTPUT): $(shell find . -name '*.go' -not -path './release/*')
	@echo "→ building $(BIN_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(RELEASE_DIR)
	@GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(OUTPUT) $(ENTRY)
	@echo "✓ binary built"

# 跨平台批量构建
.PHONY: build-all
build-all:
	@echo "→ building for linux/amd64..."
	@GOOS=linux GOARCH=amd64 $(MAKE) build GOOS=linux GOARCH=amd64
	@echo "→ building for darwin/arm64..."
	@GOOS=darwin GOARCH=arm64 $(MAKE) build GOOS=darwin GOARCH=arm64
	@echo "→ building for windows/amd64..."
	@GOOS=windows GOARCH=amd64 $(MAKE) build GOOS=windows GOARCH=amd64
	@echo ""
	@echo "✅ all builds done:"
	@ls -lh $(RELEASE_DIR)/

# ─────────── 测试 ───────────
.PHONY: test
test:
	@echo "→ running tests..."
	@$(GO) test -v ./...

.PHONY: test-lexer
test-lexer:
	@echo "→ testing mocker_lex..."
	@$(GO) test -v ./mocker_lex/...

.PHONY: test-parser
test-parser:
	@echo "→ testing parser..."
	@$(GO) test -v ./internal/parser/...

# ─────────── 跑示例 ───────────
.PHONY: run
run: build
	@$(OUTPUT) -ast -i ../example/cookie.mocker

.PHONY: run-main
run-main: build
	@$(OUTPUT) -ast -i ../example/main.mocker

# ─────────── 清理 ───────────
.PHONY: clean
clean:
	@echo "→ cleaning $(RELEASE_DIR)/..."
	@rm -rf $(RELEASE_DIR)
	@echo "✓ cleaned"

.PHONY: clean-all
clean-all: clean
	@$(GO) clean -cache -testcache
	@echo "✓ go cache cleared"

# ─────────── 依赖管理 ───────────
.PHONY: tidy
tidy:
	@$(GO) mod tidy

.PHONY: verify
verify:
	@$(GO) vet ./...
	@$(GO) build -o /dev/null ./...

# ─────────── 安装（系统 PATH）───────────
.PHONY: install
install: build
	@echo "→ installing to /usr/local/bin/circle..."
	@cp $(OUTPUT) /usr/local/bin/circle
	@chmod +x /usr/local/bin/circle
	@echo "✓ installed: $(shell which circle)"

# ─────────── 帮助 ───────────
.PHONY: help
help:
	@echo "circle 编译器 Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  make build       编译到 $(RELEASE_DIR)/$(BIN_NAME)（默认）"
	@echo "  make build-all   跨平台批量编译（linux/darwin/windows）"
	@echo "  make test        跑所有单元测试"
	@echo "  make test-lexer  只测 lexer"
	@echo "  make test-parser 只测 parser"
	@echo "  make run         编译并跑 cookie.ce"
	@echo "  make run-main    编译并跑 main.ce"
	@echo "  make clean       删除 release/ 目录"
	@echo "  make clean-all   删除 release/ + go cache"
	@echo "  make tidy        go mod tidy"
	@echo "  make verify      go vet + 编译验证"
	@echo "  make install     安装到 /usr/local/bin/circle"
	@echo "  make help        显示本帮助"
	@echo ""
	@echo "Examples:"
	@echo "  make"
	@echo "  make build GOOS=linux GOARCH=arm64"
	@echo "  make build-all"
	@echo "  ./$(OUTPUT) -ast -i ../example/cookie.ce"
