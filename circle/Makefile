# circle 编译器 Makefile
#
# 用法：
#   make build         编译 circle CLI 到 ./release/circle
#   make build-studio  编译 Mocker Studio（wails v2 GUI）到 ./release/circle-studio
#   make build-all     同时编译 CLI + Studio
#   make test          跑所有单元测试
#   make run           编译并跑（默认参数：-ast -i example/cookie.mocker）
#   make clean         删除 release/ 目录
#   make tidy          go mod tidy
#   make help          显示所有 target

# ─────────── 变量 ───────────
BINARY      := circle
STUDIO      := circle-studio
RELEASE_DIR := release
ENTRY       := ./cmd/circle
STUDIO_ENTRY := ./cmd/mocker-studio
VITE_PORT   := 9245
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

$(OUTPUT): $(shell find . -name '*.go' -not -path './release/*' -not -path './cmd/mocker-studio/*')
	@echo "→ building $(BIN_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(RELEASE_DIR)
	@GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(OUTPUT) $(ENTRY)
	@echo "✓ binary built"

# ─────────── Mocker Studio（wails v2 GUI）───────────
#
# 注意：
#   - 需要 Node.js 18+ 和 npm
#   - 需要 GTK3 / WebKit2GTK（Linux）
#   - 需要 wails v2 CLI: go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
#   - 前端必须先 npm install + build 出 dist/
#   - 前端源文件变化会自动触发重编（tsx/ts/css）
#
WAILS        ?= $(HOME)/go/bin/wails
STUDIO_OUTPUT := $(RELEASE_DIR)/$(STUDIO)-$(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
	STUDIO_OUTPUT := $(STUDIO_OUTPUT).exe
endif

# 前端源文件列表（触发重编）
FRONTEND_SRC := $(shell find $(STUDIO_ENTRY)/frontend/src -name '*.tsx' -o -name '*.ts' -o -name '*.css' 2>/dev/null)

.PHONY: build-studio
build-studio: $(STUDIO_OUTPUT)
	@echo ""
	@echo "✅ studio build done: $(STUDIO_OUTPUT)"
	@ls -lh $(STUDIO_OUTPUT) | awk '{print "   size:", $$5}'

$(STUDIO_OUTPUT): $(shell find . -name '*.go' -not -path './release/*') $(STUDIO_ENTRY)/frontend/package.json $(FRONTEND_SRC) $(STUDIO_ENTRY)/wails.json
	@echo "→ regenerating wails v2 bindings..."
	@cd $(STUDIO_ENTRY) && $(WAILS) generate module 2>/dev/null
	@echo "→ building frontend..."
	@cd $(STUDIO_ENTRY)/frontend && npm install --silent && npm run build
	@echo "→ building $(STUDIO)-$(GOOS)-$(GOARCH)..."
	@mkdir -p $(RELEASE_DIR)
	@GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -tags production,webkit2_41 -ldflags='$(LDFLAGS)' -o $(STUDIO_OUTPUT) $(STUDIO_ENTRY)
	@echo "✓ studio binary built"

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

# Studio dev 模式（需要安装 wails v2 CLI: go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1）
#
# 工作流：
#   make dev-studio    # 启动 wails dev（hot reload Go + 前端）
#
.PHONY: dev-studio
dev-studio:
	@echo "→ starting wails v2 dev mode (Go + frontend hot reload)..."
	@cd $(STUDIO_ENTRY) && $(WAILS) dev

# 仅前端 dev（vite server，方便浏览器单独调试）
.PHONY: dev-studio-frontend
dev-studio-frontend:
	@echo "→ starting vite dev server on port $(VITE_PORT)..."
	@cd $(STUDIO_ENTRY)/frontend && npm install --silent && npm run dev

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
	@echo "  make build         编译到 $(RELEASE_DIR)/$(BIN_NAME)（默认）"
	@echo "  make build-studio  编译 Mocker Studio（wails v2 GUI）到 $(RELEASE_DIR)/$(STUDIO)"
	@echo "  make build-all     跨平台批量编译（linux/darwin/windows）"
	@echo "  make dev-studio    启动 Studio 开发模式（wails v2 dev）"
	@echo "  make test          跑所有单元测试"
	@echo "  make test-lexer    只测 lexer"
	@echo "  make test-parser   只测 parser"
	@echo "  make run           编译并跑 cookie.ce"
	@echo "  make run-main      编译并跑 main.ce"
	@echo "  make clean         删除 release/ 目录"
	@echo "  make clean-all     删除 release/ + go cache"
	@echo "  make tidy          go mod tidy"
	@echo "  make verify        go vet + 编译验证"
	@echo "  make install       安装到 /usr/local/bin/circle"
	@echo ""
	@echo "Examples:"
	@echo "  make"
	@echo "  make build GOOS=linux GOARCH=arm64"
	@echo "  make build-studio"
	@echo "  make build-all"
	@echo "  ./$(OUTPUT) -ast -i ../example/cookie.ce"
	@echo ""
	@echo "Studio:"
	@echo "  ./$(RELEASE_DIR)/$(STUDIO)-linux-amd64 ../example   # 打开 example 工作区"
